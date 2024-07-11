package service

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"time"

	"github.com/anilibria/alice/internal/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/favicon"
	"github.com/gofiber/fiber/v2/middleware/pprof"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/rs/zerolog"
)

func (m *Service) fiberMiddlewareInitialization() {

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

	// request id
	m.fb.Use(requestid.New())

	// insert payload for futher processing
	m.fb.Use(func(c *fiber.Ctx) error {
		c.Locals(utils.FLKRewriterHeader, gCli.String("rewriter-response-header"))
		return c.Next()
	})

	// prefixed logger initialization
	// - we send logs in syslog and stdout by default,
	// - but if access-log-stdout is 0 we use syslog output only
	m.fb.Use(func(c *fiber.Ctx) error {
		logger := gLog.With().Str("id", c.Locals("requestid").(string)).Logger().
			Level(m.accesslogLevel)
		syslogger := logger.Output(m.syslogWriter)

		if zerolog.GlobalLevel() > zerolog.DebugLevel {
			logger = logger.Output(io.Discard)
		}

		c.Locals("logger", &logger)
		c.Locals("syslogger", &syslogger)
		return c.Next()
	})

	// time collector + logger
	m.fb.Use(func(c *fiber.Ctx) (e error) {
		started, e := time.Now(), c.Next()
		stopped := time.Now()
		elapsed := stopped.Sub(started).Round(time.Microsecond)

		status, lvl, err := c.Response().StatusCode(), m.accesslogLevel, new(fiber.Error)
		if errors.As(e, &err) || status >= fiber.StatusInternalServerError {
			status, lvl = err.Code, zerolog.WarnLevel
		}

		// get rewriter payload
		rpayload := c.Response().Header.Peek(c.Locals(utils.FLKRewriterHeader).(string))

		rlog(c).WithLevel(lvl).
			Int("status", status).
			Str("method", c.Method()).
			Str("path", c.Path()).
			Str("ip", c.IP()).
			Dur("latency", elapsed).
			Str("payload", string(rpayload)).
			Str("user-agent", c.Get(fiber.HeaderUserAgent)).Msg("")
		rsyslog(c).WithLevel(lvl).
			Int("status", status).
			Str("method", c.Method()).
			Str("path", c.Path()).
			Str("ip", c.IP()).
			Dur("latency", elapsed).
			Str("payload", string(rpayload)).
			Str("user-agent", c.Get(fiber.HeaderUserAgent)).Msg("")

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

	// debug
	if gCli.Bool("http-pprof-enable") {
		m.fb.Use(pprof.New())
	}

	// favicon disable
	m.fb.Use(favicon.New(favicon.ConfigDefault))

	// compress support
	m.fb.Use(compress.New(compress.Config{
		Level: compress.LevelBestSpeed,
	}))
}

func (m *Service) fiberRouterInitialization() {

	// routers initialization
	// rw := extractor.NewRewriter()

	// check for api initialization
	// m.fb.Use(skip.New(rw.HandleUnavailable, rw.IsInitialized))

	// // check for multipart/formdata content-type
	// m.fb.Use(skip.New(rw.HandleDummy, rw.IsMultipartForm))

	// // rewrite
	// m.fb.Post("/public/api/index.php", rw.HandleIndex)

	//
	//
	//
	//

	m.fb.Use(m.proxy.HandleProxyToDst)
}
