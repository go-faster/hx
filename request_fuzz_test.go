package hx

import (
	"bufio"
	"bytes"
	"testing"
)

func FuzzRequest(f *testing.F) {
	// Add valid requests to corpus.
	for _, s := range []string{
		"GET /ee/user/project/integrations/webhooks.html HTTP/1.1\r\nHost: docs.gitlab.com\r\n\r\n",
	} {
		var r Request
		if err := r.Read(bufio.NewReader(bytes.NewReader([]byte(s)))); err != nil {
			f.Fatalf("unexpected error: %s", err)
		}
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		var r Request

		if err := r.Read(bufio.NewReader(bytes.NewReader(data))); err != nil {
			return
		}

		w := bytes.Buffer{}
		if _, err := r.WriteTo(&w); err != nil {
			return
		}
	})
}
