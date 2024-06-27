package service

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/anilibria/apiv1-cacher/internal/rewriter"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/favicon"
	"github.com/gofiber/fiber/v2/middleware/pprof"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/gofiber/fiber/v2/middleware/skip"
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

	// prefixed logger initialization
	// - we send logs in syslog and stdout by default,
	// - but if access-log-stdout is 0 we use syslog output only
	// !!!
	// !!!
	// !!!
	// m.fb.Use(func(c *fiber.Ctx) error {
	// 	logger := gLog.With().Str("id", c.Locals("requestid").(string)).Logger().
	// 		Level(m.runtime.Config.Get(runtime.ParamAccessLevel).(zerolog.Level))
	// 	syslogger := logger.Output(m.syslogWriter)

	// 	if m.runtime.Config.Get(runtime.ParamAccessStdout).(int) == 0 {
	// 		logger = logger.Output(io.Discard)
	// 	}

	// 	c.Locals("logger", &logger)
	// 	c.Locals("syslogger", &syslogger)
	// 	return c.Next()
	// })

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
	rw := rewriter.NewRewriter()

	// check for api initialization
	// m.fb.Use(skip.New(rw.HandleUnavailable, rw.IsInitialized))

	// check for multipart/formdata content-type
	m.fb.Use(skip.New(rw.HandleDummy, rw.IsMultipartForm))

	// rewrite
	m.fb.Post("/public/api/index.php", rw.HandleIndex)
}
