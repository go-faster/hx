package hx

import (
	"bufio"
	"bytes"
	"testing"
)

func FuzzResponse(f *testing.F) {
	// Add valid responses to corpus.
	for _, s := range []string{
		"HTTP/1.1 200 OK\r\nContent-Length: 10\r\nContent-Type: foo/bar\r\n\r\n0123456789",
		"HTTP/1.1 230 OK\r\nContent-Type: text\r\nTransfer-Encoding: aaabbb\r\n\r\n2\r\ner\r\n2\r\nty\r\n0\r\n\r\nwe",
		"HTTP/1.1 300 OK\r\nContent-Length: 5\r\nContent-Type: bar\r\n\r\n56789aaa",
		"HTTP/1.1 500 OK\r\nContent-Length: 0\r\nContent-Type: foo/bar\r\n\r\n",
		"HTTP/1.1 200 OK\r\nContent-Type: foobar\r\n\r\nzxxc",
	} {
		var r Response
		if err := r.Read(bufio.NewReader(bytes.NewReader([]byte(s)))); err != nil {
			f.Fatalf("unexpected error: %s", err)
		}
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		var r Response

		if err := r.Read(bufio.NewReader(bytes.NewReader(data))); err != nil {
			return
		}

		w := bytes.Buffer{}
		if _, err := r.WriteTo(&w); err != nil {
			return
		}
	})
}
