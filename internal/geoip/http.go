package geoip

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/anilibria/alice/internal/utils"
	"github.com/oschwald/maxminddb-golang"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
	"github.com/valyala/fasthttp"

	futils "github.com/gofiber/fiber/v2/utils"
)

type GeoIPHTTPClient struct {
	mu sync.RWMutex
	*maxminddb.Reader
	fd *os.File

	hclient *fasthttp.Client

	// maxmind
	mmurl      string
	mmusername string
	mmpassword string

	mmSkipHashVerify bool
	mmLastHash       []byte

	muUpdate     sync.Mutex
	mmUpdateFreq time.Duration
	mmRetryFreq  time.Duration

	appname, tempdir string
	skipVerify       bool

	muReady sync.RWMutex
	ready   bool

	log *zerolog.Logger

	done  func() <-chan struct{}
	abort context.CancelFunc
}

func NewGeoIPHTTPClient(c context.Context) (_ GeoIPClient, e error) {
	cli := c.Value(utils.CKCliCtx).(*cli.Context)

	gipc := &GeoIPHTTPClient{
		log: c.Value(utils.CKLogger).(*zerolog.Logger),

		done:  c.Done,
		abort: c.Value(utils.CKAbortFunc).(context.CancelFunc),

		appname: cli.App.Name,
		tempdir: fmt.Sprintf("%s_%s", cli.App.Name, cli.App.Version),

		skipVerify: cli.Bool("geoip-skip-database-verify"),

		mmSkipHashVerify: cli.Bool("geoip-download-sha256-skip"),
		mmUpdateFreq:     cli.Duration("geoip-update-frequency"),
		mmRetryFreq:      cli.Duration("geoip-update-retry-frequency"),
	}

	return gipc.configureHTTPClient(cli)
}

func (m *GeoIPHTTPClient) Bootstrap() {
	var e error

	if m.fd, m.Reader, e = m.databaseDownload(); e != nil {
		m.log.Error().Msg("could not bootstrap GeoIPHTTPClient - " + e.Error())
		m.abort()
		return
	}
	defer m.destroyDB(m.fd, m.Reader)
	m.log.Info().Msg("geoip initial downloading has been completed")

	if !m.skipVerify {
		if e = m.Reader.Verify(); e != nil {
			m.log.Error().Msg("could not verify maxmind DB - " + e.Error())
			m.abort()
			return
		}
	}

	m.setReady(true)
	defer m.setReady(false)

	m.loop()
}

func (m *GeoIPHTTPClient) LookupCountryISO(ip string) (string, error) {
	return lookupISOByIP(&m.mu, m.Reader, ip)
}

func (m *GeoIPHTTPClient) IsReady() bool {
	m.muReady.RLock()
	defer m.muReady.RUnlock()
	return m.ready
}

//

func (m *GeoIPHTTPClient) loop() {
	m.log.Debug().Msg("initiate geoip db update loop...")
	defer m.log.Debug().Msg("geoip db update loop has been closed")

	var update *time.Timer
	if m.mmUpdateFreq != 0 {
		update = time.NewTimer(m.mmUpdateFreq)
		m.log.Info().Msgf("geoip database updater enabled; update period - %s", m.mmUpdateFreq.String())
	}

LOOP:
	for {
		select {
		case <-update.C:
			update.Stop()

			if !m.muUpdate.TryLock() {
				m.log.Error().Msg("could not start the mmdb update, last process is not marked as complete")
				update.Reset(m.mmRetryFreq)
				continue
			}
			if !m.IsReady() {
				m.log.Error().Msg("could not start the mmdb update, ready flag is false at this moment")
				update.Reset(m.mmRetryFreq)
				m.muUpdate.Unlock()
				continue
			}
			m.log.Info().Msg("starting geoip database update")
			m.log.Debug().Msg("geoip database update, downloading...")
			defer m.log.Info().Msg("geoip database update, finished")

			newfd, newrd, e := m.databaseDownload()
			if e != nil && newfd != nil && newrd != nil { // update is not required
				m.log.Info().Msg(e.Error())
				update.Reset(m.mmUpdateFreq)
				m.muUpdate.Unlock()
				continue
			} else if e != nil {
				m.log.Error().Msg("could update the mmdb - " + e.Error())
				update.Reset(m.mmRetryFreq)
				m.muUpdate.Unlock()
				continue
			}

			m.log.Trace().Msg("geoip database update, old mmdb - " + m.fd.Name())
			m.log.Trace().Msg("geoip database update, new mmdb - " + newfd.Name())

			m.log.Debug().Msg("geoip database update, rotating...")
			m.rotateActiveDB(newfd, newrd)

			if !m.skipVerify {
				m.log.Debug().Msg("geoip database update, verifying...")
				m.Verify()
			}

			update.Reset(m.mmUpdateFreq)
			m.muUpdate.Unlock()
		case <-m.done():
			m.log.Info().Msg("internal abort() has been caught; initiate application closing...")
			break LOOP
		}
	}
}

