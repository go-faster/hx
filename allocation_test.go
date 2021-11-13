//go:build !race
// +build !race

package hx

import (
	"bufio"
	"testing"
)

func TestAllocationServeConn(t *testing.T) {
	s := &Server{
		Handler: Nop,
	}

	rw := &readWriter{}
	// Make space for the request and response here so it
	// doesn't allocate within the test.
	rw.r.Grow(1024)
	rw.w.Grow(1024)

	ctx := &Ctx{c: rw}
	w := bufio.NewWriter(rw)
	r := bufio.NewReader(rw)

	n := testing.AllocsPerRun(100, func() {
		rw.r.WriteString("GET / HTTP/1.1\r\nHost: google.com\r\nCookie: foo=bar\r\n\r\n")
		if err := s.serveConn(ctx, r, w); err != nil {
			t.Fatal(err)
		}

		// Reset the write buffer to make space for the next response.
		rw.w.Reset()
	})

	if n != 0 {
		t.Fatalf("expected 0 allocations, got %f", n)
	}
}

func BenchmarkServeConn(b *testing.B) {
	const req = "GET / HTTP/1.1\r\nHost: google.com\r\nCookie: foo=bar\r\n\r\n"

	s := &Server{Handler: Nop}

	rw := &readWriter{}
	ctx := &Ctx{c: rw}
	w := bufio.NewWriter(rw)
	r := bufio.NewReader(rw)
	// Make space for the request and response here so it
	// doesn't allocate within the test.
	rw.r.Grow(1024)
	rw.w.Grow(1024)
	b.ReportAllocs()
	b.SetBytes(int64(len(req)))

	for i := 0; i < b.N; i++ {
		rw.w.Reset()
		rw.r.WriteString(req)
		if err := s.serveConn(ctx, r, w); err != nil {
			b.Fatal(err)
		}
	}
}

func TestAllocationURI(t *testing.T) {
	uri := []byte("http://username:password@hello.%e4%b8%96%e7%95%8c.com/some/path?foo=bar#test")

	n := testing.AllocsPerRun(100, func() {
		u := AcquireURI()
		u.Parse(nil, uri) //nolint:errcheck
		ReleaseURI(u)
	})

	if n != 0 {
		t.Fatalf("expected 0 allocations, got %f", n)
	}
}
