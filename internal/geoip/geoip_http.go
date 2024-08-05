package geoip

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/anilibria/alice/internal/utils"
	"github.com/klauspost/compress/gzip"
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

	if m.Reader, e = m.databaseDownload(m.fd); e != nil {
		m.log.Error().Msg("could not bootstrap GeoIPHTTPClient - " + e.Error())
		m.abort()
		return
	}

	if !m.skipVerify {
		if e = m.Reader.Verify(); e != nil {
			m.log.Error().Msg("could not verify maxmind DB - " + e.Error())
			m.abort()
			return
		}
	}

	m.setReady(true)

	m.loop()
	m.setReady(false)

	m.destroy()
}

func (m *GeoIPHTTPClient) LookupCountryISO(ip string) (string, error) {
	return lookupISOByIP(m.Reader, ip)
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
	}

LOOP:
	for {
		select {
		case <-update.C:
			if !m.muUpdate.TryLock() {
				m.log.Error().Msg("could not start the mmdb update, last proccess is not marked as complete")
				update.Reset(m.mmRetryFreq)
				continue
			}
			if !m.IsReady() {
				m.log.Error().Msg("could not start the mmdb update, ready flag is false at this moment")
				update.Reset(m.mmRetryFreq)
				m.muUpdate.Unlock()
				continue
			}

			var newfd *os.File
			newrd, e := m.databaseDownload(newfd)
			if e != nil {
				m.log.Error().Msg("could not start the mmdb update - " + e.Error())
				update.Reset(m.mmRetryFreq)
				m.muUpdate.Unlock()
				continue
			}

			m.rotateActiveDB(newfd, newrd)
			m.muUpdate.Unlock()
		case <-m.done():
			m.log.Info().Msg("internal abort() has been caught; initiate application closing...")
			break LOOP
		}
	}
}

func (m *GeoIPHTTPClient) destroy() {
	if e := m.Close(); e != nil {
		m.log.Warn().Msg("could not close maxmind reader - " + e.Error())
	}

	m.destroyDB(m.fd, m.Reader)
}

func (m *GeoIPHTTPClient) destroyDB(mmfile *os.File, mmreader *maxminddb.Reader) {
	if e := mmreader.Close(); e != nil {
		m.log.Warn().Msg("could not close maxmind reader - " + e.Error())
	}

	if e := mmfile.Close(); e != nil {
		m.log.Warn().Msg("could not close temporary geoip file - " + e.Error())
	}

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
	m.Reader, m.fd = mmreader, mmfile
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

func (m *GeoIPHTTPClient) databaseDownload(tmpfile *os.File) (_ *maxminddb.Reader, e error) {
	if tmpfile, e = m.makeTempFile(); e != nil {
		return
	}
	m.log.Debug().Msgf("file %s has been successfully allocated", tmpfile.Name())

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
			m.log.Info().Msg("maxmind responded sha256 is not changed; mmdb download will be skipped")
			return m.Reader, e
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

		m.log.Debug().Msg("maxmind database sha256 verification passed")
		m.mmLastHash = expectedHash
	}

	if e = m.extractTarGzArchive(tmpfile, bytes.NewBuffer(rsp.Body())); e != nil {
		return
	}

	return maxminddb.Open(tmpfile.Name())
}

func (m *GeoIPHTTPClient) extractTarGzArchive(dst io.Writer, src io.Reader) (e error) {
	var rd *gzip.Reader
	if rd, e = gzip.NewReader(src); e != nil {
		return
	}

	return m.extractTarArchive(dst, rd)
}

func (m *GeoIPHTTPClient) extractTarArchive(dst io.Writer, src io.Reader) (e error) {
	tr := tar.NewReader(src)
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
		if written, e = io.Copy(dst, tr); e != nil { // skipcq: GO-S2110 decompression bomb isn't possible here
			return
		}

		m.log.Debug().Msgf("parsed response has written in temporary file with %d bytes", written)
		break
	}

	return
}

func (*GeoIPHTTPClient) databaseSHA256Verify(payload []byte) (hash []byte) {
	sha := sha256.New()
	sha.Write(payload)

	hash = make([]byte, sha.Size()*2)
	hex.Encode(hash, sha.Sum(nil))

	return
}

func (m *GeoIPHTTPClient) requestSHA256(req *fasthttp.Request) (_ []byte, e error) {
	shareq := m.acquireGeoIPRequest(req)
	defer fasthttp.ReleaseRequest(shareq)

	if !shareq.URI().QueryArgs().Has("suffix") {
		e = errors.New("unknown maxmind url format; suffix arg is missing, sha256 verification is not possible")
		return
	}
	shareq.URI().QueryArgs().Set("suffix", "tar.gz.sha256")

	rsp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(rsp)

	if e = m.requestWithRedirects(shareq, rsp); e != nil {
		return
	}

	if zerolog.GlobalLevel() <= zerolog.DebugLevel {
		m.log.Trace().Msg(rsp.String())
		m.log.Debug().Msgf("maxmind respond with hash - '%s' (string)", futils.UnsafeString(rsp.Body()[:64]))
	}

	hash := make([]byte, 64)
	copy(hash, rsp.Body()[:64])

	return hash, e
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
