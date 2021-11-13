//go:build windows
// +build windows

package hx

import "testing"

func TestURIPathNormalizeIssue86(t *testing.T) {
	t.Parallel()

	// see https://github.com/go-faster/hx/issues/86
	var u URI

	testURIPathNormalize(t, &u, `C:\a\b\c\fs.go`, `C:\a\b\c\fs.go`)
}
