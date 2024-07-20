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
	"github.com/gofiber/storage/bbolt/v2"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
)

var (
	gCli  *cli.Context
	gLog  *zerolog.Logger
	gALog *zerolog.Logger

	gCtx   context.Context
	gAbort context.CancelFunc
)

type Service struct {
	loopError error

	fb     *fiber.App
	fbstor fiber.Storage

	proxy *proxy.Proxy
	cache *cache.Cache

	syslogWriter io.Writer

	pprofPrefix string
	pprofSecret []byte
}

func NewService(c *cli.Context, l *zerolog.Logger, s io.Writer) *Service {
	gCli, gLog, gALog = c, l, nil

	if zerolog.GlobalLevel() > zerolog.DebugLevel && zerolog.GlobalLevel() < zerolog.NoLevel {
		alogger := gLog.With().Logger().Output(s)
		gALog = &alogger
	}

	service := new(Service)
	service.syslogWriter = s

	appname := fmt.Sprintf("%s/%s", c.App.Name, c.App.Version)
	errdesc := "error provided by " + appname + " service"

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
		IdleTimeout:  gCli.Duration("http-idle-timeout"),
		ReadTimeout:  gCli.Duration("http-read-timeout"),
		WriteTimeout: gCli.Duration("http-write-timeout"),

		DisableDefaultContentType: true,

		RequestMethods: []string{
			fiber.MethodHead,
			fiber.MethodGet,
			fiber.MethodPost,
		},

		ErrorHandler: func(c *fiber.Ctx, err error) error {
			// reject invalid requests
			if strings.TrimSpace(c.Hostname()) == "" {
				gLog.Warn().Msg("invalid request from " + c.Context().RemoteIP().String())
				gLog.Debug().Msgf("invalid request: %+v ; error - %+v", c, err)
				return c.Context().Conn().Close()
			}

			// AniLibria apiv1 error style:
			c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSONCharsetUTF8)

			// `rspcode` - apiv1 legacy hardcode
			// if u have 4XX or 5XX in service, u must respond with 200
			rspcode, respdesc, respond :=
				fiber.StatusOK,
				errdesc,
				func(status int, msg, desc string) {
					if e := utils.RespondWithApiError(status, msg, desc, c); e != nil {
						rlog(c).Error().Msg("could not respond with JSON error - " + e.Error())
					}
				}

			// ? not profitable
			// TODO too much allocations here:
			ferr := AcquireFErr()
			defer ReleaseFErr(ferr)

			// parse fiber error
			if !errors.As(err, &ferr) {
				respond(fiber.StatusInternalServerError, err.Error(), "")
				return c.SendStatus(rspcode)
			}

			if zerolog.GlobalLevel() <= zerolog.DebugLevel {
				rlog(c).Debug().Msgf("%+v", err)
			}

			respond(ferr.Code, ferr.Error(), respdesc)
			return c.SendStatus(rspcode)
		},
	})

	// storage setup for fiber's limiter
	if gCli.Bool("limiter-use-bbolt") {
		var prefix string
		if prefix = gCli.String("database-prefix"); prefix == "" {
			prefix = "."
		}

		service.fbstor = bbolt.New(bbolt.Config{
			Database: fmt.Sprintf("%s/%s.db", prefix, gCli.App.Name),
			Bucket:   "apiv1-limiter",
			Timeout:  1 * time.Minute,
			Reset:    gCli.Bool("limiter-startup-reset"),
		})
	}

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
			echan <- e
		}
	})

	// main event loop
	wg.Add(1)
	go m.loop(echan, wg.Done)

	gLog.Info().Msg("ready...")

	wg.Wait()
	return m.loopError
}

func (m *Service) loop(errs chan error, done func()) {
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
		case err := <-errs:
			gLog.Debug().Err(err).Msg("there are internal errors from one of application submodule")
			m.loopError = err

			gLog.Info().Msg("calling abort()...")
			gAbort()
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

// TODO 2delete
// I think this block of code is not profitable
// so may be it must be reverted

var ferrPool = sync.Pool{
	New: func() interface{} {
		return new(fiber.Error)
	},
}

func AcquireFErr() *fiber.Error {
	return ferrPool.Get().(*fiber.Error)
}

func ReleaseFErr(e *fiber.Error) {
	// ? is it required
	e.Code, e.Message = 0, ""
	ferrPool.Put(e)
}
