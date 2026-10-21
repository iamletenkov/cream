package cli

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode"
)

var manDate = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

func flagNames(register func(*flag.FlagSet)) []string {
	fs := newFlagSet("")
	register(fs)
	var out []string
	fs.VisitAll(func(f *flag.Flag) {
		if _, ok := f.Value.(reservedFlag); !ok {
			out = append(out, f.Name)
		}
	})
	return out
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestManDocsMatchHelp: the English prose of the man page covers exactly the
// commands and flags that --help knows — no command or flag without prose,
// no prose for a command or flag that does not exist.
func TestManDocsMatchHelp(t *testing.T) {
	t.Parallel()
	cmds := App{}.commands()
	var names []string
	for _, c := range cmds {
		names = append(names, c.Name)
		var flags []string
		if c.Flags != nil {
			flags = flagNames(c.Flags)
		}
		sort.Strings(flags)
		if got := keys(manFlags[c.Name]); !slices.Equal(got, flags) {
			t.Errorf("%s: manFlags = %v, flags of --help = %v", c.Name, got, flags)
		}
		if doc := manCommands[c.Name]; doc.Summary == "" || doc.Text == "" {
			t.Errorf("%s: no manCommands entry", c.Name)
		}
	}
	sort.Strings(names)
	if got := keys(manCommands); !slices.Equal(got, names) {
		t.Errorf("manCommands = %v, commands = %v", got, names)
	}
	for k := range manFlags {
		if !slices.Contains(names, k) {
			t.Errorf("manFlags has unknown command %q", k)
		}
	}
	g := Globals{}
	globals := flagNames(g.register)
	sort.Strings(globals)
	if got := keys(manGlobalFlags); !slices.Equal(got, globals) {
		t.Errorf("manGlobalFlags = %v, global flags = %v", got, globals)
	}
}

// TestManPageListsHelp: every command synopsis line of --help and every flag
// appears in cream(1).
func TestManPageListsHelp(t *testing.T) {
	t.Parallel()
	page := App{}.ManPages("1.2.3", manDate)["cream.1"]
	for _, c := range (App{}).commands() {
		if !strings.Contains(page, ".SS "+c.Name+"\n") {
			t.Errorf("cream.1: no section for %s", c.Name)
		}
		syn := synopses(c)
		if len(syn) == 0 {
			t.Errorf("%s: no synopsis in Usage %q", c.Name, c.Usage)
		}
		for _, s := range syn {
			if !strings.Contains(page, ".B cream "+roffEscape(s)+"\n") {
				t.Errorf("cream.1: no synopsis %q", s)
			}
		}
		if c.Flags == nil {
			continue
		}
		fs := newFlagSet("")
		c.Flags(fs)
		fs.VisitAll(func(f *flag.Flag) {
			dash := "\\-\\-"
			if len(f.Name) == 1 {
				dash = "\\-"
			}
			if !strings.Contains(page, "\\fB"+dash+roffEscape(f.Name)+"\\fR") {
				t.Errorf("cream.1: %s has no flag %s%s", c.Name, strings.ReplaceAll(dash, "\\", ""), f.Name)
			}
		})
	}
	for _, r := range reservedCommands {
		if !isReservedCommand(r) || !strings.Contains(page, r) {
			t.Errorf("reserved command %s", r)
		}
	}
}

// TestManExitCodes: every exit code constant of cerr is in EXIT STATUS.
func TestManExitCodes(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("../cerr/*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("cerr sources: %v", err)
	}
	re := regexp.MustCompile(`(?m)^\s+Exit\w+\s+=\s+(\d+)$`)
	var codes []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			codes = append(codes, m[1])
		}
	}
	if len(codes) < 10 {
		t.Fatalf("found only %v exit codes in cerr", codes)
	}
	for _, c := range codes {
		if !slices.ContainsFunc(manExitCodes, func(e struct{ Code, Text string }) bool { return e.Code == c }) {
			t.Errorf("exit code %s missing from cream(1)", c)
		}
	}
}

// TestManPagesWellFormed: ASCII only (no Russian help text leaks), a .TH with
// the given date and version, deterministic output.
func TestManPagesWellFormed(t *testing.T) {
	t.Parallel()
	pages := App{}.ManPages("1.2.3", manDate)
	if len(pages) != 2 {
		t.Fatalf("pages = %v", keys(pages))
	}
	again := App{}.ManPages("1.2.3", manDate)
	for name, page := range pages {
		if page != again[name] {
			t.Errorf("%s: not deterministic", name)
		}
		if !strings.HasPrefix(page, ".TH ") || !strings.Contains(strings.SplitN(page, "\n", 2)[0], `2026-10-18 "cream 1.2.3"`) {
			t.Errorf("%s: bad .TH line %q", name, strings.SplitN(page, "\n", 2)[0])
		}
		for i, r := range page {
			if r > unicode.MaxASCII {
				t.Errorf("%s: non-ASCII %q at %d: %q", name, r, i, page[max(0, i-40):min(len(page), i+40)])
				break
			}
		}
		for _, sec := range []string{".SH NAME\n", ".SH SYNOPSIS\n", ".SH DESCRIPTION\n", ".SH SEE ALSO\n"} {
			if !strings.Contains(page, sec) {
				t.Errorf("%s: no %q", name, sec)
			}
		}
	}
	for _, sec := range []string{".SH COMMANDS\n", ".SH GLOBAL OPTIONS\n", ".SH ENVIRONMENT\n", ".SH FILES\n", ".SH EXIT STATUS\n", ".SH EXAMPLES\n"} {
		if !strings.Contains(pages["cream.1"], sec) {
			t.Errorf("cream.1: no %q", sec)
		}
	}
}
