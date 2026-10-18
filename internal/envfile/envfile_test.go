package envfile

import (
	"reflect"
	"strings"
	"testing"
)

type kv struct{ k, v string }

// pairs returns every key of f in Keys() order with its Get value.
func pairs(t *testing.T, f *File) []kv {
	t.Helper()
	var out []kv
	for _, k := range f.Keys() {
		v, ok := f.Get(k)
		if !ok {
			t.Fatalf("Keys() lists %q, but Get reports it missing", k)
		}
		out = append(out, kv{k, v})
	}
	return out
}

func TestParse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want []kv
	}{
		{name: "empty file", in: "", want: nil},
		{name: "simple", in: "A=1\n", want: []kv{{"A", "1"}}},
		{name: "no trailing newline", in: "A=1", want: []kv{{"A", "1"}}},
		{name: "blank lines and whitespace", in: "\n  \n\t\nA=1\n\n", want: []kv{{"A", "1"}}},
		{name: "whitespace around key and value", in: "  A \t=  1 2  \n", want: []kv{{"A", "1 2"}}},
		{name: "empty value", in: "A=\nB=   \n", want: []kv{{"A", ""}, {"B", ""}}},
		{name: "empty value at EOF", in: "A=", want: []kv{{"A", ""}}},
		{name: "hash comment", in: "# A=1\nB=2\n", want: []kv{{"B", "2"}}},
		{name: "semicolon comment", in: "; A=1\nB=2\n", want: []kv{{"B", "2"}}},
		{name: "indented comment", in: "   # A=1\nB=2\n", want: []kv{{"B", "2"}}},
		{name: "hash inside value is literal", in: "A=x #y\n", want: []kv{{"A", "x #y"}}},
		{name: "single quotes", in: "A='x y  '\n", want: []kv{{"A", "x y  "}}},
		{name: "backslash in single quotes is literal", in: `A='a\b\'` + "\n", want: []kv{{"A", `a\b\`}}},
		{name: "single quotes span lines", in: "A='a\nb'\nB=1\n", want: []kv{{"A", "a\nb"}, {"B", "1"}}},
		{name: "double quotes", in: `A="x y  "` + "\n", want: []kv{{"A", "x y  "}}},
		{name: "double quote escapes", in: "A=\"q\\\" b\\\\ d\\$ t\\`\"\n", want: []kv{{"A", "q\" b\\ d$ t`"}}},
		{name: "double quote keeps unknown escape", in: `A="a\nb\x"` + "\n", want: []kv{{"A", `a\nb\x`}}},
		{name: "double quote continuation", in: "A=\"ab\\\ncd\"\n", want: []kv{{"A", "abcd"}}},
		{name: "double quotes span lines", in: "A=\"a\nb\"\n", want: []kv{{"A", "a\nb"}}},
		{name: "unquoted backslash escapes next char", in: `A=a\b\\c\"d` + "\n", want: []kv{{"A", `ab\c"d`}}},
		{name: "unquoted continuation", in: "A=ab\\\ncd\n", want: []kv{{"A", "abcd"}}},
		{name: "escaped trailing space kept", in: "A=x\\ \n", want: []kv{{"A", "x "}}},
		{name: "backslash at EOF", in: `A=x\`, want: []kv{{"A", "x"}}},
		{name: "leading backslash", in: `A=\ x` + "\n", want: []kv{{"A", " x"}}},
		{name: "quoted parts concatenate", in: `A="x"'y'"z"` + "\n", want: []kv{{"A", "xyz"}}},
		{name: "bare text after quote joins value", in: `A="x"y` + "\n", want: []kv{{"A", "xy"}}},
		{name: "quotes after bare text are literal", in: `A=x"y z" 'w'` + "\n", want: []kv{{"A", `x"y z" 'w'`}}},
		{name: "space between quoted parts is dropped", in: `A="x"   "y"` + "\n", want: []kv{{"A", "xy"}}},
		{name: "leading whitespace before quote", in: "A=   \"  x\"\n", want: []kv{{"A", "  x"}}},
		{name: "hash after closing quote joins value", in: `A="x" #c` + "\n", want: []kv{{"A", "x#c"}}},
		{name: "unterminated double quote runs to EOF", in: "A=\"x\ny", want: []kv{{"A", "x\ny"}}},
		{name: "unterminated single quote runs to EOF", in: "A='x\ny\n", want: []kv{{"A", "x\ny\n"}}},
		{name: "equals in value", in: "A==b=c\n", want: []kv{{"A", "=b=c"}}},
		{name: "last assignment wins, first position kept", in: "A=1\nB=2\nA=3\n", want: []kv{{"A", "3"}, {"B", "2"}}},
		{name: "export prefix is not a valid name", in: "export A=1\nB=2\n", want: []kv{{"B", "2"}}},
		{name: "invalid names dropped", in: "1A=1\nA-B=2\nA B=3\n=4\nC_9=5\n_=6\n", want: []kv{{"C_9", "5"}, {"_", "6"}}},
		{name: "line without equals ignored", in: "garbage\nA=1\n", want: []kv{{"A", "1"}}},
		{name: "CRLF line ends", in: "A=1\r\nB=\"2\"\r\n", want: []kv{{"A", "1"}, {"B", "2"}}},
		{name: "lone CR ends a line", in: "A=1\rB=2", want: []kv{{"A", "1"}, {"B", "2"}}},
		{name: "unicode value", in: "A=ключ значение\n", want: []kv{{"A", "ключ значение"}}},
		{name: "tab is whitespace", in: "A=\tx\t\n", want: []kv{{"A", "x"}}},
		{name: "vertical tab is not whitespace", in: "A=\vx\v\n", want: []kv{{"A", "\vx\v"}}},
		// systemd >= 254: a comment ending in `\` no longer swallows the next line.
		{name: "comment ending in backslash (v254+)", in: "# c \\\nA=1\n", want: []kv{{"A", "1"}}},
		{name: "comment backslash mid-line", in: "# a\\b\nA=1\n", want: []kv{{"A", "1"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f, err := Parse([]byte(tc.in))
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.in, err)
			}
			if got := pairs(t, f); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Parse(%q) = %v, want %v", tc.in, got, tc.want)
			}
			if got := string(f.Bytes()); got != tc.in {
				t.Errorf("Bytes() of an untouched file = %q, want the input %q", got, tc.in)
			}
		})
	}
}

