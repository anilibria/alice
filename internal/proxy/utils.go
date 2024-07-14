package proxy

import (
	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
)

func rlog(c *fiber.Ctx) *zerolog.Logger {
	return c.Locals("logger").(*zerolog.Logger)
}

type CustomHeaders uint8

const (
	CHCacheKeyOverride CustomHeaders = 1 << iota
	CHCacheKeyPrefix
	CHCacheKeySuffix
	CHCacheBypass
)

var Stoch = map[string]CustomHeaders{
	"X-CacheKey-Override": CHCacheKeyOverride,
	"X-CacheKey-Prefix":   CHCacheKeyPrefix,
	"X-CacheKey-Suffix":   CHCacheKeySuffix,
	"X-Cache-Bypass":      CHCacheBypass,
}

var CHtos = map[CustomHeaders]string{
	CHCacheKeyOverride: "X-CacheKey-Override",
	CHCacheKeyPrefix:   "X-CacheKey-Prefix",
	CHCacheKeySuffix:   "X-CacheKey-Suffix",
	CHCacheBypass:      "X-Cache-Bypass",
}
