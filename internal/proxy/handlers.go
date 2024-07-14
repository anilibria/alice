package proxy

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

func (m *Proxy) IsRequestCached(c *fiber.Ctx) (ok bool) {
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

// intrnal api handlers

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
	if e = m.cache.ApiStatsReset(); e != nil {
		return fiber.NewError(fiber.StatusBadRequest, "key could not be empty")
	}

	return respondPlainWithStatus(c, fiber.StatusAccepted)
}

func (m *Proxy) HandleCacheDump(c *fiber.Ctx) (e error) {
	var cachekey string
	if cachekey = c.Query("key"); cachekey == "" {
		return fiber.NewError(fiber.StatusBadRequest, "key could not be empty")
	}

	var payload []byte
	if payload, e = m.cache.ApiDump(cachekey); e != nil {
		return fiber.NewError(fiber.StatusInternalServerError, e.Error())
	}

	c.Response().SetBodyRaw(payload)
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

	if e = m.cache.ApiPurge(cachekey); e != nil {
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
