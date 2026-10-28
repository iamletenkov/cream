package cli

import (
	"flag"
	"fmt"
	"strings"
)

// permute reorders args so that flags may follow positionals (spec §11.1):
// the result is flags…, "--", positionals…, ready for fs.Parse.
//
// "--" ends flag processing; everything after it is positional. A value flag
// known to fs takes the next argument as its value. Unknown flags are kept for
// fs.Parse to reject. With until > 0, once that many positionals are seen the
// rest is taken verbatim (cream run: nothing after <action> is a cream flag).
func permute(fs *flag.FlagSet, args []string, until int) ([]string, error) {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		if until > 0 && len(pos) >= until {
			pos = append(pos, args[i:]...)
			break
		}
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if len(a) < 2 || a[0] != '-' {
			pos = append(pos, a)
			continue
		}
		flags = append(flags, a)
		name := strings.TrimPrefix(a[1:], "-")
		if strings.Contains(name, "=") {
			continue
		}
		f := fs.Lookup(name)
		if f == nil || isBoolFlag(f) {
			continue
		}
		if i+1 >= len(args) {
			return nil, fmt.Errorf("флагу -%s нужно значение", name)
		}
		i++
		flags = append(flags, args[i])
	}
	out := make([]string, 0, len(flags)+1+len(pos))
	out = append(out, flags...)
	out = append(out, "--")
	return append(out, pos...), nil
}

func isBoolFlag(f *flag.Flag) bool {
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

// reservedFlag is a flag deferred past cream 1.0 (ruling Р1): it parses as a
// bool, and the dispatcher answers 64 usage.reserved when it is set.
type reservedFlag struct{}

func (reservedFlag) String() string   { return "false" }
func (reservedFlag) Set(string) error { return nil }
func (reservedFlag) IsBoolFlag() bool { return true }

// reserveFlags registers deferred flags (--require-pinned) on a command's flag set.
func reserveFlags(fs *flag.FlagSet, names ...string) {
	for _, n := range names {
		fs.Var(reservedFlag{}, n, "зарезервировано в cream/v1, не реализовано в cream 1.0")
	}
}

// usedReservedFlag returns the name of a reserved flag that was set, or "".
func usedReservedFlag(fs *flag.FlagSet) string {
	var name string
	fs.Visit(func(f *flag.Flag) {
		if _, ok := f.Value.(reservedFlag); ok && name == "" {
			name = f.Name
		}
	})
	return name
}
