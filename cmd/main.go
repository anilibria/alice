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

	"github.com/anilibria/apiv1-cacher/internal/service"
	"github.com/anilibria/apiv1-cacher/utils"
)

var version = "devel" // -ldflags="-X main.version=X.X.X"
var buildtime = "never"

func main() {
	retcode := 0
	defer func() { os.Exit(retcode) }()

	// non-blocking writer
	dwr := diode.NewWriter(os.Stdout, 1000, 10*time.Millisecond, func(missed int) {
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
	cli.VersionFlag = &cli.BoolFlag{Name: "version", Aliases: []string{"V"}}

	app.Name = "apiv1-cacher"
	app.Version = fmt.Sprintf("%s\t%s", version, buildtime)
	app.Authors = []*cli.Author{
		&cli.Author{
			Name:  "MindHunter86",
			Email: "mindhunter86@vkom.cc",
		},
	}
	app.Copyright = "(c) 2024 mindhunter86\nwith love for AniLibria project"
	app.Usage = "AniLibria APIv1 cache service"

	app.Flags = []cli.Flag{
		// common settings
		&cli.StringFlag{
			Name:    "log-level",
			Aliases: []string{"l"},
			Value:   "debug",
			Usage:   "levels: trace, debug, info, warn, err, panic, disabled",
			EnvVars: []string{"LOG_LEVEL"},
		},
		&cli.BoolFlag{
			Name:    "quite",
			Aliases: []string{"q"},
			Usage:   "Flag is equivalent to --log-level=quite",
		},

		// common settings : syslog
		&cli.StringFlag{
			Name:    "syslog-server",
			Value:   "",
			EnvVars: []string{"SYSLOG_ADDRESS"},
		},
		&cli.StringFlag{
			Name:  "syslog-proto",
			Value: "tcp",
		},
		&cli.StringFlag{
			Name:  "syslog-tag",
			Value: "",
		},

		// fiber-server settings
		&cli.StringFlag{
			Name:  "http-listen-addr",
			Usage: "Ex: 127.0.0.1:8080, :8080",
			Value: "127.0.0.1:8080",
		},
		&cli.StringFlag{
			Name:  "http-trusted-proxies",
			Usage: "Ex: 10.0.0.0/8; Separated by comma",
		},
		&cli.BoolFlag{
			Name: "http-prefork",
			Usage: `Enables use of the SO_REUSEPORT socket option;
			if enabled, the application will need to be ran
			through a shell because prefork mode sets environment variables`,
		},
		&cli.BoolFlag{
			Name:  "http-pprof-enable",
			Usage: "enable golang http-pprof methods",
		},

		// limiter settings
		&cli.BoolFlag{
			Name:  "limiter-use-bbolt",
			Usage: "use bbolt key\value file database instead of memory database",
		},
		&cli.IntFlag{
			Name:  "limiter-max-req",
			Value: 200,
		},
		&cli.DurationFlag{
			Name:  "limiter-records-duration",
			Value: 5 * time.Minute,
		},
	}

	app.Action = func(c *cli.Context) (e error) {
		var lvl zerolog.Level
		if lvl, e = zerolog.ParseLevel(c.String("log-level")); e != nil {
			log.Fatal().Err(e).Msg("")
		}

		zerolog.SetGlobalLevel(lvl)
		if c.Bool("quite") {
			zerolog.SetGlobalLevel(zerolog.Disabled)
		}

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
		retcode = 1
	}

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
