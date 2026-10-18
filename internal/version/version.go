// Package version holds the build version of cream.
package version

import "strings"

// Version is set at build time:
//
//	go build -ldflags "-X github.com/iamletenkov/cream/internal/version.Version=v1.2.3"
//
// It is the only package-level variable allowed to be mutable, and only via ldflags.
var Version = "dev"

// Line returns the human-readable version line for the given raw version:
// a leading "v" is dropped, an empty value becomes "dev".
func Line(raw string) string {
	v := strings.TrimPrefix(strings.TrimSpace(raw), "v")
	if v == "" {
		v = "dev"
	}
	return "cream " + v
}
