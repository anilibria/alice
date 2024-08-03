package geoip

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"

	"github.com/anilibria/alice/internal/utils"
	"github.com/klauspost/compress/gzip"
	"github.com/oschwald/maxminddb-golang"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
	"github.com/valyala/fasthttp"

	futils "github.com/gofiber/fiber/v2/utils"
)

type GeoIPHTTPClient struct {
	*maxminddb.Reader

	hclient *fasthttp.Client

	// maxmind
	mmfd       *os.File
	mmurl      string
	mmusername string
	mmpassword string

	appname, tempdir string

	mu      sync.RWMutex
	isReady bool

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
	}

	return gipc.configureHTTPClient(cli)
}

func (m *GeoIPHTTPClient) Bootstrap() {
	var e error

	if m.Reader, e = m.databaseDownload(); e != nil {
		m.log.Error().Msg("could not bootstrap GeoIPHTTPClient - " + e.Error())
		m.abort()
		return
	}
	m.log.Info().Msg("geoip has been initied")

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

func (m *GeoIPHTTPClient) LookupCountryISO(ip string) (string, error) {
	return lookupISOByIP(m.Reader, ip)
}

func (m *GeoIPHTTPClient) IsReady() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.isReady
}

//

func (m *GeoIPHTTPClient) destroy() {
	if e := m.Close(); e != nil {
		m.log.Warn().Msg("could not close maxmind reader - " + e.Error())
	}

	if e := m.mmfd.Close(); e != nil {
		m.log.Warn().Msg("could not close temporary geoip file - " + e.Error())
	}

	if e := os.Remove(m.mmfd.Name()); e != nil {
		m.log.Warn().Msg("could not remove temporary file - " + e.Error())
	}
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

func (m *GeoIPHTTPClient) databaseDownload() (_ *maxminddb.Reader, e error) {
	if m.mmfd, e = m.makeTempFile(); e != nil {
		return
	}
	m.log.Debug().Msgf("file %s has been successfully allocated", m.mmfd.Name())

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	req.Header.SetUserAgent(m.hclient.Name)
	req.SetRequestURI(m.mmurl)
	req.URI().SetUsername(m.mmusername)
	req.URI().SetPassword(m.mmpassword)

	rsp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(rsp)

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
			m.log.Trace().Msg(rsp.String())
			m.log.Error().Msgf("maxmind responded with %d", status)

			e = errors.New("maxmind api returned non 200 response")
			return
		}

		if len(rsp.Body()) == 0 {
			m.log.Trace().Msg(rsp.String())
			m.log.Error().Msg("maxmind responded with empty body")

			e = errors.New("maxmind responded with empty body")
			return
		}

		break
	}

	// GZIP reader
	var rd *gzip.Reader
	if rd, e = gzip.NewReader(bytes.NewBuffer(rsp.Body())); e != nil {
		return
	}

	// TAR reader
	tr := tar.NewReader(rd)
	for {
		var hdr *tar.Header
		hdr, e = tr.Next()

		if e == io.EOF {
			break // End of archive
		} else if e != nil {
			return
		}

		m.log.Trace().Msg("found file in maxmind tar archive - " + hdr.Name)
		if !strings.HasSuffix(hdr.Name, "mmdb") {
			continue
		}

		m.log.Trace().Msg("found mmdb file, copy to temporary file")

		var written int64
		if written, e = io.Copy(m.mmfd, tr); e != nil {
			return
		}

		m.log.Debug().Msgf("parsed response has written in temporary file with %d bytes", written)
		break
	}

	// !!! --geoip-download-sha256-skip
	// !!! --geoip-download-sha256-skip
	// !!! --geoip-download-sha256-skip
	// !!! --geoip-download-sha256-skip

	return maxminddb.Open(m.mmfd.Name())
}
