package systemd

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/iamletenkov/cream/internal/paths"
)

func TestInit(t *testing.T) {
	t.Parallel()
	l := paths.Layout{Root: t.TempDir()}
	for range 2 { // idempotent
		if err := Init(l); err != nil {
			t.Fatal(err)
		}
	}
	dirs := []struct {
		path string
		mode fs.FileMode
	}{
		{filepath.Dir(l.UnitTemplate()), 0o755},
		{filepath.Dir(l.ExtTree("x")), 0o755},
		{filepath.Dir(l.EtcDir("x")), 0o755},
		{filepath.Dir(l.InstalledDir()), 0o755},
		{l.InstalledDir(), 0o755},
		{filepath.Dir(l.StateDir("x")), 0o755},
		{l.LockDir(), 0o700},
		{filepath.Dir(l.Incoming("x")), 0o700},
	}
	for _, d := range dirs {
		st, err := os.Stat(d.path)
		if err != nil {
			t.Fatal(err)
		}
		if !st.IsDir() || st.Mode().Perm() != d.mode {
			t.Errorf("%s: %v, want dir %#o", d.path, st.Mode(), d.mode)
		}
	}
	got, err := os.ReadFile(l.UnitTemplate())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, UnitTemplate()) {
		t.Errorf("template mismatch:\n%s", got)
	}
	assertMode(t, l.UnitTemplate(), 0o644)
}

func TestInitFixesModes(t *testing.T) {
	t.Parallel()
	l := paths.Layout{Root: t.TempDir()}
	if err := os.MkdirAll(l.LockDir(), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(l.LockDir(), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(l.UnitTemplate()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.UnitTemplate(), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Init(l); err != nil {
		t.Fatal(err)
	}
	assertMode(t, l.LockDir(), 0o700)
	assertMode(t, l.UnitTemplate(), 0o644)
	if got, _ := os.ReadFile(l.UnitTemplate()); !bytes.Equal(got, UnitTemplate()) {
		t.Errorf("stale template kept")
	}
}

// Init recomputes the target of every enabled instance from its drop-in:
// a lost or misplaced link comes back into the stage's wants directory.
func TestInitRelinksTargets(t *testing.T) {
	t.Parallel()
	l := paths.Layout{Root: t.TempDir()}
	if _, err := Enable(l, "docker", StageSpec{Stage: "system"}); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(l.WantsDir("multi-user.target"), "cream-configure@docker.service")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(l.WantsDir("graphical.target"), "cream-configure@docker.service")
	if err := os.MkdirAll(filepath.Dir(stray), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(templateLink, stray); err != nil {
		t.Fatal(err)
	}
	if err := Init(l); err != nil {
		t.Fatal(err)
	}
	if dst, err := os.Readlink(link); err != nil || dst != templateLink {
		t.Errorf("link not restored: %q %v", dst, err)
	}
	if _, err := os.Lstat(stray); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stray link kept: %v", err)
	}
}

func TestInitRejectsUnknownStageInDropIn(t *testing.T) {
	t.Parallel()
	l := paths.Layout{Root: t.TempDir()}
	p := l.DropIn("docker")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("[Unit]\nX-CreamStage=martian\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Init(l); err == nil {
		t.Fatal("want error")
	}
}
