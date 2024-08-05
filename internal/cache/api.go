package cache

import (
	"bytes"
	"errors"
	"io"
	"math"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
)

type ApiCacheEntry struct {
	Timestamp uint64
	Hash      uint64
	Key       string
}

func (m *Cache) ApiDump(country, key string, w io.Writer) error {
	return m.Write(country, key, w)
}

func (m *Cache) ApiDumpKeys() io.Reader {
	tb := table.NewWriter()
	defer tb.Render()

	buf := bytes.NewBuffer(nil)

	tb.SetOutputMirror(buf)
	tb.AppendHeader(table.Row{
		"timestamp", "zone", "hash", "key",
	})

	for zone, cache := range m.pools {
		for iter := cache.Iterator(); iter.SetNext(); {
			entry, e := iter.Value()

			if e != nil {
				m.log.Warn().Msg("an error occurred in cache iterator - " + e.Error())
				continue
			}

			tb.AppendRow([]interface{}{
				time.Unix(int64(entry.Timestamp()), 0).Format(time.RFC3339),
				zoneHumanize[zone],
				entry.Hash(),
				entry.Key(),
			})

		}
	}

	tb.Style().Options.SeparateRows = true

	tb.SortBy([]table.SortBy{
		{Number: 0, Mode: table.Asc},
	})

	return buf
}

func (m *Cache) ApiPurge(country, key string) error {
	zone := m.cacheZoneByISO(country)
	return m.pools[zone].Delete(key)
}

func (m *Cache) ApiPurgeAll() error {
	var errs string
	for _, cache := range m.pools {
		if e := cache.Reset(); e != nil {
			m.log.Error().Msg("an error occurred while resetting the cache - " + e.Error())
			errs = errs + "\n" + e.Error()
		}
	}

	if errs != "" {
		return errors.New(errs)
	}

	return nil
}

func (m *Cache) ApiStats() io.Reader {
	tb := table.NewWriter()
	defer tb.Render()

	spaceHumanizeMB := func(bytes int) float64 {
		return float64(bytes) / 1024 / 1024
	}

	round := func(val float64, precision uint) float64 {
		ratio := math.Pow(10, float64(precision))
		return math.Round(val*ratio) / ratio
	}

	buf := bytes.NewBuffer(nil)

	tb.SetOutputMirror(buf)
	tb.AppendHeader(table.Row{
		"zone", "number of entries", "capacity (mb)", "hits", "misses", "delhits", "delmisses", "collisions",
	})

	for zone, cache := range m.pools {
		tb.AppendRow([]interface{}{
			zoneHumanize[zone],
			cache.Len(),
			round(spaceHumanizeMB(cache.Capacity()), 2),
			cache.Stats().Hits,
			cache.Stats().Misses,
			cache.Stats().DelHits,
			cache.Stats().DelMisses,
			cache.Stats().Collisions,
		})
	}

	tb.Style().Options.SeparateRows = true

	tb.SortBy([]table.SortBy{
		{Number: 0, Mode: table.Asc},
	})

	return buf
}

func (m *Cache) ApiStatsReset(country string) error {
	zone := m.cacheZoneByISO(country)
	return m.pools[zone].ResetStats()
}