func (m *GeoIPHTTPClient) destroyDB(mmfile *os.File, mmreader *maxminddb.Reader) {
	m.log.Trace().Msg("geoip database destroy, maxmind closing...")
	if e := mmreader.Close(); e != nil {
		m.log.Warn().Msg("could not close maxmind reader - " + e.Error())
	}

	m.log.Trace().Msg("geoip database destroy, mmdb closing...")
	if e := mmfile.Close(); e != nil {
		m.log.Warn().Msg("could not close temporary geoip file - " + e.Error())
	}

	m.log.Trace().Msg("geoip database destroy, mmdb removing...")
	if e := os.Remove(mmfile.Name()); e != nil {
		m.log.Warn().Msg("could not remove temporary file - " + e.Error())
	}
}

func (m *GeoIPHTTPClient) rotateActiveDB(mmfile *os.File, mmreader *maxminddb.Reader) {
	m.setReady(false)
	defer m.setReady(true)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.destroyDB(m.fd, m.Reader)
	m.fd, m.Reader = mmfile, mmreader
}

func (m *GeoIPHTTPClient) setReady(ready bool) {
	m.muReady.Lock()
	m.ready = ready
	m.muReady.Unlock()
}

func (m *GeoIPHTTPClient) configureHTTPClient(c *cli.Context) (_ GeoIPClient, e error) {
	rrl := fasthttp.AcquireURI()
	defer fasthttp.ReleaseURI(rrl)

	if e = rrl.Parse(nil, futils.UnsafeBytes(c.String("geoip-maxmind-permalink"))); e != nil {
		return
	}
	m.mmurl = rrl.String()

	var creds []string
	if creds = strings.Split(c.String("geoip-maxmind-license"), ":"); len(creds) != 2 {
		e = errors.New("license format is not valid; it must be formated as `client_id:key`")
		return
	} else if len(creds[0]) == 0 || len(creds[1]) == 0 {
		e = errors.New("license id or key is empty; record must be formated as `client_id:key`")
		return
	}
	m.mmusername, m.mmpassword = creds[0], creds[1]

	m.hclient = &fasthttp.Client{
		// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/User-Agent#crawler_and_bot_ua_strings
		Name: fmt.Sprintf("Mozilla/5.0 (compatible; %s/%s; +https://anilibria.top/support)",
			c.App.Name, c.App.Version),

		DisableHeaderNamesNormalizing: true,
		DisablePathNormalizing:        true,
		NoDefaultUserAgentHeader:      false,

		// TODO
		// ? timeouts
		// ? dns cache
		// ? keep alive
	}

	return m, e
}

