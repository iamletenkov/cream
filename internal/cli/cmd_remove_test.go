package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iamletenkov/cream/internal/paths"
)

// TestGoldenRemove — формы result и ошибок `cream remove` (§6.5, §11.4).
func TestGoldenRemove(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		args     []string
		seed     []string
		wantExit int
	}{
		{name: "remove", args: []string{"remove", "--json", "noop"}, seed: []string{"noop"}},
		{name: "remove-purge", args: []string{"--json", "remove", "--purge", "with-config"}, seed: []string{"with-config"}},
		{name: "remove-forget", args: []string{"remove", "noop", "--forget", "--json"}, seed: []string{"noop"}},
		{name: "remove-dependents", args: []string{"remove", "--json", "lib-fixture"},
			seed: []string{"lib-fixture", "depends-lib"}, wantExit: 67},
		{name: "remove-force", args: []string{"remove", "--json", "--force", "lib-fixture"},
			seed: []string{"lib-fixture", "depends-lib"}},
		{name: "remove-not-installed", args: []string{"remove", "--json", "noop"}, wantExit: 66},
		{name: "remove-purge-forget", args: []string{"remove", "--json", "--purge", "--forget", "noop"},
			seed: []string{"noop"}, wantExit: 64},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			for _, ext := range tc.seed {
				seedRoot(t, root, ext)
			}
			out := runCLI(t, cliIn{Root: root}, tc.args...)
			if out.Code != tc.wantExit {
				t.Errorf("exit = %d, want %d; stderr:\n%s", out.Code, tc.wantExit, out.Stderr)
			}
			golden(t, tc.name, normRoot(out.Stdout, root))
		})
	}
}

func TestRemoveCLI(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	seedRoot(t, root, "noop")
	l := paths.Layout{Root: root}

	if out := runCLI(t, cliIn{Root: root}, "remove"); out.Code != 64 {
		t.Errorf("без имени: %+v", out)
	}
	if out := runCLI(t, cliIn{Root: root}, "remove", "noop", "extra"); out.Code != 64 {
		t.Errorf("два имени: %+v", out)
	}
	// Без --json — строка для человека в stdout.
	out := runCLI(t, cliIn{Root: root}, "remove", "noop")
	if out.Code != 0 || !strings.Contains(out.Stdout, "noop") {
		t.Errorf("remove: %+v", out)
	}
	if _, err := os.Stat(l.InstalledFile("noop")); !os.IsNotExist(err) {
		t.Errorf("запись реестра осталась: %v", err)
	}
	if _, err := os.Stat(filepath.Join(l.StateDir("noop"), "remove.count")); err != nil {
		t.Errorf("хук remove не вызван: %v", err)
	}
}

// На настоящем корне remove требует root (77) до любых действий.
func TestRemoveCLINeedsRoot(t *testing.T) {
	t.Parallel()
	out := runCLI(t, cliIn{Euid: 1000}, "remove", "--json", "noop")
	if out.Code != 77 || !strings.Contains(out.Stdout, `"perm.root"`) {
		t.Errorf("не root: %+v", out)
	}
}
