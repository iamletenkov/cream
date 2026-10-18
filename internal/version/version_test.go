package version

import "testing"

func TestLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "empty is dev", raw: "", want: "cream dev"},
		{name: "spaces only is dev", raw: "  ", want: "cream dev"},
		{name: "dev as is", raw: "dev", want: "cream dev"},
		{name: "tag with v", raw: "v1.2.3", want: "cream 1.2.3"},
		{name: "plain semver", raw: "1.2.3", want: "cream 1.2.3"},
		{name: "git describe", raw: "v0.1.0-3-gabc1234-dirty", want: "cream 0.1.0-3-gabc1234-dirty"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Line(tc.raw); got != tc.want {
				t.Errorf("Line(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
