package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/anilibria/alice/internal/anilibria"
	"github.com/anilibria/alice/internal/cache"
	"github.com/anilibria/alice/internal/geoip"
	"github.com/anilibria/alice/internal/utils"
	"github.com/gofiber/fiber/v2"
	futils "github.com/gofiber/fiber/v2/utils"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
	"github.com/valyala/bytebufferpool"
	"github.com/valyala/fasthttp"
)

type Proxy struct {
	client *ProxyClient
	config *ProxyConfig

	cache      *cache.Cache
	geoip      geoip.GeoIPClient
	randomizer *anilibria.Randomizer
}

type ProxyConfig struct {
	dstServer, dstHost string
	apiSecret          []byte
}

func NewProxy(c context.Context) *Proxy {
	cli := c.Value(utils.CKCliCtx).(*cli.Context)

	var randomizer *anilibria.Randomizer
	if c.Value(utils.CKRandomizer) != nil {
		randomizer = c.Value(utils.CKRandomizer).(*anilibria.Randomizer)
	}

	var gip geoip.GeoIPClient
	if c.Value(utils.CKGeoIP) != nil {
		gip = c.Value(utils.CKGeoIP).(geoip.GeoIPClient)
	}

	return &Proxy{
		client: NewClient(cli),
		config: &ProxyConfig{
			dstServer: cli.String("proxy-dst-server"),
			dstHost:   cli.String("proxy-dst-host"),
			apiSecret: []byte(cli.String("cache-api-secret")),
		},

		geoip:      gip,
		randomizer: randomizer,

		cache: c.Value(utils.CKCache).(*cache.Cache),
	}
}

func (m *Proxy) ProxyFiberRequest(c *fiber.Ctx) (e error) {
	req := m.acquireRewritedRequest(c)
	defer fasthttp.ReleaseRequest(req)

	rsp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(rsp)

	if e = m.doRequest(c, req, rsp); e != nil {
		rlog(c).Warn().Msg(req.String())
		return
	}

	if !m.IsCacheBypass(c) {
		return m.cacheAndRespond(c, rsp)
	}

	return m.respondWithStatus(c, rsp.Body(), rsp.StatusCode())
}

func (m *Proxy) ProxyCachedRequest(c *fiber.Ctx) (e error) {
	return m.respondFromCache(c)
}

func (*Proxy) IsCacheBypass(c *fiber.Ctx) bool {
	key := c.Context().UserValue(utils.UVCacheKey).(*Key)
	return key.Len() == 0
}

//
//
//

func (m *Proxy) acquireRewritedRequest(c *fiber.Ctx) *fasthttp.Request {
	req := fasthttp.AcquireRequest()
	c.Context().Request.CopyTo(req)

	req.SetBodyRaw(c.BodyRaw())

	req.Header.SetHost(m.config.dstHost)
	req.UseHostHeader = true

	return req
}

func (m *Proxy) doRequest(c *fiber.Ctx, req *fasthttp.Request, rsp *fasthttp.Response) (e error) {
	if e = m.client.Do(req, rsp); e != nil {
		return
	}

	status, body := rsp.StatusCode(), rsp.Body()

	if status < fasthttp.StatusOK || status >= fasthttp.StatusInternalServerError {
		e = fmt.Errorf("proxy server respond with status %d", status)
		return
	} else if status >= fiber.StatusBadRequest {
		rlog(c).Info().Msgf("status %d detected for request, bypass cache", status)

		m.bypassCache(c)
		return
	}

	if len(body) == 0 {
		e = errors.New("proxy server respond with nil body")
		return
	}

	if cookie := rsp.Header.Peek("Set-Cookie"); len(cookie) != 0 {
		key := c.Context().UserValue(utils.UVCacheKey).(*Key)
		key.Reset()
		c.Response().Header.Set("X-Alice-Cache", "BYPASS")
		c.Response().Header.Set("Set-Cookie", string(cookie))
		// TODO: refactor
	}

	var ok bool
	if ok, e = m.unmarshalApiResponse(c, rsp); e != nil {
		rlog(c).Warn().Msg(e.Error())
		m.bypassCache(c)
	} else if !ok {
		m.bypassCache(c)
	}

	return
}

func (*Proxy) unmarshalApiResponse(c *fiber.Ctx, rsp *fasthttp.Response) (ok bool, e error) {
	var apirsp *utils.ApiResponseWOData
	if apirsp, e = utils.UnmarshalApiResponse(rsp.Body()); e != nil || apirsp == nil {
		rlog(c).Warn().Msg("could not parse legacy api response - " + futils.UnsafeString(rsp.Body()))
		return
	}
	defer utils.ReleaseApiResponseWOData(apirsp)

	if apirsp.Status && (apirsp.Error == nil || apirsp.Error.Code == 0) {
		ok = true
		return
	}

	if apirsp.Error == nil {
		if zerolog.GlobalLevel() <= zerolog.DebugLevel {
			rlog(c).Trace().Msg(futils.UnsafeString(rsp.Body()))
			rlog(c).Trace().Msgf("%+v", apirsp)
			rlog(c).Trace().Msgf("%+v", apirsp.Error)
		}

		rlog(c).Error().Msg("smth is wrong in dst response - status false and err == nil")
		return
	}

	if zerolog.GlobalLevel() <= zerolog.DebugLevel {
		rlog(c).Trace().Msgf("%+v", apirsp)
		rlog(c).Trace().Msgf("%+v", apirsp.Error)
	}

	rlog(c).Info().Msgf("api server respond with %d - %s", apirsp.Error.Code, apirsp.Error.Message)
	return
}

