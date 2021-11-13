//go:build gofuzz
// +build gofuzz

package fuzz

import (
	"bufio"
	"bytes"

	github.com/go-faster/hx"
)

func Fuzz(data []byte) int {
	req := hx.AcquireRequest()
	defer hx.ReleaseRequest(req)

	if err := req.Read(bufio.NewReader(bytes.NewReader(data))); err != nil {
		return 0
	}

	w := bytes.Buffer{}
	if _, err := req.WriteTo(&w); err != nil {
		return 0
	}

	return 1
}
