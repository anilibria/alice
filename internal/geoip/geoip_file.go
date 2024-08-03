package geoip

import (
	"context"
	"fmt"
	"sync"

	"github.com/anilibria/alice/internal/utils"
	"github.com/oschwald/maxminddb-golang"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
)

type GeoIPFileClient struct {
	*maxminddb.Reader

	appname, tempdir string

	mu       sync.RWMutex
	isFailed bool
	isReady  bool

	log  *zerolog.Logger
	done func() <-chan struct{}
}

func NewGeoIPFileClient(c context.Context, path string) (_ GeoIPClient, e error) {
	cli := c.Value(utils.CKCliCtx).(*cli.Context)

	gipc := &GeoIPFileClient{
		log:  c.Value(utils.CKLogger).(*zerolog.Logger),
		done: c.Done,

		appname: cli.App.Name,
		tempdir: fmt.Sprintf("%s_%s", cli.App.Name, cli.App.Version),
	}

	gipc.Reader, e = maxminddb.Open(path)
	return gipc, e
}

func (m *GeoIPFileClient) Bootstrap() {
	m.log.Info().Msg("geoip has been initied")

	<-m.done()
	m.log.Info().Msg("internal abort() has been caught; initiate application closing...")

	m.destroy()
}

func (m *GeoIPFileClient) destroy() {
	if e := m.Close(); e != nil {
		m.log.Warn().Msg("could not close maxmind reader - " + e.Error())
	}
}
