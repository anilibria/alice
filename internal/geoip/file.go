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
	mu sync.RWMutex
	*maxminddb.Reader

	appname, tempdir string
	skipVerify       bool

	muReady sync.RWMutex
	ready   bool

	log *zerolog.Logger

	done  func() <-chan struct{}
	abort context.CancelFunc
}

func NewGeoIPFileClient(c context.Context, path string) (_ GeoIPClient, e error) {
	cli := c.Value(utils.CKCliCtx).(*cli.Context)

	gipc := &GeoIPFileClient{
		log: c.Value(utils.CKLogger).(*zerolog.Logger),

		done:  c.Done,
		abort: c.Value(utils.CKAbortFunc).(context.CancelFunc),

		appname:    cli.App.Name,
		tempdir:    fmt.Sprintf("%s_%s", cli.App.Name, cli.App.Version),
		skipVerify: cli.Bool("geoip-skip-database-verify"),
	}

	gipc.Reader, e = maxminddb.Open(path)
	return gipc, e
}

func (m *GeoIPFileClient) Bootstrap() {
	if !m.skipVerify {
		if e := m.Reader.Verify(); e != nil {
			m.log.Error().Msg("could not verify maxmind DB - " + e.Error())
			m.abort()
			return
		}
	}

	m.log.Debug().Msg("geoip has been initied")
	m.setReady(true)

	<-m.done()
	m.log.Info().Msg("internal abort() has been caught; initiate application closing...")

	m.setReady(false)
	m.destroy()
}

func (m *GeoIPFileClient) LookupCountryISO(ip string) (string, error) {
	return lookupISOByIP(&m.mu, m.Reader, ip)
}

func (m *GeoIPFileClient) IsReady() bool {
	m.muReady.RLock()
	defer m.muReady.RUnlock()

	return m.ready
}

//

func (m *GeoIPFileClient) destroy() {
	if e := m.Close(); e != nil {
		m.log.Warn().Msg("could not close maxmind reader - " + e.Error())
	}
}

func (m *GeoIPFileClient) setReady(ready bool) {
	m.muReady.Lock()
	defer m.muReady.Unlock()

	m.ready = ready
}