// The v249/v254 divergence on a comment ending in `\` is pinned here: the
// legacy dialect is what the systemd-249 reference fixtures are checked with.
func TestParseLegacyCommentEscape(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		in           string
		modern, v249 []kv
	}{
		{
			name:   "comment backslash swallows next line before v254",
			in:     "# c \\\nA=1\nB=2\n",
			modern: []kv{{"A", "1"}, {"B", "2"}},
			v249:   []kv{{"B", "2"}},
		},
		{
			name:   "chain of comment continuations",
			in:     "# c \\\nA=1 \\\nB=2\nC=3\n",
			modern: []kv{{"A", "1 B=2"}, {"C", "3"}},
			v249:   []kv{{"C", "3"}},
		},
		{
			name:   "same result without the backslash",
			in:     "# c\nA=1\n",
			modern: []kv{{"A", "1"}},
			v249:   []kv{{"A", "1"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, d := range []struct {
				legacy bool
				want   []kv
			}{{false, tc.modern}, {true, tc.v249}} {
				f, err := parse([]byte(tc.in), d.legacy)
				if err != nil {
					t.Fatalf("parse(legacy=%v): %v", d.legacy, err)
				}
				if got := pairs(t, f); !reflect.DeepEqual(got, d.want) {
					t.Errorf("parse(%q, legacy=%v) = %v, want %v", tc.in, d.legacy, got, d.want)
				}
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
	}{
		{name: "invalid utf-8 in value", in: "A=\xff\n"},
		{name: "invalid utf-8 in quoted value", in: "A=\"x\xc3\"\n"},
		{name: "invalid utf-8 in key", in: "A\xff=1\n"},
		{name: "NUL byte", in: "A=1\x00\nB=2\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse([]byte(tc.in)); err == nil {
				t.Errorf("Parse(%q): want error, got nil", tc.in)
			}
		})
	}
}

func TestParseAllowsInvalidUTF8InComments(t *testing.T) {
	t.Parallel()
	f, err := Parse([]byte("# \xff\nA=1\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if v, _ := f.Get("A"); v != "1" {
		t.Errorf("A = %q, want 1", v)
	}
}

func TestEdit(t *testing.T) {
	t.Parallel()
	type op struct {
		del  bool
		k, v string
	}
	tests := []struct {
		name string
		in   string
		ops  []op
		want string
	}{
		{
			name: "set keeps comments, blanks and order",
			in:   "# head\nA=1\n\n; note\nB=2\n",
			ops:  []op{{k: "A", v: "x y"}},
			want: "# head\nA=\"x y\"\n\n; note\nB=2\n",
		},
		{
			name: "set replaces first, drops later duplicates",
			in:   "A=1\nB=2\n  A=3\nC=4\n",
			ops:  []op{{k: "A", v: "z"}},
			want: "A=z\nB=2\nC=4\n",
		},
		{
			name: "set appends new key at the end",
			in:   "# c\nA=1\n",
			ops:  []op{{k: "B", v: "2"}, {k: "C", v: ""}},
			want: "# c\nA=1\nB=2\nC=\n",
		},
		{
			name: "append to file without trailing newline",
			in:   "A=1",
			ops:  []op{{k: "B", v: "2"}},
			want: "A=1\nB=2\n",
		},
		{
			name: "append to empty file",
			in:   "",
			ops:  []op{{k: "B", v: "2"}},
			want: "B=2\n",
		},
		{
			name: "set replaces a multi-line record whole",
			in:   "A=\"x\ny\"\nB=1\n",
			ops:  []op{{k: "A", v: "z"}},
			want: "A=z\nB=1\n",
		},
		{
			name: "set replaces a continued record whole",
			in:   "A=x\\\ny\nB=1\n",
			ops:  []op{{k: "A", v: "z"}},
			want: "A=z\nB=1\n",
		},
		{
			name: "set keeps indentation and CRLF",
			in:   "  A=1\r\nB=2\r\n",
			ops:  []op{{k: "A", v: "3"}},
			want: "  A=3\r\nB=2\r\n",
		},
		{
			name: "delete removes every assignment",
			in:   "# c\nA=1\nB=2\n\tA=3\n",
			ops:  []op{{del: true, k: "A"}},
			want: "# c\nB=2\n",
		},
		{
			name: "delete CRLF record",
			in:   "A=1\r\nB=2\r\n",
			ops:  []op{{del: true, k: "A"}},
			want: "B=2\r\n",
		},
		{
			name: "delete missing key is a no-op",
			in:   "A=1\n",
			ops:  []op{{del: true, k: "Z"}},
			want: "A=1\n",
		},
		{
			name: "delete then set rewrites in place",
			in:   "A=1\nB=2\n",
			ops:  []op{{del: true, k: "A"}, {k: "A", v: "9"}},
			want: "A=9\nB=2\n",
		},
		{
			name: "set then delete a new key leaves nothing",
			in:   "A=1\n",
			ops:  []op{{k: "B", v: "2"}, {del: true, k: "B"}},
			want: "A=1\n",
		},
		{
			name: "invalid-name lines are kept verbatim",
			in:   "export A=1\nB=2\n",
			ops:  []op{{k: "A", v: "3"}},
			want: "export A=1\nB=2\nA=3\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f, err := Parse([]byte(tc.in))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			for _, o := range tc.ops {
				if o.del {
					f.Delete(o.k)
				} else {
					f.Set(o.k, o.v)
				}
			}
			if got := string(f.Bytes()); got != tc.want {
				t.Errorf("Bytes() = %q, want %q", got, tc.want)
			}
			assertReparse(t, f)
		})
	}
}

