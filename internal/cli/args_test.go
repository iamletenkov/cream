package cli

import (
	"flag"
	"slices"
	"testing"
	"time"
)

func testFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.Bool("apply", false, "")
	fs.Bool("json", false, "")
	fs.Duration("lock-timeout", time.Second, "")
	fs.String("params-file", "", "")
	return fs
}

func TestPermute(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		args    []string
		until   int
		want    []string
		wantErr bool
	}{
		{name: "flag after positionals", args: []string{"teleport", "K=V", "--apply"},
			want: []string{"--apply", "--", "teleport", "K=V"}},
		{name: "flag before positionals", args: []string{"--apply", "teleport", "K=V"},
			want: []string{"--apply", "--", "teleport", "K=V"}},
		{name: "double dash stops parsing", args: []string{"teleport", "--", "--apply", "-x"},
			want: []string{"--", "teleport", "--apply", "-x"}},
		{name: "value flag consumes next", args: []string{"ext", "--params-file", "f", "K=-"},
			want: []string{"--params-file", "f", "--", "ext", "K=-"}},
		{name: "value flag with equals", args: []string{"ext", "--params-file=f"},
			want: []string{"--params-file=f", "--", "ext"}},
		{name: "single dash flag", args: []string{"ext", "-lock-timeout", "3s", "-json"},
			want: []string{"-lock-timeout", "3s", "-json", "--", "ext"}},
		{name: "bool with explicit value", args: []string{"ext", "--apply=false"},
			want: []string{"--apply=false", "--", "ext"}},
		{name: "lone dash is positional", args: []string{"ext", "-"},
			want: []string{"--", "ext", "-"}},
		{name: "unknown flag is kept for the parser", args: []string{"ext", "--nope"},
			want: []string{"--nope", "--", "ext"}},
		{name: "value flag without value", args: []string{"ext", "--params-file"}, wantErr: true},
		{name: "run: nothing after action is permuted", args: []string{"--json", "ext", "act", "--json"}, until: 2,
			want: []string{"--json", "--", "ext", "act", "--json"}},
		{name: "run: flags between ext and action are permuted", args: []string{"ext", "--json", "act", "-x", "--apply"}, until: 2,
			want: []string{"--json", "--", "ext", "act", "-x", "--apply"}},
		{name: "run: double dash before action", args: []string{"ext", "--", "--act"}, until: 2,
			want: []string{"--", "ext", "--act"}},
		{name: "empty", args: nil, want: []string{"--"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := permute(testFlagSet(), tc.args, tc.until)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("permute(%q) = %q, want error", tc.args, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("permute(%q) error: %v", tc.args, err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("permute(%q) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

// Перестановка и разбор вместе: `set teleport K=V --apply` ≡ `set --apply teleport K=V`.
func TestPermuteThenParse(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"teleport", "K=V", "--apply"},
		{"--apply", "teleport", "K=V"},
		{"teleport", "--apply", "K=V"},
	} {
		fs := testFlagSet()
		p, err := permute(fs, args, 0)
		if err != nil {
			t.Fatalf("permute(%q): %v", args, err)
		}
		if err := fs.Parse(p); err != nil {
			t.Fatalf("Parse(%q): %v", p, err)
		}
		if fs.Lookup("apply").Value.String() != "true" {
			t.Errorf("%q: apply not set", args)
		}
		if got := fs.Args(); !slices.Equal(got, []string{"teleport", "K=V"}) {
			t.Errorf("%q: Args() = %q", args, got)
		}
	}
}
