package proxy

import (
	"context"
	"errors"
	"fmt"

	"github.com/anilibria/alice/internal/cache"
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

	if e = m.cachedResponse(c); e == nil {
		return
	}

	m.log.Trace().Msg("cachedResponse - " + e.Error())

	rsp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(rsp)

	if e = m.doRequest(c, req, rsp); e != nil {
		return
	}

	return m.cacheResponse(c, rsp)
}

func (m *Proxy) cachedResponse(c *fiber.Ctx) (e error) {

	var key []byte
	if key, e = NewExtractor(c).RequestCacheKey(); e != nil {
		return
	}

	// var ok bool
	// if ok, e = m.cache.IsResponseCached(string(key)); e != nil || !ok {
	// 	return
	// }

	var body []byte
	if body, e = m.cache.CachedResponse(string(key)); e != nil {
		return
	}

	fmt.Println(futils.UnsafeString(key))
	fmt.Println(futils.UnsafeString(body))

	c.Response().SetBodyRaw(body)
	c.Response().SetStatusCode(fiber.StatusOK)

	return
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

	if status < fasthttp.StatusOK && status >= fasthttp.StatusInternalServerError {
		e = errors.New(fmt.Sprintf("proxy server respond with status %d", status))
		return
	}

	if len(body) == 0 {
		e = errors.New("proxy server respond with nil body")
		return
	}

	rsp.Header.CopyTo(&c.Response().Header)

	c.Response().SetBodyRaw(body)
	c.Response().SetStatusCode(status)
	return
}

func (m *Proxy) cacheResponse(c *fiber.Ctx, rsp *fasthttp.Response) (e error) {
	var key []byte
	if key, e = NewExtractor(c).RequestCacheKey(); e != nil {
		return
	}

	m.log.Trace().Msgf("Key: %s", futils.UnsafeString(key))
	m.log.Debug().Msgf("Del %d, Hit %d, Miss %d",
		m.cache.Stats().DelHits, m.cache.Stats().Hits, m.cache.Stats().Misses)

	fmt.Println(futils.UnsafeString(rsp.Body()))

	// !! UNSAFE PANIC ??
	// !! UNSAFE PANIC ??
	// futils.UnsafeString(key)
	if e = m.cache.CacheResponse(string(key), rsp.Body()); e != nil {
		return
	}

	return
}
