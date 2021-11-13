package hx

import (
	"bytes"
	"sync"
)

type pool struct {
	p sync.Pool
}

func (p *pool) Get() *bytes.Buffer { return p.p.Get().(*bytes.Buffer) }

func (p *pool) Put(b *bytes.Buffer) {
	b.Reset()
	p.p.Put(b)
}

var bufPool = newPool()

func newPool() *pool {
	return &pool{
		p: sync.Pool{
			New: func() interface{} {
				return new(bytes.Buffer)
			},
		},
	}
}
