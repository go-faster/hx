//go:build gofuzz
// +build gofuzz

package fuzz

import (
	"bufio"
	"bytes"

	github.com/go-faster/hx"
)

func Fuzz(data []byte) int {
	res := hx.AcquireResponse()
	defer hx.ReleaseResponse(res)

	if err := res.Read(bufio.NewReader(bytes.NewReader(data))); err != nil {
		return 0
	}

	w := bytes.Buffer{}
	if _, err := res.WriteTo(&w); err != nil {
		return 0
	}

	return 1
}
