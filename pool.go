package hx

import (
	"sync"
)

type pool struct {
	p sync.Pool
}

func (p *pool) Get() *Buffer { return p.p.Get().(*Buffer) }

func (p *pool) Put(b *Buffer) {
	b.Reset()
	p.p.Put(b)
}

var bufPool = newPool()

func newPool() *pool {
	return &pool{
		p: sync.Pool{
			New: func() interface{} {
				return new(Buffer)
			},
		},
	}
}
