package proxy

import (
	"context"
	"errors"
	"fmt"

	"github.com/anilibria/alice/internal/cache"
	"github.com/anilibria/alice/internal/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
	"github.com/valyala/fasthttp"
)

type Proxy struct {
	client *ProxyClient
	config *ProxyConfig

	cache *cache.Cache

	log *zerolog.Logger
}

type ProxyConfig struct {
	dstServer string
	dstHost   string
}

func NewProxy(c context.Context) *Proxy {
	cli := c.Value(utils.CKCliCtx).(*cli.Context)

	return &Proxy{
		client: NewClient(cli),
		config: &ProxyConfig{
			dstServer: cli.String("proxy-dst-server"),
			dstHost:   cli.String("proxy-dst-host"),
		},

		cache: c.Value(utils.CKCache).(*cache.Cache),

		log: c.Value(utils.CKLogger).(*zerolog.Logger),
	}
}

func (m *Proxy) ProxyFiberRequest(c *fiber.Ctx) (e error) {
	req := m.acquireRewritedRequest(c)
	defer fasthttp.ReleaseRequest(req)

	rsp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(rsp)

	if e = m.doRequest(req, rsp); e != nil {
		return
	}

	return m.cacheAndRespond(c, rsp)
}

func (m *Proxy) ProxyCachedRequest(c *fiber.Ctx) (e error) {
	return m.respondFromCache(c)
}

func (m *Proxy) acquireRewritedRequest(c *fiber.Ctx) *fasthttp.Request {
	req := fasthttp.AcquireRequest()
	c.Context().Request.CopyTo(req)

	req.SetBodyRaw(c.BodyRaw())

	req.Header.SetHost(m.config.dstHost)
	req.UseHostHeader = true

	return req
}

func (m *Proxy) doRequest(req *fasthttp.Request, rsp *fasthttp.Response) (e error) {
	if e = m.client.Do(req, rsp); e != nil {
		return
	}

	status, body := rsp.StatusCode(), rsp.Body()

	if status < fasthttp.StatusOK && status >= fasthttp.StatusInternalServerError {
		e = fmt.Errorf("proxy server respond with status %d", status)
		return
	}

	if len(body) == 0 {
		e = errors.New("proxy server respond with nil body")
	}

	return
}

func (m *Proxy) cacheResponse(c *fiber.Ctx, rsp *fasthttp.Response) (e error) {
	key := c.Context().UserValue(utils.UVCacheKey).(*Key)

	if m.log.GetLevel() <= zerolog.DebugLevel {
		m.log.Trace().Msgf("Key: %s", key.UnsafeString())
		m.log.Debug().Msgf("Del %d, Hit %d, Miss %d",
			m.cache.Stats().DelHits, m.cache.Stats().Hits, m.cache.Stats().Misses)
	}

	if e = m.cache.CacheResponse(key.UnsafeString(), rsp.Body()); e != nil {
		return
	}

	return
}

func (m *Proxy) cacheAndRespond(c *fiber.Ctx, rsp *fasthttp.Response) (e error) {
	if e = m.cacheResponse(c, rsp); e == nil {
		return m.respondFromCache(c)
	}

	m.log.Warn().Msgf("could not cache the response: %s", e.Error())
	return m.respondWithStatus(c, rsp.Body(), rsp.StatusCode())
}

func (m *Proxy) canRespondFromCache(c *fiber.Ctx) (_ bool, e error) {
	key := c.Context().UserValue(utils.UVCacheKey).(*Key)

	var ok bool
	if ok, e = m.cache.IsResponseCached(key.UnsafeString()); e != nil {
		m.log.Warn().Msg("there is problems with cache driver")
		return
	} else if !ok {
		return
	}

	return true, e
}

func (m *Proxy) respondFromCache(c *fiber.Ctx) (e error) {
	key := c.Context().UserValue(utils.UVCacheKey).(*Key)

	var body []byte
	if body, e = m.cache.CachedResponse(key.UnsafeString()); e != nil {
		return
	}

	return m.respondWithStatus(c, body, fiber.StatusOK)
}

func (m *Proxy) respondWithStatus(c *fiber.Ctx, body []byte, status int) error {
	if m.log.GetLevel() <= zerolog.DebugLevel {
		m.log.Debug().Msgf("DelHits %d, DelMiss %d, Coll %d, Hit %d, Miss %d",
			m.cache.Stats().DelHits, m.cache.Stats().DelMisses, m.cache.Stats().Collisions,
			m.cache.Stats().Hits, m.cache.Stats().Misses)
	}

	c.Response().SetBodyRaw(body)
	c.Response().Header.SetContentType(fiber.MIMEApplicationJSONCharsetUTF8)
	return c.SendStatus(status)
}
