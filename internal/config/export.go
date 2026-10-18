package config

import (
	"slices"
	"strings"
)

// ExportShell renders the effective non-secret values as KEY='…' lines,
// sorted by key, safe to eval in a POSIX shell (cream config export --shell).
func (d *Domain) ExportShell() (string, error) {
	vals, err := d.Values()
	if err != nil {
		return "", err
	}
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(shellQuote(vals[k]))
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// shellQuote wraps v in single quotes and turns each single quote into the
// sequence: close quote, backslash-quote, open quote.
// Nothing inside single quotes is expanded by a POSIX shell.
func shellQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}
