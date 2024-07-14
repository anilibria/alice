package service

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/anilibria/alice/internal/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/pprof"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/skip"
	"github.com/rs/zerolog"
)

func (m *Service) fiberMiddlewareInitialization() {
	// request id 3.0
	m.fb.Use(func(c *fiber.Ctx) error {
		c.Set("X-Request-Id", strconv.FormatUint(c.Context().ID(), 10))
		return c.Next()
	})

	// prefixed logger initialization
	// - we send logs in syslog and stdout by default,
	// - but if access-log-stdout is 0 we use syslog output only
	m.fb.Use(func(c *fiber.Ctx) error {
		logger, spanid := gLog.With().Logger(), c.Context().ID()

		logger.UpdateContext(func(zc zerolog.Context) zerolog.Context {
			return zc.Uint64("id", spanid)
		})

		if zerolog.GlobalLevel() > zerolog.DebugLevel && zerolog.GlobalLevel() < zerolog.NoLevel {
			logger = logger.Output(m.syslogWriter)
		}

		c.Locals("logger", &logger)
		return c.Next()
	})

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

		var err *fiber.Error
		if errors.As(e, &err) || status >= fiber.StatusInternalServerError {
			status, lvl = err.Code, zerolog.WarnLevel
		}

		rlog(c).WithLevel(lvl).
			Int("status", status).
			Str("method", c.Method()).
			Str("path", c.Path()).
			Str("ip", c.Context().RemoteIP().String()).
			Dur("latency", elapsed).
			Str("user-agent", c.Get(fiber.HeaderUserAgent)).Msg(err.Error())

		return
	})

	// ! LIMITER
	// media.Use(limiter.New(limiter.Config{
	// 	Next: func(c *fiber.Ctx) bool {
	// 		if m.runtime.Config.Get(runtime.ParamLimiter).(int) == 0 {
	// 			return true
	// 		}

	// 		return c.IP() == "127.0.0.1" || gCli.App.Version == "devel"
	// 	},

	// 	Max:        gCli.Int("limiter-max-req"),
	// 	Expiration: gCli.Duration("limiter-records-duration"),

	// 	KeyGenerator: func(c *fiber.Ctx) string {
	// 		return c.IP()
	// 	},

	// 	Storage: m.fbstor,
	// }))

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

	// compress support
	m.fb.Use(compress.New(compress.Config{
		Level: compress.LevelBestSpeed,
	}))
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