// Appending after a file whose tail leaves the parser mid-token (open quote,
// pending escape, comment continuation) must not glue the new key into it.
func TestAppendAfterDirtyTail(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want []kv
	}{
		{name: "open single quote", in: "A='x", want: []kv{{"A", "x"}, {"N", "v"}}},
		{name: "open double quote", in: "A=\"x", want: []kv{{"A", "x"}, {"N", "v"}}},
		{name: "escape inside open double quote", in: "A=\"x\\", want: []kv{{"A", "x"}, {"N", "v"}}},
		{name: "pending escape", in: "A=x\\", want: []kv{{"A", "x"}, {"N", "v"}}},
		{name: "comment continuation", in: "# c\\", want: []kv{{"N", "v"}}},
		{name: "key without equals", in: "garbage", want: []kv{{"N", "v"}}},
		{name: "trailing spaces", in: "A=x  ", want: []kv{{"A", "x"}, {"N", "v"}}},
		{name: "invalid-name open quote", in: "export B='x", want: []kv{{"N", "v"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f, err := Parse([]byte(tc.in))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			f.Set("N", "v")
			out := f.Bytes()
			if !strings.HasPrefix(string(out), tc.in) {
				t.Errorf("Bytes() = %q must keep the original %q as prefix", out, tc.in)
			}
			for _, legacy := range []bool{false, true} {
				g, err := parse(out, legacy)
				if err != nil {
					t.Fatalf("reparse(legacy=%v) of %q: %v", legacy, out, err)
				}
				if got := pairs(t, g); !reflect.DeepEqual(got, tc.want) {
					t.Errorf("reparse(%q, legacy=%v) = %v, want %v", out, legacy, got, tc.want)
				}
			}
		})
	}
}

