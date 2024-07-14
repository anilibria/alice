package cache

import (
	"bytes"
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

func (m *Cache) ApiDump(key string) ([]byte, error) {
	return m.Get(key)
}

func (m *Cache) ApiDumpKeys() io.Reader {
	tb := table.NewWriter()
	defer tb.Render()

	buf := bytes.NewBuffer(nil)

	tb.SetOutputMirror(buf)
	tb.AppendHeader(table.Row{
		"timestamp", "hash", "key",
	})

	for iter := m.Iterator(); iter.SetNext(); {
		entry, e := iter.Value()

		if e != nil {
			m.log.Warn().Msg("an error occurred in cache iterator - " + e.Error())
			continue
		}

		tb.AppendRow([]interface{}{
			time.Unix(int64(entry.Timestamp()), 0).Format(time.RFC3339),
			entry.Hash(),
			entry.Key(),
		})

	}

	tb.Style().Options.SeparateRows = true

	tb.SortBy([]table.SortBy{
		{Number: 0, Mode: table.Asc},
	})

	return buf
}

func (m *Cache) ApiPurge(key string) error {
	return m.Delete(key)
}

func (m *Cache) ApiPurgeAll() error {
	return m.Reset()
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
		"number of entries", "capacity (mb)", "hits", "misses", "delhits", "delmisses", "collisions",
	})

	tb.AppendRow([]interface{}{
		m.Len(),
		round(spaceHumanizeMB(m.Capacity()), 2),
		m.Stats().Hits,
		m.Stats().Misses,
		m.Stats().DelHits,
		m.Stats().DelMisses,
		m.Stats().Collisions,
	})

	tb.Style().Options.SeparateRows = true

	return buf
}

func (m *Cache) ApiStatsReset() error {
	return m.ResetStats()
}
