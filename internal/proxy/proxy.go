package proxy

import (
	"context"
	"errors"
	"fmt"

	"github.com/anilibria/alice/internal/cache"
	"github.com/anilibria/alice/internal/geoip"
	"github.com/anilibria/alice/internal/utils"
	"github.com/gofiber/fiber/v2"
	futils "github.com/gofiber/fiber/v2/utils"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
	"github.com/valyala/fasthttp"
)

type Proxy struct {
	client *ProxyClient
	config *ProxyConfig

	cache *cache.Cache
	geoip geoip.GeoIPClient
}

type ProxyConfig struct {
	dstServer, dstHost string
	apiSecret          []byte
}

func NewProxy(c context.Context) *Proxy {
	cli := c.Value(utils.CKCliCtx).(*cli.Context)

	return &Proxy{
		client: NewClient(cli),
		config: &ProxyConfig{
			dstServer: cli.String("proxy-dst-server"),
			dstHost:   cli.String("proxy-dst-host"),
			apiSecret: []byte(cli.String("cache-api-secret")),
		},

		cache: c.Value(utils.CKCache).(*cache.Cache),
		geoip: c.Value(utils.CKGeoIP).(geoip.GeoIPClient),
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

	rlog(c).Debug().Msg(rsp.String() + rsp.String() + rsp.String() + rsp.String())

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

	if e = m.cache.Cache(country, key.UnsafeString(), rsp.Body()); e != nil {
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
		rlog(c).Warn().Msg("there is problems with cache driver")
		return
	} else if !ok {
		return
	}

	return true, e
}

func (m *Proxy) respondFromCache(c *fiber.Ctx) (e error) {
	key := c.Context().UserValue(utils.UVCacheKey).(*Key)
	country := m.countryByRemoteIP(c)

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
