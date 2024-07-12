package proxy

import (
	"sync"

	futils "github.com/gofiber/fiber/v2/utils"
)

type Key struct {
	key []byte
}

var keyPool = sync.Pool{
	New: func() interface{} {
		return new(Key)
	},
}

func AcquireKey() *Key {
	return keyPool.Get().(*Key)
}

func ReleaseKey(key *Key) {
	key.Reset()
	keyPool.Put(key)
}

func (m *Key) Reset() {
	m.key = m.key[:0]
}

func (m *Key) Bytes() []byte {
	return m.key
}

func (m *Key) UnsafeString() string {
	return futils.UnsafeString(m.key)
}

func (m *Key) Put(key []byte) {
	m.key = append(m.key[:0], key...)
}
