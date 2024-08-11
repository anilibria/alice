package anilibria

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"sync"
	"time"

	"github.com/anilibria/alice/internal/utils"
	futils "github.com/gofiber/fiber/v2/utils"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
)

type Randomizer struct {
	log  *zerolog.Logger
	done func() <-chan struct{}

	rctx    context.Context
	rclient *redis.Client

	releasesKey string
	relUpdFreq  time.Duration

	mu       sync.RWMutex
	ready    bool
	releases []string
}

func New(c context.Context) (_ *Randomizer, e error) {
	cli, log :=
		c.Value(utils.CKCliCtx).(*cli.Context),
		c.Value(utils.CKLogger).(*zerolog.Logger)

	r := &Randomizer{}
	r.log, r.done = log, c.Done

	r.rclient = redis.NewClient(&redis.Options{
		Addr:     cli.String("randomizer-redis-host"),
		Password: cli.String("randomizer-redis-password"),
		DB:       cli.Int("randomizer-redis-database"),

		ClientName: fmt.Sprintf("%s/%s", cli.App.Name, cli.App.Version),

		MaxRetries:   cli.Int("redis-client-maxretries"),
		DialTimeout:  cli.Duration("redis-client-dialtimeout"),
		ReadTimeout:  cli.Duration("redis-client-readtimeout"),
		WriteTimeout: cli.Duration("redis-client-writetimeout"),
	})

	r.rctx = context.Background()
	r.releases, r.releasesKey = make([]string, 0), cli.String("randomizer-releaseskey")

	if r.releases, e = r.lookupReleases(); e != nil {
		r.log.Error().Msg(e.Error())
		return
	}
	r.setReady(true)

	return r, e
}

func (m *Randomizer) Bootstrap() {
	m.loop()
	m.destroy()
}

func (m *Randomizer) IsReady() bool {
	return m.isReady()
}

func (m *Randomizer) Randomize() string {
	return m.randomRelease()
}

//

func (m *Randomizer) loop() {
	m.log.Debug().Msg("initiate randomizer release update loop...")
	defer m.log.Debug().Msg("randomizer release update loop has been closed")

	update := time.NewTimer(m.relUpdFreq)

LOOP:
	for {
		select {
		case <-m.done():
			m.log.Info().Msg("internal abort() has been caught; initiate application closing...")
			break LOOP
		case <-update.C:
			releases, e := m.lookupReleases()
			if e != nil {
				m.log.Error().Msg("could not updated releases for randomizer - " + e.Error())
				continue
			}

			m.rotateReleases(releases)
		}
	}
}

func (m *Randomizer) destroy() {
	if e := m.rclient.Close(); e != nil {
		m.log.Error().Msg("coudl not properly close http client - " + e.Error())
	}
}

func (m *Randomizer) setReady(ready bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ready = ready
}

func (m *Randomizer) isReady() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.ready
}

func (m *Randomizer) peekReleaseKeyChunks() (_ int, e error) {
	var res string
	if res, e = m.rclient.Get(m.rctx, m.releasesKey).Result(); e == redis.Nil {
		e = errors.New("no such release key in redis; is it correct - " + m.releasesKey)
		return
	} else if e != nil {
		return
	} else if res == "" {
		e = errors.New("redis client respond with an empty string; is release key is alive?")
		return
	}

	return strconv.Atoi(res)
}

func (m *Randomizer) lookupReleases() (_ []string, e error) {
	var chunks int
	if chunks, e = m.peekReleaseKeyChunks(); e != nil {
		return
	} else if chunks == 0 {
		e = errors.New("invalid chunks count was responded by redis client or converted by golang")
		return
	}
	m.log.Trace().Msgf("release key says about %d chunks", chunks)

	// avoid mass allocs
	started := time.Now()
	releases := make([]string, len(m.releases))
	res, errs, total, banned := "", []string{}, 0, 0

	for i := 0; i < chunks; i++ {
		m.log.Trace().Msgf("parsing chunk %d/%d...", i, chunks)

		if res, e = m.rclient.Get(m.rctx, m.releasesKey+strconv.Itoa(i)).Result(); e == redis.Nil {
			e = errors.New(fmt.Sprintf("given chunk number %d is not exists", i))
			m.log.Warn().Msg(e.Error())
			errs = append(errs, e.Error())
			continue
		} else if e != nil {
			m.log.Warn().Msg("an error occured while peeking a releases chunk - " + e.Error())
			errs = append(errs, e.Error())
			continue
		}

		var releasesChunk Releases
		if e = json.Unmarshal(futils.UnsafeBytes(res), &releasesChunk); e != nil {
			m.log.Warn().Msg("an error occured while unmarshal release chunk - " + e.Error())
			errs = append(errs, e.Error())
			continue
		}

		for _, release := range releasesChunk {
			if release.BlockedInfo != nil && release.BlockedInfo.IsBlockedByCopyrights {
				m.log.Debug().Msgf("release %d (%s) worldwide banned, skip it...", release.Id, release.Code)
				banned++
				continue
			}

			if zerolog.GlobalLevel() <= zerolog.DebugLevel {
				m.log.Trace().Msgf("release %d with code %s found", release.Id, release.Code)
			}

			total++
			releases = append(releases, release.Code)
		}

	}

	if errslen := len(errs); errslen != 0 {
		m.log.Error().Msgf("%d chunks were corrupted, data from them did not get into the cache", errslen)
		m.log.Error().Msg("release redis extraction process errors:")

		for _, err := range errs {
			m.log.Error().Msg(err)
		}
	}

	m.log.Info().Msgf("in %s from %d (of %d) chunks added %d releases and %d skipped because of WW ban",
		time.Since(started).String(), chunks-len(errs), chunks, total, banned)
	return releases, nil
}

func (m *Randomizer) rotateReleases(releases []string) {
	m.setReady(false)
	m.mu.Lock()

	defer m.setReady(true)
	defer m.mu.Unlock()

	m.log.Debug().Msgf("update current %d releases with slice of %d releases",
		len(m.releases), len(releases))
	m.releases = releases
}

func (m *Randomizer) randomRelease() (_ string) {
	if !m.isReady() {
		m.log.Warn().Msg("randomizer is not ready yet")
		return
	}

	if !m.mu.TryRLock() {
		m.log.Warn().Msg("could not get randomized release, read lock is not available")
		return
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	r := rand.Intn(len(m.releases))
	return m.releases[r]
}
