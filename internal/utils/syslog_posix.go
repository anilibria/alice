//go:build !windows && !plan9

package utils

import (
	"io"
	"log/syslog"

	"github.com/urfave/cli/v2"
)

func SetUpSyslogWriter(c *cli.Context) (_ io.Writer, e error) {
	return syslog.Dial(c.String("syslog-proto"), c.String("syslog-server"), syslog.LOG_INFO, c.String("syslog-tag"))
}