// assertReparse checks that Bytes() reads back to exactly what Get/Keys report.
func assertReparse(t *testing.T, f *File) {
	t.Helper()
	g, err := Parse(f.Bytes())
	if err != nil {
		t.Fatalf("reparse of %q: %v", f.Bytes(), err)
	}
	if got, want := pairs(t, g), pairs(t, f); !reflect.DeepEqual(got, want) {
		t.Errorf("reparse of %q = %v, in-memory view = %v", f.Bytes(), got, want)
	}
}

func TestGetSetDeleteKeys(t *testing.T) {
	t.Parallel()
	f, err := Parse([]byte("A=1\nB=2\nA=3\n"))
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := f.Get("A"); !ok || v != "3" {
		t.Errorf("Get(A) = %q,%v; want 3,true", v, ok)
	}
	if _, ok := f.Get("Z"); ok {
		t.Error("Get(Z) reported present")
	}
	f.Set("C", "4")
	f.Set("A", "5")
	f.Delete("B")
	if got, want := f.Keys(), []string{"A", "C"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Keys() = %v, want %v", got, want)
	}
	if _, ok := f.Get("B"); ok {
		t.Error("Get(B) after Delete reported present")
	}
	if v, _ := f.Get("A"); v != "5" {
		t.Errorf("Get(A) = %q, want 5", v)
	}
	assertReparse(t, f)
}

func TestSetPanicsOnInvalidInput(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, k, v string }{
		{name: "invalid name", k: "A-B", v: "1"},
		{name: "empty name", k: "", v: "1"},
		{name: "NUL in value", k: "A", v: "a\x00b"},
		{name: "invalid utf-8 value", k: "A", v: "\xff"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if recover() == nil {
					t.Errorf("Set(%q, %q) did not panic", tc.k, tc.v)
				}
			}()
			f, _ := Parse(nil)
			f.Set(tc.k, tc.v)
		})
	}
}

func TestValidNameAndCheckValue(t *testing.T) {
	t.Parallel()
	names := map[string]bool{"A": true, "_x9": true, "a_B": true, "": false, "9A": false, "A-B": false, "A B": false, "Ä": false}
	for n, want := range names {
		if got := ValidName(n); got != want {
			t.Errorf("ValidName(%q) = %v, want %v", n, got, want)
		}
	}
	values := map[string]bool{"": true, "x y\n\t$`\"\\": true, "ключ": true, "a\x00": false, "\xff": false}
	for v, ok := range values {
		if err := CheckValue(v); (err == nil) != ok {
			t.Errorf("CheckValue(%q) = %v, want ok=%v", v, err, ok)
		}
	}
}
