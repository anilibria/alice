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

	mu      sync.RWMutex
	isReady bool

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

		appname: cli.App.Name,
		tempdir: fmt.Sprintf("%s_%s", cli.App.Name, cli.App.Version),
	}

	gipc.Reader, e = maxminddb.Open(path)
	return gipc, e
}

func (m *GeoIPFileClient) Bootstrap() {
	m.log.Info().Msg("geoip has been initied")

	var e error
	if e = m.Reader.Verify(); e != nil {
		m.log.Error().Msg("could not verify maxmind DB - " + e.Error())
		m.abort()
		return
	}

	m.mu.Lock()
	m.isReady = true
	m.mu.Unlock()

	<-m.done()
	m.log.Info().Msg("internal abort() has been caught; initiate application closing...")

	m.destroy()
}

func (m *GeoIPFileClient) LookupCountryISO(ip string) (string, error) {
	return lookupISOByIP(m.Reader, ip)
}

func (m *GeoIPFileClient) IsReady() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isReady
}

//

func (m *GeoIPFileClient) destroy() {
	if e := m.Close(); e != nil {
		m.log.Warn().Msg("could not close maxmind reader - " + e.Error())
	}
}
