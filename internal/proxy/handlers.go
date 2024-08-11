package proxy

import (
	"bytes"
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func (m *Proxy) IsRequestCached(c *fiber.Ctx) (ok bool) {
	if m.IsCacheBypass(c) {
		return true
	}

	var e error
	if ok, e = m.canRespondFromCache(c); e != nil {
		rlog(c).Warn().Msg(e.Error())
	}

	return !ok
}

func (m *Proxy) HandleProxyToCache(c *fiber.Ctx) (e error) {
	if e = m.ProxyCachedRequest(c); e != nil {
		return c.Next()
	}

	return
}

func (m *Proxy) HandleProxyToDst(c *fiber.Ctx) (e error) {
	if e = m.ProxyFiberRequest(c); e != nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, e.Error())
	}

	return
}

func (m *Proxy) HandleRandomRelease(c *fiber.Ctx) (e error) {
	if m.randomizer == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "BUG! randomizer is not initialized")
	}

	var release string
	if release = m.randomizer.Randomize(); release == "" {
		return fiber.NewError(fiber.StatusServiceUnavailable,
			"an error occured in randomizer, maybe it's not ready yet")
	}

	if bytes.Equal(c.Request().PostArgs().Peek("js"), []byte("1")) {
		fmt.Fprintln(c, release)
		return respondPlainWithStatus(c, fiber.StatusOK)
	}

	c.Response().Header.Set(fiber.HeaderLocation, "/release/"+release+".html")
	return respondPlainWithStatus(c, fiber.StatusFound)
}

// internal api handlers

func respondPlainWithStatus(c *fiber.Ctx, status int) error {
	c.Set(fiber.HeaderContentType, fiber.MIMETextPlainCharsetUTF8)
	return c.SendStatus(status)
}

// func (m *Proxy) HandleCacheStats(c *fiber.Ctx) (_ error) { return }

func (m *Proxy) HandleCacheStats(c *fiber.Ctx) (_ error) {
	fmt.Fprintln(c, m.cache.ApiStats())
	return respondPlainWithStatus(c, fiber.StatusOK)
}

func (m *Proxy) HandleCacheStatsReset(c *fiber.Ctx) (e error) {
	var country string
	if country = c.Query("country"); country == "" {
		return fiber.NewError(fiber.StatusBadRequest, "key could not be empty")
	}

	if e = m.cache.ApiStatsReset(country); e != nil {
		return fiber.NewError(fiber.StatusBadRequest, "key could not be empty")
	}

	return respondPlainWithStatus(c, fiber.StatusAccepted)
}

func (m *Proxy) HandleCacheDump(c *fiber.Ctx) (e error) {
	var cachekey string
	if cachekey = c.Query("key"); cachekey == "" {
		return fiber.NewError(fiber.StatusBadRequest, "key could not be empty")
	}

	var country string
	if country = c.Query("country"); country == "" {
		return fiber.NewError(fiber.StatusBadRequest, "key could not be empty")
	}

	if e = m.cache.ApiDump(country, cachekey, c); e != nil {
		return fiber.NewError(fiber.StatusInternalServerError, e.Error())
	}

	return respondPlainWithStatus(c, fiber.StatusOK)
}

func (m *Proxy) HandleCacheDumpKeys(c *fiber.Ctx) (_ error) {
	fmt.Fprintln(c, m.cache.ApiDumpKeys())
	return respondPlainWithStatus(c, fiber.StatusOK)
}

func (m *Proxy) HandleCachePurge(c *fiber.Ctx) (e error) {
	var cachekey string
	if cachekey = c.Query("key"); cachekey == "" {
		return fiber.NewError(fiber.StatusBadRequest, "key could not be empty")
	}

	var country string
	if country = c.Query("country"); country == "" {
		return fiber.NewError(fiber.StatusBadRequest, "key could not be empty")
	}

	if e = m.cache.ApiPurge(country, cachekey); e != nil {
		return fiber.NewError(fiber.StatusInternalServerError, e.Error())
	}

	return respondPlainWithStatus(c, fiber.StatusAccepted)
}

func (m *Proxy) HandleCachePurgeAll(c *fiber.Ctx) (e error) {
	if e = m.cache.ApiPurgeAll(); e != nil {
		return fiber.NewError(fiber.StatusInternalServerError, e.Error())
	}

	return respondPlainWithStatus(c, fiber.StatusAccepted)
}