func (*Proxy) bypassCache(c *fiber.Ctx) {
	key := c.Context().UserValue(utils.UVCacheKey).(*Key)
	key.Reset()
}

func (m *Proxy) cacheResponse(c *fiber.Ctx, rsp *fasthttp.Response) (e error) {
	key := c.Context().UserValue(utils.UVCacheKey).(*Key)
	country := m.countryByRemoteIP(c)

	if zerolog.GlobalLevel() < zerolog.InfoLevel {
		rlog(c).Trace().Msgf("Key: %s", key.UnsafeString())
	}

	// cache response body
	if e = m.cache.Cache(country, key.UnsafeString(), rsp.Body()); e != nil {
		return
	}

	// get modified headers for further caching V2
	headers := utils.AcquireHeaderCache()
	defer utils.ReleaseHeaderCache(headers)

	rsp.Header.VisitAll(func(k, v []byte) {
		if len(c.Response().Header.PeekBytes(k)) != 0 {
			return
		}

		if _, ok := utils.HeadersIgnoreList[futils.UnsafeString(k)]; ok {
			return
		}

		headers[futils.UnsafeString(k)] = v
	})

	buf := bytebufferpool.Get()
	defer bytebufferpool.Put(buf)
	defer buf.Reset()

	if buf.B, e = json.Marshal(headers); e != nil {
		return
	}

	if e = m.cache.Cache(country, key.UnsafeHeadersKey(), buf.Bytes()); e != nil {
		return
	}

	return
}

func (m *Proxy) cacheAndRespond(c *fiber.Ctx, rsp *fasthttp.Response) (e error) {
	if e = m.cacheResponse(c, rsp); e == nil {
		return m.respondFromCache(c)
	}

	rlog(c).Warn().Msgf("could not cache the response: %s", e.Error())
	return m.respondWithStatus(c, rsp.Body(), rsp.StatusCode())
}

func (m *Proxy) canRespondFromCache(c *fiber.Ctx) (_ bool, e error) {
	key := c.Context().UserValue(utils.UVCacheKey).(*Key)
	country := m.countryByRemoteIP(c)

	var ok bool
	if ok, e = m.cache.IsCached(country, key.UnsafeString()); e != nil {
		c.Response().Header.Set("X-Alice-Cache", "FAILED")
		rlog(c).Warn().Msg("there is problems with cache driver")
		return
	} else if !ok {
		c.Response().Header.Set("X-Alice-Cache", "MISS")
		return
	}

	c.Response().Header.Set("X-Alice-Cache", "HIT")
	return true, e
}

func (m *Proxy) respondFromCache(c *fiber.Ctx) (e error) {
	key := c.Context().UserValue(utils.UVCacheKey).(*Key)
	country := m.countryByRemoteIP(c)

	buf := bytebufferpool.Get()
	defer bytebufferpool.Put(buf)
	defer buf.Reset()

	if e = m.cache.Write(country, key.UnsafeHeadersKey(), buf); e != nil {
		return
	}

	headers := utils.AcquireHeaderCache()
	defer utils.ReleaseHeaderCache(headers)

	if e = json.Unmarshal(buf.B, &headers); e != nil {
		return
	}

	for name, value := range headers {
		c.Response().Header.SetBytesKV(futils.UnsafeBytes(name), value)
	}

	// get body from cache
	if e = m.cache.Write(country, key.UnsafeString(), c); e != nil {
		return
	}

	return m.respondWithStatus(c, nil, fiber.StatusOK)
}

func (*Proxy) respondWithStatus(c *fiber.Ctx, body []byte, status int) error {
	if body != nil {
		c.Response().SetBodyRaw(body)
	}

	c.Response().Header.SetContentType(fiber.MIMEApplicationJSONCharsetUTF8)
	return c.SendStatus(status)
}

func (m *Proxy) countryByRemoteIP(c *fiber.Ctx) (country string) {
	if m.geoip == nil {
		return
	}

	if !m.geoip.IsReady() {
		rlog(c).Warn().Msg("geoip is not ready now")
		return
	}

	var e error
	if country, e = m.geoip.LookupCountryISO(c.IP()); e != nil {
		rlog(c).Warn().Msg("could not parse ISO for client - " + e.Error())
		return
	}

	if zerolog.GlobalLevel() < zerolog.InfoLevel {
		rlog(c).Trace().Msgf("ip: %s; country: %s", c.IP(), country)
	}

	return
}
