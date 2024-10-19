package anilibria

import (
	"errors"
	"math/rand"
	"strconv"
	"sync"
)

type ROption func(*options)

type options struct {
	fetchtries int
}

func WithFetchTries(tries int) ROption {
	return func(o *options) {
		o.fetchtries = tries
	}
}

//
//
//

type Releases struct {
	opts *options

	mu      sync.RWMutex
	idxid   map[string]*Release
	idxcode map[string]*Release

	// commitedmu   sync.RWMutex
	// commitedid   map[string]*Release
	// commitedcode map[string]*Release
}

func NewReleases(opts ...ROption) *Releases {
	r := &Releases{
		idxid:   make(map[string]*Release),
		idxcode: make(map[string]*Release),

		opts: &options{
			fetchtries: 10,
		},
	}

	for _, opt := range opts {
		opt(r.opts)
	}

	return r
}

func (m *Releases) Commit(releases map[string]*Release) {
	actionWithLock(&m.mu, func() {
		m.idxcode = releases
		m.idxid = make(map[string]*Release)

		for _, release := range m.idxcode {
			m.idxid[strconv.Itoa(int(release.Id))] = release

		}
	})
}

func (m *Releases) Len() (length int) {
	length, _ = actionWithRLock[int](&m.mu, func() (length int, _ bool) {
		length = len(m.idxcode)
		return length, length != 0
	})

	return
}

func (m *Releases) RandomRelease(region string) (release *Release, e error) {
	var ok bool
	var blocked bool

	for try := 1; try <= m.opts.fetchtries; try++ {
		release, ok = actionWithRLock(&m.mu, func() (*Release, bool) {
			max := len(m.idxid)

			// skipcq: GSC-G404 math/rand is enoght here
			return m.idxid[strconv.Itoa(rand.Intn(max))], max != 0
		})

		if !ok {
			return nil, errors.New("randomizer has not ready yet or unexpected error occurred")
		}

		if blocked, ok = release.IsOverworldBlocked(); !ok || blocked {
			continue
		}

		if blocked, ok = release.IsBlockedInRegion(region); !ok || blocked {
			continue
		}

		return release, nil
	}

	return nil, errors.New("there are too many errors in release fetching")
}

func (m *Releases) IsExists(code string) (ok bool) {
	_, ok = actionWithRLock(&m.mu, func() (_ *Release, ok bool) {
		_, ok = m.idxcode[code]
		return
	})

	return
}

//
//
//

func actionWithRLock[V int | *Release](mu *sync.RWMutex, action func() (V, bool)) (V, bool) {
	mu.RLock()
	defer mu.RUnlock()

	return action()
}

func actionWithLock(mu *sync.RWMutex, action func()) {
	mu.Lock()
	defer mu.Unlock()

	action()
}
