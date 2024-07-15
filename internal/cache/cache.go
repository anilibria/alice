package cache

import (
	"context"
	"errors"
	"io"

	"github.com/allegro/bigcache/v3"
	"github.com/anilibria/alice/internal/utils"
	"github.com/klauspost/compress/s2"
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
		MaxEntrySize:       cli.Int("cache-max-entry-size"),

		// not worked?
		Verbose: zerolog.GlobalLevel() == zerolog.TraceLevel,
		Logger:  log,
	})

	return
}

func (m *Cache) Bootstrap() {
	<-m.done()
	m.log.Info().Msg("internal abort() has been caught; initiate application closing...")

	m.log.Info().Msgf("Serving SUMMARY: DelHits %d, DelMiss %d, Coll %d, Hit %d, Miss %d",
		m.Stats().DelHits, m.Stats().DelMisses, m.Stats().Collisions,
		m.Stats().Hits, m.Stats().Misses)

	if e := m.Close(); e != nil {
		m.log.Error().Msg(e.Error())
	}
}

func (m *Cache) IsCached(key string) (_ bool, e error) {
	if _, e = m.Get(key); e != nil && errors.Is(e, bigcache.ErrEntryNotFound) {
		return false, nil
	} else if e != nil {
		return
	}

	return true, nil
}

func (m *Cache) Cache(key string, payload []byte) error {
	return m.setCompressed(key, payload)
}

func (m *Cache) Write(key string, w io.Writer) error {
	return m.writeDecompressed(key, w)
}

func (m *Cache) setCompressed(key string, payload []byte) error {
	cmp := s2.Encode(nil, payload)

	if zerolog.GlobalLevel() <= zerolog.DebugLevel {
		m.log.Trace().Msgf("compressed from %d to %d bytes", len(payload), len(cmp))
	}

	return m.Set(key, cmp)
}

func (m *Cache) writeDecompressed(key string, w io.Writer) (e error) {
	var cmp, decmp []byte
	if cmp, e = m.Get(key); e != nil {
		return
	}

	if decmp, e = s2.Decode(nil, cmp); e != nil {
		return
	}

	var wrote int
	wrote, e = w.Write(decmp)

	if zerolog.GlobalLevel() <= zerolog.DebugLevel {
		m.log.Trace().Msgf("decompressed from %d to %d bytes, wrote %d bytes", len(cmp), len(decmp), wrote)
	}

	return
}
