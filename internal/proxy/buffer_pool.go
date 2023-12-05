package proxy

import (
	"net/http/httputil"
	"sync"
)

const DefaultMaxBufferSize = 1024 * 32 // MB

type bufferPool struct {
	pool sync.Pool
}

func (b *bufferPool) Get() []byte {
	return b.pool.Get().([]byte)
}

func (b *bufferPool) Put(bytes []byte) {
	b.pool.Put(bytes) // nolint:staticcheck
}

func newBufferPool() httputil.BufferPool {
	return &bufferPool{
		pool: sync.Pool{
			New: func() interface{} {
				return make([]byte, DefaultMaxBufferSize)
			},
		},
	}
}
