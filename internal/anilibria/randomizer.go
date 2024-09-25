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
	log   *zerolog.Logger
	done  func() <-chan struct{}
	abort context.CancelFunc

	rctx    context.Context
	rclient *redis.Client

	releasesKey    string
	relUpdFreq     time.Duration
	relUpdFreqErr  time.Duration
	relUpdFreqBoot time.Duration

	mu       sync.RWMutex
	releases []string
}

func New(c context.Context) *Randomizer {
	cli := c.Value(utils.CKCliCtx).(*cli.Context)

	r := &Randomizer{
		done:  c.Done,
		log:   c.Value(utils.CKLogger).(*zerolog.Logger),
		abort: c.Value(utils.CKAbortFunc).(context.CancelFunc),

		rctx: context.Background(),
		rclient: redis.NewClient(&redis.Options{
			Addr:     cli.String("randomizer-redis-host"),
			Password: cli.String("randomizer-redis-password"),
			DB:       cli.Int("randomizer-redis-database"),

			ClientName: fmt.Sprintf("%s/%s", cli.App.Name, cli.App.Version),

			MaxRetries:   cli.Int("redis-client-maxretries"),
			DialTimeout:  cli.Duration("redis-client-dialtimeout"),
			ReadTimeout:  cli.Duration("redis-client-readtimeout"),
			WriteTimeout: cli.Duration("redis-client-writetimeout"),
		}),

		relUpdFreq:     cli.Duration("randomizer-update-frequency"),
		relUpdFreqErr:  cli.Duration("randomizer-update-frequency-onerror"),
		relUpdFreqBoot: cli.Duration("randomizer-update-frequency-bootstrap"),

		releases:    make([]string, 0),
		releasesKey: cli.String("randomizer-releaseskey"),
	}

	return r
}

func (m *Randomizer) Bootstrap() {
	m.loop()
	m.destroy()
}

func (m *Randomizer) Randomize() string {
	return m.randomRelease()
}

//

func (m *Randomizer) loop() {
	m.log.Debug().Msg("initiate randomizer release update loop...")
	defer m.log.Debug().Msg("randomizer release update loop has been closed")

	update := time.NewTimer(m.relUpdFreqBoot)

LOOP:
	for {
		select {
		case <-m.done():
			m.log.Info().Msg("internal abort() has been caught; initiate application closing...")
			break LOOP
		case <-update.C:
			update.Stop()

			var e error
			var releases []string
			if releases, e = m.lookupReleases(); e != nil {
				m.log.Error().Msg("could not updated releases for randomizer - " + e.Error())
				update.Reset(m.relUpdFreqErr)
				continue
			}

			m.rotateReleases(releases)
			update.Reset(m.relUpdFreq)
		}
	}
}

func (m *Randomizer) destroy() {
	if e := m.rclient.Close(); e != nil {
		m.log.Error().Msg("could not properly close http client - " + e.Error())
	}
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
	m.log.Info().Msgf("staring release parsing from redis with %d chunks", chunks)

	// avoid mass allocs
	started := time.Now()
	releases := make([]string, 0, len(m.releases))

	var res string
	var errs []string
	var total, banned int

	for i := 0; i < chunks; i++ {
		select {
		case <-m.done():
			e = errors.New("chunk parsing has been interrupted by global abort()")
			return
		default:
			m.log.Trace().Msgf("parsing chunk %d/%d...", i, chunks)
		}

		if res, e = m.rclient.Get(m.rctx, m.releasesKey+strconv.Itoa(i)).Result(); e == redis.Nil {
			e = fmt.Errorf("given chunk number %d is not exists", i)
			m.log.Warn().Msg(e.Error())
			errs = append(errs, e.Error())
			continue
		} else if e != nil {
			m.log.Warn().Msg("an error occurred while peeking a releases chunk - " + e.Error())
			errs = append(errs, e.Error())
			continue
		}

		var releasesChunk Releases
		if e = json.Unmarshal(futils.UnsafeBytes(res), &releasesChunk); e != nil {
			m.log.Warn().Msg("an error occurred while unmarshal release chunk - " + e.Error())
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
	m.mu.Lock()
	defer m.mu.Unlock()

	m.log.Debug().Msgf("update current %d releases with slice of %d releases",
		len(m.releases), len(releases))
	m.releases = releases
}

func (m *Randomizer) randomRelease() (_ string) {
	if !m.mu.TryRLock() {
		m.log.Warn().Msg("could not get randomized release, read lock is not available")
		return
	}
	defer m.mu.RUnlock()

	if len(m.releases) == 0 {
		m.log.Warn().Msg("randomizer is not ready yet")
		return
	}

	r := rand.Intn(len(m.releases)) // skipcq: GSC-G404 math/rand is enoght here
	return m.releases[r]
}
