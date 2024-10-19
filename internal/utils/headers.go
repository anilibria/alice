package utils

import "sync"

var HeaderCache = sync.Pool{
	New: func() any {
		return map[string][]byte{}
	},
}

func AcquireHeaderCache() map[string][]byte {
	return HeaderCache.Get().(map[string][]byte)
}

func ReleaseHeaderCache(hcache map[string][]byte) {
	for key := range hcache {
		delete(hcache, key)
	}

	HeaderCache.Put(hcache)
}

var HeadersIgnoreList = map[string]interface{}{
	"X-Accel-Expires":    nil,
	"Expires":            nil,
	"Cache-Control":      nil,
	"Set-Cookie":         nil,
	"Vary":               nil,
	"X-Accel-Redirect":   nil,
	"X-Accel-Limit-Rate": nil,
	"X-Accel-Buffering":  nil,
	"X-Accel-Charset":    nil,
}
