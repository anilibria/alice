package cache

import (
	"context"

	"github.com/allegro/bigcache/v3"
	"github.com/anilibria/alice/internal/utils"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
)

type Cache struct {
	*bigcache.BigCache

	log  *zerolog.Logger
	done func() <-chan struct{}
}

func NewCache(c context.Context) (cache *Cache, e error) {
	cli, log :=
		c.Value(utils.CKCliCtx).(*cli.Context),
		c.Value(utils.CKLogger).(*zerolog.Logger)

	cache = new(Cache)
	cache.log, cache.done = log, c.Done

	cache.BigCache, e = bigcache.New(context.Background(), bigcache.Config{
		Shards:           cli.Int("cache-shards"),
		HardMaxCacheSize: cli.Int("cache-max-size"),

		LifeWindow:  cli.Duration("cache-life-window"),
		CleanWindow: cli.Duration("cache-clean-window"),

		MaxEntriesInWindow: 1000 * 10 * 60,
		MaxEntrySize:       4096,

		Verbose: cli.Bool("cache-verbose"),

		// Ignored if OnRemove is specified.
		OnRemoveWithReason: cache.bcOnRemoveWithReason,
	})

	return
}

func (m *Cache) Bootstrap() {
	<-m.done()
	m.log.Info().Msg("internal abort() has been caught; initiate application closing...")

	if e := m.Close(); e != nil {
		m.log.Error().Msg(e.Error())
	}
}

func (m *Cache) bcOnRemoveWithReason(key string, entry []byte, reason bigcache.RemoveReason) {
	m.log.Debug().Msgf("cache_manager: key %s was dropped with %d", key, reason)
}

func (m *Cache) CacheResponse(key string, payload []byte) error {
	return m.Set(key, payload)
}

func (m *Cache) CachedResponse(key string) ([]byte, error) {
	return m.Get(key)
}

func (m *Cache) IsResponseCached(key string) (cached bool, e error) {
	if _, e = m.Get(key); e != nil {
		cached = true
		return
	} else if e == bigcache.ErrEntryNotFound {
		e = nil
		return
	} else {
		return
	}
}
