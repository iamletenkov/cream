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

// cream 1.1.1: Init переносит экземпляры cloud-user из целей 1.1.0
// (cloud-init.target.wants или multi-user.target.wants) в
// cream-configure-cloud-user.service.wants и пишет вспомогательный юнит; наличие
// cloud-init на цель больше не влияет.
func TestInitMovesCloudUserToPuller(t *testing.T) {
	t.Parallel()
	for _, old := range []string{"cloud-init.target", "multi-user.target"} {
		t.Run(old, func(t *testing.T) {
			t.Parallel()
			l := paths.Layout{Root: t.TempDir()}
			if _, err := Enable(l, "code-server", StageSpec{Stage: "cloud-user"}); err != nil {
				t.Fatal(err)
			}
			if _, err := Enable(l, "firewall", StageSpec{Stage: "early"}); err != nil {
				t.Fatal(err)
			}
			link := func(target, ext string) string {
				return filepath.Join(l.WantsDir(target), "cream-configure@"+ext+".service")
			}
			// Раскладка 1.1.0: ссылка в старой цели, вспомогательного юнита нет.
			if err := os.Remove(link(CloudUserUnit, "code-server")); err != nil {
				t.Fatal(err)
			}
			for _, p := range []string{l.CloudUserUnit(), filepath.Join(l.WantsDir("multi-user.target"), CloudUserUnit)} {
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(l.WantsDir(old), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(templateLink, link(old, "code-server")); err != nil {
				t.Fatal(err)
			}
			if err := Init(l); err != nil {
				t.Fatal(err)
			}
			if dst, err := os.Readlink(link(CloudUserUnit, "code-server")); err != nil || dst != templateLink {
				t.Errorf("%s.wants: %q %v", CloudUserUnit, dst, err)
			}
			if _, err := os.Lstat(link(old, "code-server")); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("%s.wants осталась: %v", old, err)
			}
			if _, err := os.Readlink(filepath.Join(l.WantsDir("multi-user.target"), CloudUserUnit)); err != nil {
				t.Errorf("вспомогательный юнит не включён: %v", err)
			}
			if got, _ := os.ReadFile(l.CloudUserUnit()); !bytes.Equal(got, CloudUserUnitFile()) {
				t.Errorf("файл вспомогательного юнита: %s", got)
			}
			if _, err := os.Readlink(link("sysinit.target", "firewall")); err != nil {
				t.Errorf("early: %v", err)
			}
		})
	}
}

// Init снимает ссылку вспомогательного юнита, оставшуюся без экземпляров
// (раз после пересчёта всех ссылок), и не трогает её при живом экземпляре.
func TestInitDropsOrphanPuller(t *testing.T) {
	t.Parallel()
	l := paths.Layout{Root: t.TempDir()}
	if _, err := Enable(l, "docker", StageSpec{Stage: "system"}); err != nil {
		t.Fatal(err)
	}
	if err := enablePuller(l); err != nil {
		t.Fatal(err)
	}
	puller := filepath.Join(l.WantsDir("multi-user.target"), CloudUserUnit)
	if err := Init(l); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(puller); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("ссылка без экземпляров осталась: %v", err)
	}
	if _, err := Enable(l, "wallpaper", StageSpec{Stage: "cloud-user"}); err != nil {
		t.Fatal(err)
	}
	if err := Init(l); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(puller); err != nil {
		t.Errorf("ссылка снята при живом экземпляре: %v", err)
	}
}

// Init без экземпляров cloud-user вспомогательный юнит пишет (как шаблон), но
// не включает.
func TestInitWritesPullerUnitOnly(t *testing.T) {
	t.Parallel()
	l := paths.Layout{Root: t.TempDir()}
	if err := Init(l); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(l.CloudUserUnit()); !bytes.Equal(got, CloudUserUnitFile()) {
		t.Errorf("файл вспомогательного юнита: %s", got)
	}
	assertMode(t, l.CloudUserUnit(), 0o644)
	if _, err := os.Lstat(filepath.Join(l.WantsDir("multi-user.target"), CloudUserUnit)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("включён без экземпляров: %v", err)
	}
}

// Гонка с remove: Init прочёл стадию, и тут remove снял ссылку и drop-in —
// перевешанная Init ссылка не остаётся.
func TestRelinkAllRaceWithRemove(t *testing.T) {
	t.Parallel()
	l := paths.Layout{Root: t.TempDir()}
	if _, err := Enable(l, "docker", StageSpec{Stage: "system"}); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(l.WantsDir("multi-user.target"), "cream-configure@docker.service")
	removed := false
	err := relinkAll(l, func(ext string) {
		if ext == "docker" && !removed {
			removed = true
			if err := Disable(l, ext); err != nil {
				t.Fatal(err)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("hook not called")
	}
	if _, err := os.Lstat(link); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("ссылка снятого расширения осталась: %v", err)
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
