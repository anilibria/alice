package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/anilibria/alice/internal/cache"
	"github.com/anilibria/alice/internal/proxy"
	"github.com/anilibria/alice/internal/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
)

var (
	gCli *cli.Context
	gLog *zerolog.Logger

	gCtx   context.Context
	gAbort context.CancelFunc
)

type Service struct {
	fb *fiber.App
	// fbstor fiber.Storage

	proxy *proxy.Proxy
	cache *cache.Cache

	syslogWriter io.Writer

	pprofPrefix string
	pprofSecret []byte
}

func NewService(c *cli.Context, l *zerolog.Logger, s io.Writer) *Service {
	gCli, gLog = c, l

	service := new(Service)
	service.syslogWriter = s

	appname := fmt.Sprintf("%s/%s", c.App.Name, c.App.Version)

	service.fb = fiber.New(fiber.Config{
		EnableTrustedProxyCheck: len(gCli.String("http-trusted-proxies")) > 0,
		TrustedProxies:          strings.Split(gCli.String("http-trusted-proxies"), ","),
		ProxyHeader:             fiber.HeaderXForwardedFor,

		AppName:               appname,
		ServerHeader:          appname,
		DisableStartupMessage: true,

		StrictRouting:      true,
		DisableDefaultDate: true,
		DisableKeepalive:   false,

		DisablePreParseMultipartForm: true,

		Prefork:      gCli.Bool("http-prefork"),
		IdleTimeout:  300 * time.Second,
		ReadTimeout:  10000 * time.Millisecond,
		WriteTimeout: 2000 * time.Millisecond,

		DisableDefaultContentType: true,

		RequestMethods: []string{
			fiber.MethodHead,
			fiber.MethodGet,
			fiber.MethodPost,
		},

		ErrorHandler: func(c *fiber.Ctx, err error) error {
			// reject invalid requests
			if strings.TrimSpace(c.Hostname()) == "" {
				gLog.Warn().Msgf("invalid request from %s", c.Context().Conn().RemoteAddr().String())
				gLog.Debug().Msgf("invalid request: %+v ; error - %+v", c, err)
				return c.Context().Conn().Close()
			}

			c.Set(fiber.HeaderContentType, fiber.MIMETextPlainCharsetUTF8)

			var e *fiber.Error
			if !errors.As(err, &e) {
				return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
			}

			rlog(c).Error().Msgf("%v", err)
			return c.SendStatus(e.Code)
		},
	})

	// storage setup for fiber's limiter
	// if gCli.Bool("limiter-use-bbolt") {
	// 	var prefix string
	// 	if prefix = gCli.String("database-prefix"); prefix == "" {
	// 		prefix = "."
	// 	}

	// 	service.fbstor = bolt.New(bolt.Config{
	// 		Database: fmt.Sprintf("%s/%s.db", prefix, gCli.App.Name),
	// 		Bucket:   "application-limiter",
	// 		Reset:    false,
	// 	})
	// }

	return service
}

func (m *Service) Bootstrap() (e error) {
	var wg sync.WaitGroup
	var echan = make(chan error, 32)

	// goroutine helper
	gofunc := func(w *sync.WaitGroup, p func()) {
		w.Add(1)

		go func(done, payload func()) {
			payload()
			done()
		}(w.Done, p)
	}

	gCtx, gAbort = context.WithCancel(context.Background())
	gCtx = context.WithValue(gCtx, utils.CKLogger, gLog)
	gCtx = context.WithValue(gCtx, utils.CKCliCtx, gCli)
	gCtx = context.WithValue(gCtx, utils.CKAbortFunc, gAbort)

	// defer m.checkErrorsBeforeClosing(echan)
	// defer wg.Wait() // !!
	defer gLog.Debug().Msg("waiting for opened goroutines")
	defer gAbort()

	// BOOTSTRAP SECTION:
	// cache module
	if m.cache, e = cache.NewCache(gCtx); e != nil {
		return
	}
	gCtx = context.WithValue(gCtx, utils.CKCache, m.cache)
	gofunc(&wg, m.cache.Bootstrap)

	// proxy module
	m.proxy = proxy.NewProxy(gCtx)

	// another subsystems
	// ? write initialization block above the http
	// ...

	// fiber configuration
	m.fiberMiddlewareInitialization()
	m.fiberRouterInitialization()

	// ! http server bootstrap (shall be at the end of bootstrap section)
	gofunc(&wg, func() {
		gLog.Debug().Msg("starting fiber http server...")
		defer gLog.Debug().Msg("fiber http server has been stopped")

		if e = m.fb.Listen(gCli.String("http-listen-addr")); errors.Is(e, context.Canceled) {
			return
		} else if e != nil {
			gLog.Error().Err(e).Msg("fiber internal error")
		}
	})

	// main event loop
	wg.Add(1)
	go m.loop(echan, wg.Done)

	gLog.Info().Msg("ready...")

	wg.Wait()
	return nil
}

func (m *Service) loop(_ chan error, done func()) {
	defer done()

	kernSignal := make(chan os.Signal, 1)
	signal.Notify(kernSignal, syscall.SIGINT, syscall.SIGTERM, syscall.SIGTERM, syscall.SIGQUIT)

	gLog.Debug().Msg("initiate main event loop...")
	defer gLog.Debug().Msg("main event loop has been closed")

LOOP:
	for {
		select {
		case <-kernSignal:
			gLog.Info().Msg("kernel signal has been caught; initiate application closing...")
			gAbort()
			break LOOP
		// case err := <-errs:
		// 	gLog.Error().Err(err).Msg("there are internal errors from one of application submodule")
		// 	gLog.Info().Msg("calling abort()...")
		// 	gAbort()
		case <-gCtx.Done():
			gLog.Info().Msg("internal abort() has been caught; initiate application closing...")
			break LOOP
		}
	}

	// http destruct (wtf fiber?)
	// ShutdownWithContext() may be called only after fiber.Listen is running (O_o)
	if e := m.fb.ShutdownWithContext(gCtx); e != nil {
		gLog.Error().Err(e).Msg("fiber Shutdown() error")
	}
}

func rlog(c *fiber.Ctx) *zerolog.Logger {
	return c.Locals("logger").(*zerolog.Logger)
}
