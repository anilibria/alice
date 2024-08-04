package anilibria

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	rctx    context.Context
	rclient *redis.Client

	releasesKey string

	log  *zerolog.Logger
	done func() <-chan struct{}

	mu       sync.RWMutex
	ready    bool
	releases []string
}

func New(c context.Context) (_ *Randomizer) {
	cli, log :=
		c.Value(utils.CKCliCtx).(*cli.Context),
		c.Value(utils.CKLogger).(*zerolog.Logger)

	r := &Randomizer{}

	r.log, r.done = log, c.Done

	r.rctx = context.Background()
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

	r.releasesKey = cli.String("randomizer-releaseskey")
	r.releases = make([]string, 0)

	//
	//
	if _, e := r.lookupReleases(); e != nil {
		r.log.Error().Msg(e.Error())
		r.done()
	}
	//
	//

	return r
}

func (m *Randomizer) Bootstrap() (e error) {
	// add ping
	// add timer
	return m.destroy()
}

func (*Randomizer) IsReady() bool {
	return false
}

//

func (*Randomizer) loop() {

}

func (m *Randomizer) destroy() error {
	return m.rclient.Close()
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
