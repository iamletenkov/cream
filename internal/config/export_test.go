package config

import (
	"strings"
	"testing"
)

func TestShellQuote(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{"", "''"},
		{"plain", "'plain'"},
		{"it's", `'it'\''s'`},
		{"$(rm -rf /)", "'$(rm -rf /)'"},
		{"`id`", "'`id`'"},
		{"a\nb", "'a\nb'"},
		{`\"`, `'\"'`},
		{"''", `''\'''\'''`},
	}
	for _, tc := range tests {
		if got := shellQuote(tc.in); got != tc.want {
			t.Errorf("shellQuote(%q) = %s, ожидали %s", tc.in, got, tc.want)
		}
	}
}

func TestExportShell(t *testing.T) {
	t.Parallel()
	d, l := newDomain(t)
	writeFile(t, l.ConfigFile(testExt), "LABEL_x=\"it's $(id) `w`\"\nPORT=9000\nTOKEN=misplaced\n", 0o644)
	writeFile(t, l.SecretsFile(testExt), "TOKEN=hunter2\n", 0o600)
	got, err := d.ExportShell()
	if err != nil {
		t.Fatal(err)
	}
	want := "FLAG='0'\nHOST=''\nLABEL_x='it'\\''s $(id) `w`'\nMODE='a'\nPORT='9000'\nRO='x'\nVERSION='1.0'\n"
	if got != want {
		t.Errorf("ExportShell:\n%s\nожидали:\n%s", got, want)
	}
	for _, s := range []string{"hunter2", "misplaced", "TOKEN"} {
		if strings.Contains(got, s) {
			t.Errorf("в экспорте %q", s)
		}
	}
}
