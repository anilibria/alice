package main

import (
	"time"

	"github.com/urfave/cli/v2"
)

func flagsInitialization(expertMode bool) []cli.Flag {
	return []cli.Flag{
		// common settings
		&cli.StringFlag{
			Name:    "log-level",
			Value:   "info",
			Usage:   "levels: trace, debug, info, warn, err, panic, disabled",
			Aliases: []string{"l"},
			EnvVars: []string{"LOG_LEVEL"},
		},
		&cli.BoolFlag{
			Name:               "expert-mode",
			Usage:              "show hidden flags",
			DisableDefaultText: true,
		},

		// common settings : syslog
		&cli.StringFlag{
			Name:     "syslog-server",
			Category: "Syslog settings",
			Usage:    "syslog server (optional); syslog sender is not used if value is empty",
			Value:    "",
			EnvVars:  []string{"SYSLOG_ADDRESS"},
		},
		&cli.StringFlag{
			Name:     "syslog-proto",
			Category: "Syslog settings",
			Usage:    "syslog protocol (optional); tcp or udp is possible",
			Value:    "tcp",
			EnvVars:  []string{"SYSLOG_PROTO"},
		},
		&cli.StringFlag{
			Name:     "syslog-tag",
			Category: "Syslog settings",
			Usage:    "optional setting; more information in syslog RFC",
			Value:    "",
			Hidden:   expertMode,
		},

		// fiber-server settings
		&cli.StringFlag{
			Name:     "http-listen-addr",
			Category: "HTTP server settings",
			Usage:    "format - 127.0.0.1:8080, :8080",
			Value:    "127.0.0.1:8080",
		},
		&cli.StringFlag{
			Name:     "http-trusted-proxies",
			Category: "HTTP server settings",
			Usage:    "format - 192.168.0.0/16; can be separated by comma",
		},
		&cli.StringFlag{
			Name:     "http-realip-header",
			Category: "HTTP server settings",
			Value:    "X-Real-Ip",
		},
		&cli.BoolFlag{
			Name:     "http-prefork",
			Category: "HTTP server settings",
			Usage: `enables use of the SO_REUSEPORT socket option;
			if enabled, the application will need to be ran
			through a shell because prefork mode sets environment variables;
			EXPERIMENTAL! USE CAREFULLY!`,
			Hidden:             expertMode,
			DisableDefaultText: true,
		},
		&cli.DurationFlag{
			Name:     "http-read-timeout",
			Category: "HTTP server settings",
			Value:    10 * time.Second,
		},
		&cli.DurationFlag{
			Name:     "http-write-timeout",
			Category: "HTTP server settings",
			Value:    5 * time.Second,
		},
		&cli.DurationFlag{
			Name:     "http-idle-timeout",
			Category: "HTTP server settings",
			Value:    10 * time.Minute,
		},
		&cli.BoolFlag{
			Name:               "http-pprof-enable",
			Category:           "HTTP server settings",
			Usage:              "enable golang http-pprof methods",
			DisableDefaultText: true,
		},
		&cli.StringFlag{
			Name:     "http-pprof-prefix",
			Category: "HTTP server settings",
			Usage:    "it should start with (but not end with) a slash. Example: '/test'",
			EnvVars:  []string{"PPROF_PREFIX"},
		},
		&cli.StringFlag{
			Name:     "http-pprof-secret",
			Category: "HTTP server settings",
			Usage:    "define static secret in x-pprof-secret header for avoiding unauthorized access",
			EnvVars:  []string{"PPROF_SECRET"},
		},

		// limiter settings
		&cli.BoolFlag{
			Name:               "limiter-enable",
			Category:           "Limiter settings",
			Hidden:             expertMode,
			DisableDefaultText: true,
		},
		&cli.BoolFlag{
			Name:               "limiter-use-bbolt",
			Category:           "Limiter settings",
			Usage:              "use bbolt key\value file database instead of memory database",
			Hidden:             expertMode,
			DisableDefaultText: true,
		},
		&cli.BoolFlag{
			Name:               "limiter-bbolt-reset",
			Category:           "Limiter settings",
			Usage:              "if bbolt used as storage, reset all limited IPs on startup",
			Hidden:             expertMode,
			DisableDefaultText: true,
		},
		&cli.IntFlag{
			Name:     "limiter-max-req",
			Category: "Limiter settings",
			Hidden:   true,
			Value:    200,
		},
		&cli.DurationFlag{
			Name:     "limiter-records-duration",
			Category: "Limiter settings",
			Hidden:   true,
			Value:    5 * time.Minute,
		},

		// proxy settings
		&cli.StringFlag{
			Name:     "proxy-dst-server",
			Category: "Proxy settings",
			Usage:    "destination server",
			Value:    "127.0.0.1:36080",
		},
		&cli.StringFlag{
			Name:     "proxy-dst-host",
			Category: "Proxy settings",
			Usage:    "request Host header for dst server",
			Value:    "api.anilibria.tv",
		},
		&cli.DurationFlag{
			Name:     "proxy-read-timeout",
			Category: "Proxy settings",
			Value:    10 * time.Second,
		},
		&cli.DurationFlag{
			Name:     "proxy-write-timeout",
			Category: "Proxy settings",
			Value:    5 * time.Second,
		},
		&cli.DurationFlag{
			Name:     "proxy-conn-timeout",
			Category: "Proxy settings",
			Usage:    "force connection rotation after this `time`",
			Value:    10 * time.Minute,
		},
		&cli.DurationFlag{
			Name:     "proxy-idle-timeout",
			Category: "Proxy settings",
			Value:    5 * time.Minute,
		},
		&cli.IntFlag{
			Name:     "proxy-max-idle-conn",
			Category: "Proxy settings",
			Value:    256,
		},
		&cli.IntFlag{
			Name:     "proxy-max-conns-per-host",
			Category: "Proxy settings",
			Value:    256,
		},
		&cli.DurationFlag{
			Name:     "proxy-dns-cache-dur",
			Category: "Proxy settings",
			Value:    1 * time.Minute,
		},
		&cli.IntFlag{
			Name:     "proxy-tcpdial-concurr",
			Category: "Proxy settings",
			Usage:    "0 - unlimited",
			Value:    0,
		},

		// cache settings
		&cli.StringFlag{
			Name:     "cache-api-secret",
			Category: "Cache settings",
			Usage:    "define static secret in x-api-secret header for avoiding unauthorized access",
			Value:    "secret",
		},
		&cli.IntFlag{
			Name:     "cache-shards",
			Category: "Cache settings",
			Usage:    "number of shards (must be a power of 2)",
			Value:    512,
		},
		&cli.DurationFlag{
			Name:     "cache-life-window",
			Category: "Cache settings",
			Usage:    "time after which entry can be evicted",
			Value:    10 * time.Minute,
		},
		&cli.DurationFlag{
			Name:     "cache-clean-window",
			Category: "Cache settings",
			Usage: `interval between removing expired entries (clean up)
			If set to <= 0 then no action is performed.
			Setting to < 1 second is counterproductive — bigcache has a one second resolution.`,
			Value: 1 * time.Minute,
		},
		&cli.IntFlag{
			Name:     "cache-max-size",
			Category: "Cache settings",
			Usage: `cache will not allocate more memory than this limit, value in MB;
			if value is reached then the oldest entries can be overridden for the new ones;
			0 value means no size limit; if cache-rfngroup-countries is used, then a second pool with the
			same size will be created, so that the total amount of allocated memory will be X*2`,
			Value: 1024,
		},
		&cli.IntFlag{
			Name:     "cache-max-entry-size",
			Category: "Cache settings",
			Usage:    "max size of entry in bytes. Used only to calculate initial size for cache shards",
			Value:    64 * 1024,
		},
		&cli.StringFlag{
			Name:     "cache-rfngroup-countries",
			Category: "Cache settings",
			Usage: `additional quarantine cache group for filtered responses by backend;
			for quarantine countries use ISO identifier; multiple comma-separated values are supported;
			for quarantine group all settings will by copied from default pool, be careful with 
			cache-max-size; if empty - default cache pool will be used; Example: RU,UA,BY,KZ`,
		},

		// geoip settings
		&cli.BoolFlag{
			Name:     "geoip-enable",
			Category: "GeoIP",
		},
		&cli.StringFlag{
			Name:     "geoip-db-path",
			Category: "GeoIP",
			Usage:    "if path is not empty, geoip downloading will be skipped",
		},
		&cli.StringFlag{
			Name:     "geoip-maxmind-license",
			Category: "GeoIP",
			Usage:    "clientid:key",
			EnvVars:  []string{"GEOIP_MAXMIND_LICENSE"},
		},
		&cli.StringFlag{
			Name:     "geoip-maxmind-permalink",
			Category: "GeoIP",
			Value:    "https://download.maxmind.com/geoip/databases/GeoLite2-Country/download?suffix=tar.gz",
			// "https://download.maxmind.com/geoip/databases/GeoLite2-Country/download?suffix=tar.gz.sha256"
		},
		&cli.BoolFlag{
			Name:     "geoip-download-sha256-skip",
			Category: "GeoIP",
			Usage: `sha256 helps to check database contents of the mmdb database
			and avoid unnecessary requests to MaxMind CDN`,
			DisableDefaultText: true,
		},
		&cli.DurationFlag{
			Name:     "geoip-update-frequency",
			Category: "GeoIP",
			Usage: `when geoip-maxmind-permalink is selected and geoip-db-path is empty,
			once within a certain 'PERIOD' of time app will update the geoip database;
			do not forget about maxmind download limits; set to 0s if want to disable`,
			Value: 24 * time.Hour,
		},
		&cli.DurationFlag{
			Name:     "geoip-update-retry-frequency",
			Category: "GeoIP",
			Value:    1 * time.Hour,
		},
		&cli.BoolFlag{
			Name:               "geoip-skip-database-verify",
			Category:           "GeoIP",
			Usage:              "skip mmdb contents validation by vendor function db.Verify()",
			DisableDefaultText: true,
		},

		// anilibria release randomizer
		&cli.BoolFlag{
			Name:     "randomizer-enable",
			Category: "Release randomizer",
			Usage: `alice has its own function for randomizing releases; it optimizes random_release (apiv1)
			and public/random.php (www site), ensuring the high performance of these methods;
			if disabled, all reuqests will be cached in shared cache pool with another methods`,
		},
		&cli.StringFlag{
			Name:     "randomizer-redis-host",
			Category: "Release randomizer",
			Value:    "127.0.0.1:6279",
		},
		&cli.StringFlag{
			Name:     "randomizer-redis-password",
			Category: "Release randomizer",
			Value:    "",
		},
		&cli.IntFlag{
			Name:     "randomizer-redis-database",
			Category: "Release randomizer",
			Value:    0,
		},
		&cli.BoolFlag{
			Name:     "randomizer-redis-zstd-enable",
			Category: "Release randomizer",
			Usage:    "enable redis payload decompression with zstd algo",
		},
		&cli.StringFlag{
			Name:     "randomizer-releaseskey",
			Category: "Release randomizer",
			Usage:    "the feature of the legacy",
			Value:    "apiInfo",
			Hidden:   expertMode,
		},
		&cli.DurationFlag{
			Name:     "randomizer-update-frequency",
			Category: "Release randomizer",
			Value:    5 * time.Minute,
		},
		&cli.DurationFlag{
			Name:     "randomizer-update-frequency-onerror",
			Category: "Release randomizer",
			Value:    1 * time.Minute,
			Hidden:   expertMode,
		},
		&cli.DurationFlag{
			Name:     "randomizer-update-frequency-bootstrap",
			Category: "Release randomizer",
			Value:    5 * time.Second,
			Hidden:   expertMode,
		},
		&cli.IntFlag{
			Name:   "randomizer-random-fetch-tries",
			Usage:  "possible errors (tries) for fetching random release",
			Value:  10,
			Hidden: expertMode,
		},
		&cli.IntFlag{
			Name:     "redis-client-maxretries",
			Category: "Release randomizer",
			Hidden:   expertMode,
			Value:    3,
		},
		&cli.DurationFlag{
			Name:     "redis-client-dialtimeout",
			Category: "Release randomizer",
			Hidden:   expertMode,
			Value:    5 * time.Second,
		},
		&cli.DurationFlag{
			Name:     "redis-client-readtimeout",
			Category: "Release randomizer",
			Hidden:   expertMode,
			Value:    3 * time.Second,
		},
		&cli.DurationFlag{
			Name:     "redis-client-writetimeout",
			Category: "Release randomizer",
			Hidden:   expertMode,
			Value:    3 * time.Second,
		},

		// custom settings
		&cli.BoolFlag{
			Name:               "anilibrix-cmpb-mode",
			Category:           "Feature flags",
			Usage:              "avoiding 'Cannot POST //public/api/index.php' errors with req rewrite",
			Hidden:             expertMode,
			DisableDefaultText: true,
		},
	}

}
