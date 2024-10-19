package anilibria

import (
	"errors"
	"fmt"
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

	mu       sync.RWMutex
	releases map[string]*Release
	idxcode  []string

	// commitedmu   sync.RWMutex
	// commitedid   map[string]*Release
	// commitedcode map[string]*Release
}

func NewReleases(opts ...ROption) *Releases {
	r := &Releases{
		releases: make(map[string]*Release),

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
	length, _ := actionWithRLock[int](&m.mu, func() (lenth int, _ bool) {
		lenth = len(m.releases)
		return lenth, lenth != 0
	})

	actionWithLock(&m.mu, func() {
		m.releases = releases
		m.idxcode = make([]string, 0, len(m.releases))

		for _, release := range m.releases {
			if release == nil {
				fmt.Println("BUG: we caught en empty release in commit stage!")
				continue
			}

			// build index ID:RELEASE for faster searching
			m.releases[strconv.Itoa(int(release.Id))] = release

			// store code in slice for further Random() requests
			m.idxcode = append(m.idxcode, release.Code)
		}
	})

	fmt.Printf("COMMITING: old map %d len, new map %d len\n", length, m.Len())
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
			var max int
			if max = len(m.idxcode); max == 0 {
				return nil, false
			}

			// skipcq: GSC-G404 math/rand is enoght here
			id := m.idxcode[rand.Intn(max)]
			return m.releases[id], true
		})

		if !ok || release == nil {
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
		_, ok = m.releases[code]
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
