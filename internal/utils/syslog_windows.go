//go:build windows

package utils

import (
	"io"

	"github.com/urfave/cli/v2"
)

func SetUpSyslogWriter(_ *cli.Context) (io.Writer, error) {
	return nil, nil
}
