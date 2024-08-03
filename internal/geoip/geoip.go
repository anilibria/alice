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

	"github.com/anilibria/alice/internal/utils"
	futils "github.com/gofiber/fiber/v2/utils"
	"github.com/klauspost/compress/gzip"
	"github.com/oschwald/maxminddb-golang"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
	"github.com/valyala/fasthttp"
)

type GeoIPClient struct {
	*maxminddb.Reader

	hclient *fasthttp.Client

	// maxmind
	mmfd       *os.File
	mmurl      string
	mmusername string
	mmpassword string

	appname, tempdir string

	mu       sync.RWMutex
	isFailed bool
	isReady  bool

	log  *zerolog.Logger
	cli  *cli.Context
	done func() <-chan struct{}
}

func NewGeoIPClient(c context.Context) (gipc *GeoIPClient, e error) {
	cli, log :=
		c.Value(utils.CKCliCtx).(*cli.Context),
		c.Value(utils.CKLogger).(*zerolog.Logger)

	if !cli.Bool("geoip-enable") {
		log.Warn().Msg("geoip disabled, all requests will be cached in one zone")
		return
	}

	gipc = &GeoIPClient{
		log:  log,
		done: c.Done,

		appname: cli.App.Name,
		tempdir: fmt.Sprintf("%s_%s", cli.App.Name, cli.App.Version),
	}

	if path := cli.String("geoip-db-path"); path != "" {
		log.Info().Msg("geoip-db-path found, use provided GeoIP db")
		return gipc.withoutHTTP(path)
	}

	log.Info().Msg("geoip-db-path not found, initialize GeoIP downloading...")
	return gipc.withHTTP(cli)
}

func (m *GeoIPClient) Bootstrap() (e error) {
	if m.hclient != nil {
		if m.Reader, e = m.databaseDownload(); e != nil {
			return
		}
	}
	m.log.Info().Msg("geoip has been initied")

	<-m.done()
	return m.Destroy()
}

func (m *GeoIPClient) Destroy() error {
	if e := m.mmfd.Close(); e != nil {
		m.log.Warn().Msg("could not close temporary geoip file - " + e.Error())
	}

	return os.Remove(m.mmfd.Name())
}

func (m *GeoIPClient) withoutHTTP(path string) (_ *GeoIPClient, e error) {
	m.Reader, e = maxminddb.Open(path)
	return
}

func (m *GeoIPClient) withHTTP(c *cli.Context) (_ *GeoIPClient, e error) {
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

	return
}

func (m *GeoIPClient) makeTempFile() (_ *os.File, e error) {
	var fstat fs.FileInfo
	if fstat, e = os.Stat(m.tempdir); e != nil {
		if !os.IsNotExist(e) {
			return
		}

		os.MkdirAll(m.tempdir, 0755)
	} else if !fstat.IsDir() {
		e = errors.New("temporary path is exists and it's not a directory - " + m.tempdir)
		return
	}

	var fd *os.File
	fd, e = os.CreateTemp(m.tempdir, m.appname+"_*")

	return fd, e
}

func (m *GeoIPClient) databaseDownload() (_ *maxminddb.Reader, e error) {
	if m.mmfd, e = m.makeTempFile(); e != nil {
		return
	}
	m.log.Debug().Msgf("file %s has been successfully allocated", m.mmfd.Name())

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	// req.SetRequestURIBytes(m.httpurl.FullURI())
	// req.URI().SetUsername(string(m.httpurl.Username()))
	// req.URI().SetPassword(string(m.httpurl.Password()))
	// req.UseHostHeader = true
	req.Header.SetUserAgent(m.hclient.Name)

	// req.Header.Set("Accept-Encoding", "deflate")
	// req.Header.Set("Connection", "keep-alive")
	// req.Header.Set("Cache-Control", "no-cache")
	// req.Header.Set("Pragma", "no-cache")

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

			req.Header.Set(fasthttp.HeaderAuthorization, "")

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

	var rd *gzip.Reader
	if rd, e = gzip.NewReader(bytes.NewBuffer(rsp.Body())); e != nil {
		return
	}

	var written int64
	if written, e = rd.WriteTo(m.mmfd); e != nil {
		return
	}
	m.log.Debug().Msgf("response was written in temporary file with %d bytes", written)

	// !!! --geoip-download-sha256-skip
	// !!! --geoip-download-sha256-skip
	// !!! --geoip-download-sha256-skip
	// !!! --geoip-download-sha256-skip

	return maxminddb.Open(m.mmfd.Name())
}
