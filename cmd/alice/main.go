package main

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/diode"
	"github.com/urfave/cli/v2"
	"golang.org/x/exp/slices"

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
	cli.VersionPrinter = func(_ *cli.Context) {
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

	app.HideHelpCommand = true
	app.Flags = flagsInitialization(slices.Contains(os.Args, "--expert-mode") == false)

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
	// sort.Sort(cli.CommandsByName(app.Commands))

	if e := app.Run(os.Args); e != nil {
		log.WithLevel(zerolog.FatalLevel).Msg(e.Error())
		exitcode = 1
	}

	// TODO avoid this
	// diode hasn't Wait() method, so we need to use this `250` shit
	log.Trace().Msg("waiting for diode buf")
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
