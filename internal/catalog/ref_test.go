package catalog

import (
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/semver"
)

// cream100 — релизная сборка cream 1.0.0.
func cream100(t *testing.T) manifest.CreamBuild {
	t.Helper()
	v, err := semver.Parse("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	return manifest.CreamBuild{Version: v}
}

func TestParseRef(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		source  string
		name    string
		version string // "" — без @version
		bad     bool
	}{
		{in: "noop", name: "noop"},
		{in: "core/teleport", source: "core", name: "teleport"},
		{in: "teleport@4.0.0", name: "teleport", version: "4.0.0"},
		{in: "core/teleport@4.0.0-rc.1", source: "core", name: "teleport", version: "4.0.0-rc.1"},
		{in: "my.src/a_b-c.d", source: "my.src", name: "a_b-c.d"},
		{in: "", bad: true},
		{in: "/noop", bad: true},
		{in: "core/", bad: true},
		{in: "a/b/c", bad: true},
		{in: "noop@", bad: true},
		{in: "noop@1.0", bad: true},
		{in: "noop@v1.0.0", bad: true},
		{in: "noop@1.0.0@2.0.0", bad: true},
		{in: "Noop", bad: true},
		{in: "cream", bad: true},
		{in: "cream-tool", bad: true},
		{in: "global", bad: true},
		{in: "Core/noop", bad: true},
		{in: "-x/noop", bad: true},
		{in: "noop ", bad: true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			r, err := ParseRef(tc.in)
			if tc.bad {
				if cerr.ExitCode(err) != cerr.ExitUsage || cerr.CodeOf(err) != "usage.ref" {
					t.Fatalf("ParseRef(%q) = %+v, %v; ждали 64 usage.ref", tc.in, r, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRef(%q): %v", tc.in, err)
			}
			if r.Source != tc.source || r.Name != tc.name {
				t.Errorf("ParseRef(%q) = %q/%q, ждали %q/%q", tc.in, r.Source, r.Name, tc.source, tc.name)
			}
			switch {
			case tc.version == "" && r.Version != nil:
				t.Errorf("версия %v, ждали без версии", r.Version)
			case tc.version != "" && (r.Version == nil || r.Version.String() != tc.version):
				t.Errorf("версия %v, ждали %s", r.Version, tc.version)
			}
			if got := r.String(); got != tc.in {
				t.Errorf("String() = %q, ждали %q", got, tc.in)
			}
		})
	}
}
