//go:build !race
// +build !race

package hx

import (
	"testing"
)

func TestAllocationServeConn(t *testing.T) {
	t.Skip("TODO")

	s := &Server{
		Handler: func(ctx *Ctx) {},
	}

	rw := &readWriter{}
	// Make space for the request and response here so it
	// doesn't allocate within the test.
	rw.r.Grow(1024)
	rw.w.Grow(1024)

	n := testing.AllocsPerRun(100, func() {
		rw.r.WriteString("GET / HTTP/1.1\r\nHost: google.com\r\nCookie: foo=bar\r\n\r\n")
		if err := s.ServeConn(rw); err != nil {
			t.Fatal(err)
		}

		// Reset the write buffer to make space for the next response.
		rw.w.Reset()
	})

	if n != 0 {
		t.Fatalf("expected 0 allocations, got %f", n)
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
