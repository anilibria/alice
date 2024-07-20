package main

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/diode"
	"github.com/urfave/cli/v2"

	"github.com/anilibria/alice/internal/service"
	"github.com/anilibria/alice/internal/utils"
)

var version = "devel" // -ldflags="-X main.version=X.X.X"
var buildtime = "never"

func main() {
	var exitcode int
	defer func() { cli.OsExiter(exitcode) }()

	// non-blocking writer
	dwr := diode.NewWriter(os.Stdout, 1024, 10*time.Millisecond, func(missed int) {
		fmt.Fprintf(os.Stderr, "diodes dropped %d messages; check your log-rate, please\n", missed)
	})
	defer dwr.Close()

	// logger
	log := zerolog.New(zerolog.ConsoleWriter{
		Out: dwr,
	}).With().Timestamp().Caller().Logger()

	zerolog.CallerMarshalFunc = callerMarshalFunc
	zerolog.TimeFieldFormat = time.RFC3339Nano
	zerolog.SetGlobalLevel(zerolog.DebugLevel)

	// application
	app := cli.NewApp()
	cli.VersionFlag = &cli.BoolFlag{
		Name:               "version",
		Usage:              "show version",
		Aliases:            []string{"V"},
		DisableDefaultText: true,
	}
	cli.VersionPrinter = func(c *cli.Context) {
		fmt.Printf("%s\t%s\n", version, buildtime)
	}

	app.Name = "alice"
	app.Version = version
	app.Copyright = "(c) 2024 mindhunter86\nwith love for AniLibria project"
	app.Usage = "AniLibria legacy api cache service"
	app.Authors = append(app.Authors, &cli.Author{
		Name:  "MindHunter86",
		Email: "mindhunter86@vkom.cc",
	})

	app.Flags = []cli.Flag{
		// common settings
		&cli.StringFlag{
			Name:    "log-level",
			Value:   "info",
			Usage:   "levels: trace, debug, info, warn, err, panic, disabled",
			Aliases: []string{"l"},
			EnvVars: []string{"LOG_LEVEL"},
		},

		// common settings : syslog
		&cli.StringFlag{
			Name:    "syslog-server",
			Usage:   "syslog server (optional); syslog sender is not used if value is empty",
			Value:   "",
			EnvVars: []string{"SYSLOG_ADDRESS"},
		},
		&cli.StringFlag{
			Name:    "syslog-proto",
			Usage:   "syslog protocol (optional); tcp or udp is possible",
			Value:   "tcp",
			EnvVars: []string{"SYSLOG_PROTO"},
		},
		&cli.StringFlag{
			Name:  "syslog-tag",
			Usage: "optional setting; more information in syslog RFC",
			Value: "",
		},

		// fiber-server settings
		&cli.StringFlag{
			Name:  "http-listen-addr",
			Usage: "format - 127.0.0.1:8080, :8080",
			Value: "127.0.0.1:8080",
		},
		&cli.StringFlag{
			Name:  "http-trusted-proxies",
			Usage: "format - 192.168.0.0/16; can be separated by comma",
		},
		&cli.StringFlag{
			Name:  "http-realip-header",
			Value: "X-Real-Ip",
		},
		&cli.BoolFlag{
			Name: "http-prefork",
			Usage: `enables use of the SO_REUSEPORT socket option;
			if enabled, the application will need to be ran
			through a shell because prefork mode sets environment variables;
			EXPERIMENTAL! USE CAREFULLY!`,
			DisableDefaultText: true,
		},
		&cli.DurationFlag{
			Name:  "http-read-timeout",
			Value: 10 * time.Second,
		},
		&cli.DurationFlag{
			Name:  "http-write-timeout",
			Value: 5 * time.Second,
		},
		&cli.DurationFlag{
			Name:  "http-idle-timeout",
			Value: 10 * time.Minute,
		},
		&cli.BoolFlag{
			Name:               "http-pprof-enable",
			Usage:              "enable golang http-pprof methods",
			DisableDefaultText: true,
		},
		&cli.StringFlag{
			Name:    "http-pprof-prefix",
			Usage:   "it should start with (but not end with) a slash. Example: '/test'",
			EnvVars: []string{"PPROF_PREFIX"},
		},
		&cli.StringFlag{
			Name:    "http-pprof-secret",
			Usage:   "define static secret in x-pprof-secret header for avoiding unauthorized access",
			EnvVars: []string{"PPROF_SECRET"},
		},

		// limiter settings
		&cli.BoolFlag{
			Name:               "limiter-enable",
			DisableDefaultText: true,
		},
		&cli.BoolFlag{
			Name:               "limiter-use-bbolt",
			Usage:              "use bbolt key\value file database instead of memory database",
			DisableDefaultText: true,
		},
		&cli.BoolFlag{
			Name:               "limiter-bbolt-reset",
			Usage:              "if bbolt used as storage, reset all limited IPs on startup",
			DisableDefaultText: true,
		},
		&cli.IntFlag{
			Name:  "limiter-max-req",
			Value: 200,
		},
		&cli.DurationFlag{
			Name:  "limiter-records-duration",
			Value: 5 * time.Minute,
		},

		// proxy settings
		&cli.StringFlag{
			Name:  "proxy-dst-server",
			Usage: "destination server",
			Value: "127.0.0.1:36080",
		},
		&cli.StringFlag{
			Name:  "proxy-dst-host",
			Usage: "request Host header for dst server",
			Value: "api.anilibria.tv",
		},
		&cli.DurationFlag{
			Name:  "proxy-read-timeout",
			Value: 10 * time.Second,
		},
		&cli.DurationFlag{
			Name:  "proxy-write-timeout",
			Value: 5 * time.Second,
		},
		&cli.DurationFlag{
			Name:  "proxy-conn-timeout",
			Usage: "force connection rotation after this `time`",
			Value: 10 * time.Minute,
		},
		&cli.DurationFlag{
			Name:  "proxy-idle-timeout",
			Value: 5 * time.Minute,
		},
		&cli.IntFlag{
			Name:  "proxy-max-idle-conn",
			Value: 256,
		},
		&cli.IntFlag{
			Name:  "proxy-max-conns-per-host",
			Value: 256,
		},
		&cli.DurationFlag{
			Name:  "proxy-dns-cache-dur",
			Value: 1 * time.Minute,
		},
		&cli.IntFlag{
			Name:  "proxy-tcpdial-concurr",
			Usage: "0 - unlimited",
			Value: 0,
		},

		// cache settings
		&cli.StringFlag{
			Name:  "cache-api-secret",
			Usage: "define static secret in x-api-secret header for avoiding unauthorized access",
			Value: "secret",
		},
		&cli.IntFlag{
			Name:  "cache-shards",
			Usage: "number of shards (must be a power of 2)",
			Value: 512,
		},
		&cli.DurationFlag{
			Name:  "cache-life-window",
			Usage: "time after which entry can be evicted",
			Value: 10 * time.Minute,
		},
		&cli.DurationFlag{
			Name: "cache-clean-window",
			Usage: `Interval between removing expired entries (clean up)
			If set to <= 0 then no action is performed.
			Setting to < 1 second is counterproductive — bigcache has a one second resolution.`,
			Value: 1 * time.Minute,
		},
		&cli.IntFlag{
			Name: "cache-max-size",
			Usage: `cache will not allocate more memory than this limit, value in MB
			if value is reached then the oldest entries can be overridden for the new ones
			0 value means no size limit`,
			Value: 1024,
		},
		&cli.IntFlag{
			Name:  "cache-max-entry-size",
			Usage: "Max size of entry in bytes. Used only to calculate initial size for cache shards",
			Value: 64 * 1024,
		},

		// custom settings
		&cli.BoolFlag{
			Name:               "anilibrix-cmpb-mode",
			Usage:              "avoiding 'Cannot POST //public/api/index.php' errors with req rewrite",
			DisableDefaultText: true,
		},
	}

	app.Action = func(c *cli.Context) (e error) {
		var lvl zerolog.Level
		if lvl, e = zerolog.ParseLevel(c.String("log-level")); e != nil {
			log.Fatal().Err(e).Msg("")
		}
		zerolog.SetGlobalLevel(lvl)

		var syslogWriter = io.Discard
		if len(c.String("syslog-server")) != 0 {
			if runtime.GOOS == "windows" {
				log.Error().Msg("sorry, but syslog is not worked for windows; golang does not support syslog for win systems")
				return os.ErrProcessDone
			}
			log.Debug().Msg("connecting to syslog server ...")

			if syslogWriter, e = utils.SetUpSyslogWriter(c); e != nil {
				return
			}
			log.Debug().Msg("syslog connection established; reset zerolog for MultiLevelWriter set ...")

			log = zerolog.New(zerolog.MultiLevelWriter(
				zerolog.ConsoleWriter{Out: dwr},
				syslogWriter,
			)).With().Timestamp().Caller().Logger()

			log.Info().Msg("zerolog reinitialized; starting app...")
		}

		if !fiber.IsChild() {
			log.Info().Msgf("system cpu count %d", runtime.NumCPU())
			log.Info().Msgf("cmdline - %v", os.Args)
			log.Debug().Msgf("environment - %v", os.Environ())
		} else {
			log.Info().Msgf("system cpu count %d", runtime.NumCPU())
			log.Info().Msgf("old cpu count %d", runtime.GOMAXPROCS(1))
			log.Info().Msgf("new cpu count %d", runtime.GOMAXPROCS(1))
		}

		log.Debug().Msgf("%s (%s) builded %s now is ready...", app.Name, version, buildtime)
		return service.NewService(c, &log, syslogWriter).Bootstrap()
	}

	// TODO sort.Sort of Flags uses too much allocs; temporary disabled
	// sort.Sort(cli.FlagsByName(app.Flags))
	sort.Sort(cli.CommandsByName(app.Commands))

	if e := app.Run(os.Args); e != nil {
		log.WithLevel(zerolog.FatalLevel).Msg(e.Error())
		exitcode = 1
	}

	// TODO avoid this shit
	// fucking diode was no `wait` method, so we need to use this `250` shit
	log.Debug().Msg("waiting for diode buf")
	time.Sleep(250 * time.Millisecond)
}

func callerMarshalFunc(_ uintptr, file string, line int) string {
	short := file
	for i := len(file) - 1; i > 0; i-- {
		if file[i] == '/' {
			short = file[i+1:]
			break
		}
	}
	file = short
	return file + ":" + strconv.Itoa(line)
}
