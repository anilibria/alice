package service

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	"github.com/anilibria/alice/internal/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/middleware/pprof"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/rewrite"
	"github.com/gofiber/fiber/v2/middleware/skip"
	"github.com/rs/zerolog"
)

var loggerPool = sync.Pool{
	New: func() interface{} {
		if gALog != nil {
			l := gALog.With().Logger()
			return &l
		} else {
			l := gLog.With().Logger()
			return &l
		}
	},
}

func (m *Service) fiberMiddlewareInitialization() {
	// pprof profiler
	// manual:
	// 	curl -o profile.out https://host/debug/pprof -H 'X-Authorization: $TOKEN'
	// 	go tool pprof profile.out
	if gCli.Bool("http-pprof-enable") {
		m.pprofPrefix = gCli.String("http-pprof-prefix")
		m.pprofSecret = []byte(gCli.String("http-pprof-secret"))

		var pprofNext func(*fiber.Ctx) bool
		if len(m.pprofSecret) != 0 {
			pprofNext = func(c *fiber.Ctx) (_ bool) {
				isecret := c.Context().Request.Header.Peek("x-pprof-secret")

				if len(isecret) == 0 {
					return
				}

				return !bytes.Equal(m.pprofSecret, isecret)
			}
		}

		m.fb.Use(pprof.New(pprof.Config{
			Next:   pprofNext,
			Prefix: gCli.String("http-pprof-prefix"),
		}))
	}

	// request id 3.0
	m.fb.Use(func(c *fiber.Ctx) error {
		c.Set("X-Request-Id", strconv.FormatUint(c.Context().ID(), 10))
		return c.Next()
	})

	// prefixed logger initialization
	// - we send logs in syslog and stdout by default,
	// - but if access-log-stdout is 0 we use syslog output only
	m.fb.Use(func(c *fiber.Ctx) (e error) {
		logger := loggerPool.Get().(*zerolog.Logger)

		logger.UpdateContext(func(zc zerolog.Context) zerolog.Context {
			return zc.Uint64("id", c.Context().ID())
		})

		logger2 := logger.Hook(UDPSizeDiscardHook{})

		c.Locals("logger", &logger2)
		e = c.Next()

		loggerPool.Put(logger)
		return
	})

	// limiter
	if gCli.Bool("limiter-enable") {
		limitederr := fiber.NewError(fiber.StatusTooManyRequests, "to many requests has been sended, please wait and try again")

		m.fb.Use(limiter.New(limiter.Config{
			Next: func(c *fiber.Ctx) bool {
				return c.IP() == "127.0.0.1" || gCli.App.Version == "localbuilded"
			},

			Max:        gCli.Int("limiter-max-req"),
			Expiration: gCli.Duration("limiter-records-duration"),

			KeyGenerator: func(c *fiber.Ctx) string {
				return c.IP()
			},

			LimitReached: func(c *fiber.Ctx) error {
				return c.App().ErrorHandler(c, limitederr)
			},

			Storage: m.fbstor,
		}))
	}

	// panic recover for all handlers
	m.fb.Use(recover.New(recover.Config{
		EnableStackTrace: true,
		StackTraceHandler: func(c *fiber.Ctx, e interface{}) {
			rlog(c).Error().Str("request", c.Request().String()).Bytes("stack", debug.Stack()).
				Msg("panic has been caught")
			_, _ = os.Stderr.WriteString(fmt.Sprintf("panic: %v\n%s\n", e, debug.Stack())) //nolint:errcheck // This will never fail

			c.Status(fiber.StatusInternalServerError)
		},
	}))

	// time collector + logger
	m.fb.Use(func(c *fiber.Ctx) (e error) {
		started, e := time.Now(), c.Next()
		elapsed := time.Since(started).Round(time.Microsecond)

		status, lvl := c.Response().StatusCode(), utils.HTTPAccessLogLevel

		// ? not profitable
		// TODO too much allocations here:
		err := AcquireFErr()
		defer ReleaseFErr(err)

		var cause string
		if errors.As(e, &err) || status >= fiber.StatusInternalServerError {
			status, lvl, cause = err.Code, zerolog.WarnLevel, err.Error()
		}

		rlog(c).WithLevel(lvl).
			Int("status", status).
			Str("method", c.Method()).
			Str("path", c.Path()).
			Str("ip", c.IP()).
			Dur("latency", elapsed).
			Str("user-agent", c.Get(fiber.HeaderUserAgent)).Msg(cause)

		return
	})

	// rewrite module for Anilibrix Plus
	if gCli.Bool("anilibrix-cmpb-mode") {
		m.fb.Use(rewrite.New(rewrite.Config{
			Next: func(c *fiber.Ctx) bool {
				return c.Path()[:2] != "//"
			},
			Rules: map[string]string{
				"//public/api/index.php": "/public/api/index.php",
			},
		}))
	}
}

func (m *Service) fiberRouterInitialization() {
	//
	// ALICE internal cache api
	cacheapi := m.fb.Group("/internal/cache", m.proxy.MiddlewareInternalApi)

	cacheapi.Get("/stats", m.proxy.HandleCacheStats)
	cacheapi.Post("/stats/reset", m.proxy.HandleCacheStatsReset)
	cacheapi.Get("/dump", m.proxy.HandleCacheDump)
	cacheapi.Get("/dumpkeys", m.proxy.HandleCacheDumpKeys)
	cacheapi.Post("/purge", m.proxy.HandleCachePurge)
	cacheapi.Post("/purgeall", m.proxy.HandleCachePurgeAll)

	//
	// ALICE apiv1 requests proxying lifecycle:

	// step1 - validate request
	apiv1 := m.fb.Group("/public/api", m.proxy.MiddlewareValidation)

	// step2 - check cache availability and try to respond with it
	apiv1.Use(skip.New(m.proxy.HandleProxyToCache, m.proxy.IsRequestCached))

	// step3 - proxy request to upstream
	apiv1.Use(m.proxy.HandleProxyToDst)
}

type UDPSizeDiscardHook struct{}

func (UDPSizeDiscardHook) Run(e *zerolog.Event, level zerolog.Level, message string) {
	fmt.Println(len(message))
	if len(message) <= utils.MAX_UDP_MSG_BYTES {
		return
	}

	level = zerolog.ErrorLevel
	e.Msgf("message was dropped because of high length %d\n%s", len(message), message)
	e.Discard()
}
