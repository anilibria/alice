package anilibria

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/anilibria/alice/internal/utils"
	"github.com/goccy/go-json"
	futils "github.com/gofiber/fiber/v2/utils"
	"github.com/klauspost/compress/zstd"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
	"github.com/valyala/bytebufferpool"
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

	encoder *zstd.Encoder
	decoder *zstd.Decoder

	releases *Releases
}

func New(c context.Context) *Randomizer {
	cli := c.Value(utils.CKCliCtx).(*cli.Context)

	var dec *zstd.Decoder
	var enc *zstd.Encoder
	if cli.Bool("randomizer-redis-zstd-enable") {
		enc, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBetterCompression))
		dec, _ = zstd.NewReader(nil, zstd.WithDecoderConcurrency(0))
	}

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

		encoder: enc,
		decoder: dec,

		releases:    NewReleases(WithFetchTries(cli.Int("randomizer-random-fetch-tries"))),
		releasesKey: cli.String("randomizer-releaseskey"),
	}

	return r
}

func (m *Randomizer) Bootstrap() {
	m.loop()
	m.destroy()
}

func (m *Randomizer) Randomize(region string) (_ string, e error) {
	var release *Release
	if release, e = m.releases.RandomRelease(region); e != nil {
		return
	}

	return release.Code, e
}

// func (m *Randomizer) RawRelease(ident []byte) (release []byte, ok bool, e error) {
// 	var rawrelease []byte
// 	if rawrelease, ok = m.rawreleases[futils.UnsafeString(ident)]; !ok {
// 		return
// 	}

// 	// decompress chunk response from redis
// 	if release, e = m.decompressPayload(rawrelease); e != nil {
// 		m.log.Warn().Msg("an error occurred while decompress redis response - " + e.Error())
// 		return
// 	}

// 	return
// }

//
//
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
			var chunks, failed, banned int
			started, releases := time.Now(), make(map[string]*Release, m.releases.Len()+10)
			// m.releases.Len()+10 - avoiding mass alocs in lookupReleases()

			if chunks, failed, banned, e = m.lookupReleases(releases); e != nil {
				m.log.Error().Msg("could not updated releases for randomizer - " + e.Error())
				update.Reset(m.relUpdFreqErr)
				continue
			}

			parsed := time.Now()
			m.log.Info().Msgf("in %s from %d (of %d) chunks added %d releases and %d WW banned",
				time.Since(started).String(), failed, chunks, len(releases), banned)

			m.releases.Commit(releases)
			m.log.Debug().Msgf("new releases commited for %s", time.Since(parsed).String())
			update.Reset(m.relUpdFreq)
		}
	}
}

func (m *Randomizer) destroy() {
	if e := m.rclient.Close(); e != nil {
		m.log.Error().Msg("could not properly close redis client - " + e.Error())
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

	var dres []byte
	if dres, e = m.decompressPayload(futils.UnsafeBytes(res)); e != nil {
		m.log.Warn().Msg("an error occurred while decompress response redis response - " + e.Error())
		return
	}

	return strconv.Atoi(futils.UnsafeString(dres))
}

func (m *Randomizer) lookupReleases(releases map[string]*Release) (chunks, failed, banned int, e error) {
	if chunks, e = m.peekReleaseKeyChunks(); e != nil {
		return
	} else if chunks == 0 {
		e = errors.New("invalid chunks count was responded by redis client or converted by golang")
		return
	}
	m.log.Trace().Msgf("release key says about %d chunks", chunks)
	m.log.Info().Msgf("staring release parsing from redis with %d chunks", chunks)

	errs := make([]string, 0, chunks)

	for i := 0; i < chunks; i++ {
		select {
		case <-m.done():
			e = errors.New("chunk parsing has been interrupted by global abort()")
			return
		default:
			m.log.Trace().Msgf("parsing chunk %d/%d...", i, chunks)
		}

		chunk := bytebufferpool.Get()
		defer func() {
			chunk.Reset()
			bytebufferpool.Put(chunk)
		}()

		// get decompressed chunk from redis
		if chunk.B, e = m.chunkFetchFromRedis(m.releasesKey + strconv.Itoa(i)); e != nil {
			m.log.Warn().Msg("an error occurred while peeking a releases chunk - " + e.Error())
			errs = append(errs, e.Error())
			continue
		} else if chunk.Len() == 0 {
			e = fmt.Errorf("given chunk number %d is not exists", i)
			m.log.Warn().Msg(e.Error())
			errs = append(errs, e.Error())
			continue
		}

		// store raw json objects for further query=release responding
		var rawReleases RawReleasesChunk
		if e = json.Unmarshal(chunk.B, &rawReleases); e != nil {
			m.log.Warn().Msg("an error occurred while unmarshal release chunk - " + e.Error())
			errs = append(errs, e.Error())
			continue
		}

		// get json formated response from decompressed response
		var releasesChunk ReleasesChunk
		if e = json.Unmarshal(chunk.B, &releasesChunk); e != nil {
			m.log.Warn().Msg("an error occurred while unmarshal release chunk - " + e.Error())
			errs = append(errs, e.Error())
			continue
		}

		// parse json chunk response
		for id, release := range releasesChunk {
			if release == nil {
				m.log.Error().Msg("BUG! found an empty release after json.Unmarshal")
				continue
			}

			// save raw json for query=release
			release.SetRawJSON(rawReleases[id])

			if b, _ := release.IsOverworldBlocked(); b {
				m.log.Debug().Msgf("release %d (%s) worldwide banned", release.Id, release.Code)
				banned++
			}

			if zerolog.GlobalLevel() <= zerolog.DebugLevel {
				m.log.Trace().Msgf("release %d with code %s found", release.Id, release.Code)
			}

			releases[release.Code] = release
		}
	}

	if errslen := len(errs); errslen != 0 {
		m.log.Error().Msgf("%d chunks were corrupted, data from them did not get into the cache", errslen)
		m.log.Error().Msg("release redis extraction process errors:")

		for _, err := range errs {
			m.log.Error().Msg(err)
		}
	}

	return chunks, chunks - len(errs), banned, nil
}

func (m *Randomizer) chunkFetchFromRedis(key string) (chunk []byte, e error) {
	var compressed string

	// get compressed chunk response from redis
	if compressed, e = m.rclient.Get(m.rctx, key).Result(); e == redis.Nil {
		e = nil
		return
	} else if e != nil {
		return
	}

	// decompress chunk response from redis
	if chunk, e = m.decompressPayload(futils.UnsafeBytes(compressed)); e != nil {
		return
	}

	return
}

func (m *Randomizer) decompressPayload(payload []byte) ([]byte, error) {
	if m.decoder == nil {
		return payload, nil
	}

	return m.decoder.DecodeAll(payload, nil)
}

func (m *Randomizer) compressPayload(payload []byte) []byte {
	return m.encoder.EncodeAll(payload, make([]byte, 0, len(payload)))
}
