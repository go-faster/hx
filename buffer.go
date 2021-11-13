package hx

import (
	"io"
)

// Buffer is bytes.Buffer.
type Buffer struct {
	Buf []byte
}

func (b *Buffer) Reset() {
	b.Buf = b.Buf[:0]
}

// ResetN resets buffer and expands it to fit n bytes.
func (b *Buffer) ResetN(n int) {
	b.Buf = append(b.Buf[:0], make([]byte, n)...)
}

// Expand expands buffer to add n bytes.
func (b *Buffer) Expand(n int) {
	b.Buf = append(b.Buf, make([]byte, n)...)
}

// Skip moves cursor for next n bytes.
func (b *Buffer) Skip(n int) {
	b.Buf = b.Buf[n:]
}

// Read implements io.Reader.
func (b *Buffer) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(b.Buf) == 0 {
		return 0, io.EOF
	}
	n = copy(p, b.Buf)
	b.Buf = b.Buf[n:]
	return n, nil
}

// Copy returns new copy of buffer.
func (b *Buffer) Copy() []byte {
	return append([]byte{}, b.Buf...)
}

// Len returns length of internal buffer.
func (b Buffer) Len() int {
	return len(b.Buf)
}

// ResetTo sets internal buffer exactly to provided value.
//
// Buffer will retain buf, so user should not modify or read it
// concurrently.
func (b *Buffer) ResetTo(buf []byte) {
	b.Buf = buf
}

func (b *Buffer) String() string {
	return string(b.Buf)
}

// Put appends raw bytes to buffer.
//
// Buffer does not retain raw.
func (b *Buffer) Put(raw []byte) {
	b.Buf = append(b.Buf, raw...)
}

func (b *Buffer) Write(buf []byte) (int, error) {
	b.Buf = append(b.Buf, buf...)
	return len(buf), nil
}

func (b *Buffer) PutString(s string) {
	b.Buf = append(b.Buf, s...)
}
