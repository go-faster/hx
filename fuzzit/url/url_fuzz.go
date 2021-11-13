//go:build gofuzz
// +build gofuzz

package fuzz

import (
	"bytes"

	github.com/go-faster/hx"
)

func Fuzz(data []byte) int {
	u := hx.AcquireURI()
	defer hx.ReleaseURI(u)

	u.UpdateBytes(data)

	w := bytes.Buffer{}
	if _, err := u.WriteTo(&w); err != nil {
		return 0
	}

	return 1
}
