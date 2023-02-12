package hx

import (
	"testing"
)

func recoverWith(t *testing.T, d []byte, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %s, data: %q", r, d)
		}
	}()
	f()
}
