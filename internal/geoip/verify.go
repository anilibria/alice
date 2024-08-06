package geoip

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	futils "github.com/gofiber/fiber/v2/utils"
	"github.com/rs/zerolog"
	"github.com/valyala/fasthttp"
)

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
