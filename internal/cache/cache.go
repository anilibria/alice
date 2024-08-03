package cache

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/allegro/bigcache/v3"
	"github.com/anilibria/alice/internal/utils"
	"github.com/klauspost/compress/s2"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
)

type cacheZone uint8

const (
	defaultCache cacheZone = iota
	quarantineCache
)

var zoneHumanize = map[cacheZone]string{
	defaultCache:    "default cache",
	quarantineCache: "quarantine cache",
}

type Cache struct {
	pools      map[cacheZone]*bigcache.BigCache
	quarantine map[string]bool

	log  *zerolog.Logger
	done func() <-chan struct{}
}

func NewCache(c context.Context) (cache *Cache, e error) {
	cli, log :=
		c.Value(utils.CKCliCtx).(*cli.Context),
		c.Value(utils.CKLogger).(*zerolog.Logger)

	cache = new(Cache)
	cache.log, cache.done = log, c.Done

	// create default cache zone
	cache.pools = make(map[cacheZone]*bigcache.BigCache)
	if cache.pools[defaultCache], e = createBigCache(cli, log); e != nil {
		return
	}

	// create quarantine cache zone
	if countries := cli.String("cache-rfngroup-countries"); countries != "" {
		if cache.pools[quarantineCache], e = createBigCache(cli, log); e != nil {
			return
		}

		cache.quarantine = make(map[string]bool)

		for _, country := range strings.Split(countries, ",") {
			if len(country) != 2 {
				log.Warn().Msgf("invalid ISO code, country %s has length more than 2, skipping...", country)
				continue
			}

			cache.quarantine[country] = true
		}

		if len(cache.quarantine) == 0 {
			e = errors.New("BUG: impossible event with cache-rfngroup-countries len()")
			return
		}
	}

	return
}

func createBigCache(cli *cli.Context, log *zerolog.Logger) (*bigcache.BigCache, error) {
	return bigcache.New(context.Background(), bigcache.Config{
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
}

func (m *Cache) cacheZoneByISO(iso string) cacheZone {
	if m.quarantine == nil {
		return defaultCache
	}

	if _, ok := m.quarantine[iso]; ok {
		return quarantineCache
	}

	return defaultCache
}

func (m *Cache) Bootstrap() {
	<-m.done()
	m.log.Info().Msg("internal abort() has been caught; initiate application closing...")

	for zone, cache := range m.pools {
		m.log.Info().Msgf("Serving SUMMARY: DelHits %d, DelMiss %d, Coll %d, Hit %d, Miss %d",
			cache.Stats().DelHits, cache.Stats().DelMisses, cache.Stats().Collisions,
			cache.Stats().Hits, cache.Stats().Misses)

		if e := cache.Close(); e != nil {
			m.log.Error().Msgf("cache zone %d destruct error %s", zone, e.Error())
		}
	}
}

func (m *Cache) IsCached(country, key string) (_ bool, e error) {
	zone := m.cacheZoneByISO(country)

	if _, e = m.pools[zone].Get(key); e != nil && errors.Is(e, bigcache.ErrEntryNotFound) {
		return false, nil
	} else if e != nil {
		return
	}

	return true, nil
}

func (m *Cache) Cache(country, key string, payload []byte) error {
	zone := m.cacheZoneByISO(country)

	return m.setCompressed(zone, key, payload)
}

func (m *Cache) Write(country, key string, w io.Writer) error {
	zone := m.cacheZoneByISO(country)

	return m.writeDecompressed(zone, key, w)
}

func (m *Cache) setCompressed(zone cacheZone, key string, payload []byte) error {
	cmp := s2.Encode(nil, payload)

	if zerolog.GlobalLevel() <= zerolog.DebugLevel {
		m.log.Trace().Msgf("compressed from %d to %d bytes", len(payload), len(cmp))
	}

	return m.pools[zone].Set(key, cmp)
}

func (m *Cache) writeDecompressed(zone cacheZone, key string, w io.Writer) (e error) {
	var cmp, decmp []byte
	if cmp, e = m.pools[zone].Get(key); e != nil {
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
