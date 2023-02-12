package hx

import (
	"bytes"
	"testing"
)

func FuzzURL(f *testing.F) {
	// Add valid URLs to corpus.
	for _, s := range []string{
		"http://foo.bar/baz?aaa=22#aaa",
		"/aaa/bbb/ccc/../../ddd",
		"/aa//bb",
		"ftp://aaa/xxx/yyy?aaa=bb#aa",
		"/foo/bar/",
	} {
		var u URI
		if err := u.Parse(nil, []byte(s)); err != nil {
			f.Fatalf("unexpected error: %s", err)
		}
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		recoverWith(t, data, func() {
			u := AcquireURI()
			defer ReleaseURI(u)

			u.UpdateBytes(data)

			w := bytes.Buffer{}
			_, _ = u.WriteTo(&w)
		})
	})
}