func (m *GeoIPHTTPClient) makeTempFile() (_ *os.File, e error) {
	temppath := fmt.Sprintf("%s/%s", os.TempDir(), m.tempdir)
	var fstat fs.FileInfo
	if fstat, e = os.Stat(temppath); e != nil {
		if !os.IsNotExist(e) {
			return
		}

		os.MkdirAll(temppath, 0700)
	} else if !fstat.IsDir() {
		e = errors.New("temporary path is exists and it's not a directory - " + temppath)
		return
	}

	var fd *os.File
	fd, e = os.CreateTemp(temppath, m.appname+"_*.mmdb")

	return fd, e
}

func (m *GeoIPHTTPClient) acquireGeoIPRequest(parent *fasthttp.Request) (req *fasthttp.Request) {
	req = fasthttp.AcquireRequest()

	if parent != nil {
		parent.CopyTo(req)
	}

	req.SetRequestURI(m.mmurl)
	req.URI().SetUsername(m.mmusername)
	req.URI().SetPassword(m.mmpassword)

	req.Header.SetUserAgent(m.hclient.Name)

	return
}

func (m *GeoIPHTTPClient) requestWithRedirects(req *fasthttp.Request, rsp *fasthttp.Response) (e error) {
	for maxRedirects := 5; ; maxRedirects-- {
		if maxRedirects == 0 {
			e = errors.New("maxmind responded with too many redirects, redirects count exceeded")
			return
		}

		m.log.Trace().Msg(req.String())
		if e = m.hclient.Do(req, rsp); e != nil {
			return
		}

		status := rsp.StatusCode()
		if fasthttp.StatusCodeIsRedirect(status) {
			m.log.Trace().Msg(rsp.String())
			m.log.Debug().Msgf("maxmind responded with redirect %d, go to %s", status,
				futils.UnsafeString(rsp.Header.Peek(fasthttp.HeaderLocation)))

			req.Header.Del(fasthttp.HeaderAuthorization)

			req.SetRequestURIBytes(rsp.Header.Peek(fasthttp.HeaderLocation))
			req.URI().Parse(nil, rsp.Header.Peek(fasthttp.HeaderLocation))
			continue
		}

		if status != fasthttp.StatusOK {
			e = fmt.Errorf("maxmind api returned %d response", status)
			m.log.Trace().Msg(rsp.String())
			return
		}

		if len(rsp.Body()) == 0 {
			e = errors.New("maxmind responded with empty body")
			m.log.Trace().Msg(rsp.String())
			return
		}

		break
	}

	return
}

func (m *GeoIPHTTPClient) databaseDownload() (fd *os.File, _ *maxminddb.Reader, e error) {
	if fd, e = m.makeTempFile(); e != nil {
		return
	}
	m.log.Info().Msgf("file %s has been successfully allocated", fd.Name())

	req := m.acquireGeoIPRequest(nil)
	defer fasthttp.ReleaseRequest(req)

	rsp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(rsp)

	var expectedHash []byte
	if !m.mmSkipHashVerify {
		if expectedHash, e = m.requestSHA256(req); e != nil {
			return
		}

		if len(m.mmLastHash) != 0 && bytes.Equal(expectedHash, m.mmLastHash) {
			e = errors.New("maxmind responded sha256 is not changed; mmdb download will be skipped")
			return m.fd, m.Reader, e
		}
	}

	if e = m.requestWithRedirects(req, rsp); e != nil {
		return
	}

	if !m.mmSkipHashVerify {
		var responseHash []byte
		if responseHash = m.databaseSHA256Verify(rsp.Body()); len(responseHash) == 0 {
			e = errors.New("databases SHA256 verification returns an empty hash")
			return
		}

		if !bytes.Equal(responseHash, expectedHash) {
			e = errors.New("maxmind databases verification not passed, database could not be updated")
			return
		}

		m.log.Info().Msg("maxmind database sha256 verification passed")
		m.mmLastHash = expectedHash
	}

	if e = extractTarGzArchive(m.log, fd, bytes.NewBuffer(rsp.Body())); e != nil {
		return
	}

	var reader *maxminddb.Reader
	reader, e = maxminddb.Open(fd.Name())

	return fd, reader, e
}
