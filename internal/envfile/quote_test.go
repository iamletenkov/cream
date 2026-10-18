package envfile

import (
	"strings"
	"testing"
)

// hostile is a fuzz-like table of values that break naive quoting.
var hostile = []string{
	"", " ", "  x", "x  ", "a b", "\t", "\n", "\r", "\r\n", "a\nb", "x\\", "\\", "\\\\", "\\\n", "\\\"",
	"\"", "\"\"", "'", "''", "'\"'", "$x", "${x}", "$(id)", "`id`", "#x", ";x", "x#y", "x;y", "=", "==x",
	"a=b", "-", "--flag", "~", "*", "?", "[a]", "{a,b}", "!", "&", "|", "<>", "%", "@", "+", ",", ":", "/a/b",
	"ключ значение", "日本", "emoji \U0001F600", "\v\f", "\x01\x1f\x7f", "tab\tin", "end\\", "\\start",
	"mid\\\"dle", "\"quoted\"", "'single'", "a\\nb", "\\\\\\", "\"\\\"",
	strings.Repeat("x", 4096), strings.Repeat("\\\"", 64),
}

func TestQuoteRoundTrip(t *testing.T) {
	t.Parallel()
	for _, v := range hostile {
		for _, tail := range []string{"\n", "", "\r\n"} {
			for _, legacy := range []bool{false, true} {
				in := "# before \\\nK=" + Quote(v) + tail + "Z=1\n"
				if tail == "" {
					in = "K=" + Quote(v)
				}
				f, err := parse([]byte(in), legacy)
				if err != nil {
					t.Fatalf("parse(%q): %v", in, err)
				}
				// The legacy dialect swallows the line after the comment, so
				// only the bare form is meaningful there.
				if legacy && tail != "" {
					continue
				}
				if got, ok := f.Get("K"); !ok || got != v {
					t.Errorf("Quote(%q) = %q reads back as %q (ok=%v), tail %q legacy %v", v, Quote(v), got, ok, tail, legacy)
				}
				if tail != "" {
					if z, _ := f.Get("Z"); z != "1" {
						t.Errorf("Quote(%q) = %q swallows the next line", v, Quote(v))
					}
				}
			}
		}
	}
}

func TestQuoteForm(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{"", ""},
		{"plain", "plain"},
		{"a.b/c:d,e@f%g+h=i-j_k", "a.b/c:d,e@f%g+h=i-j_k"},
		{"a b", `"a b"`},
		{`a"b`, `"a\"b"`},
		{`a\b`, `"a\\b"`},
		{"$x", `"$x"`},
		{"#x", `"#x"`},
	}
	for _, tc := range tests {
		if got := Quote(tc.in); got != tc.want {
			t.Errorf("Quote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSetRoundTripHostile(t *testing.T) {
	t.Parallel()
	f, err := Parse([]byte("# head\nA=old\n"))
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range hostile {
		k := "K" + strings.Repeat("_", i)
		f.Set(k, v)
		f.Set("A", v)
	}
	g, err := Parse(f.Bytes())
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	for i, v := range hostile {
		k := "K" + strings.Repeat("_", i)
		if got, _ := g.Get(k); got != v {
			t.Errorf("%s: got %q, want %q", k, got, v)
		}
	}
	if got, _ := g.Get("A"); got != hostile[len(hostile)-1] {
		t.Errorf("A: got %q", got)
	}
}
