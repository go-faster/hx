package hx

import (
	"bufio"
	"bytes"
	"testing"
)

func FuzzCookie(f *testing.F) {
	// Add valid cookies to corpus.
	for _, s := range []string{
		"foo=bar",
		"foo=bar; Domain=aaa.com",
		"foo=bar; Domain=aaa.com; PATH=/foo/bar",
		"foo=bar; Domain=aaa.com; PATH=/foo/bar; Secure",
		"foo=bar; Domain=aaa.com; PATH=/foo/bar; Secure; HttpOnly",
		"bar=baz; Domain=aaa.com; PATH=/foo/bar; Secure; HttpOnly",
		"foo=bar; Domain=aaa.com; PATH=/foo/bar; Secure; HttpOnly; SameSite=Strict",
		"foo=bar; Domain=aaa.com; PATH=/foo/bar; Secure; HttpOnly; SameSite=Lax",
		"foo=bar; Domain=aaa.com; PATH=/foo/bar; Secure; HttpOnly; SameSite=None",
		"foo=bar; Domain=aaa.com; PATH=/foo/bar; Secure; HttpOnly; SameSite=Strict; Max-Age=100",
		"foo=bar; Domain=aaa.com; PATH=/foo/bar; Secure; HttpOnly; SameSite=Lax; Max-Age=100",
	} {
		var c Cookie
		if err := c.Parse(s); err != nil {
			f.Fatalf("unexpected error: %s", err)
		}
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		recoverWith(t, data, func() {
			var r Response

			if err := r.Read(bufio.NewReader(bytes.NewReader(data))); err != nil {
				return
			}

			w := bytes.Buffer{}
			if _, err := r.WriteTo(&w); err != nil {
				return
			}
		})
	})
}
