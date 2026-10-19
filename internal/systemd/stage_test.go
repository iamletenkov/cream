package systemd

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/paths"
)

const templateLink = "/usr/lib/systemd/system/cream-configure@.service"

func readGolden(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "golden", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEnableDropIn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, ext, golden string
		st                StageSpec
	}{
		{"plain", "docker", "system-plain.conf", StageSpec{Stage: "system"}},
		{"ordered", "x11vnc", "system-ordered.conf", StageSpec{
			Stage:  "system",
			After:  []string{"display-manager.service", "docker.service", "network.target"},
			Before: []string{"teleport.service"},
			Wants:  []string{"display-manager.service"},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l := paths.Layout{Root: t.TempDir()}
			target, err := Enable(l, tc.ext, tc.st)
			if err != nil {
				t.Fatal(err)
			}
			if target != "multi-user.target" {
				t.Errorf("target = %q", target)
			}
			got, err := os.ReadFile(l.DropIn(tc.ext))
			if err != nil {
				t.Fatal(err)
			}
			if want := readGolden(t, tc.golden); !bytes.Equal(got, want) {
				t.Errorf("drop-in mismatch\n--- got\n%s\n--- want\n%s", got, want)
			}
			assertMode(t, l.DropIn(tc.ext), 0o644)
			link := filepath.Join(l.WantsDir("multi-user.target"), "cream-configure@"+tc.ext+".service")
			dst, err := os.Readlink(link)
			if err != nil {
				t.Fatal(err)
			}
			if dst != templateLink {
				t.Errorf("link → %q, want %q", dst, templateLink)
			}
		})
	}
}

func TestEnableIdempotentAndReplacesStale(t *testing.T) {
	t.Parallel()
	l := paths.Layout{Root: t.TempDir()}
	link := filepath.Join(l.WantsDir("multi-user.target"), "cream-configure@docker.service")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/elsewhere", link); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := Enable(l, "docker", StageSpec{Stage: "system"}); err != nil {
			t.Fatal(err)
		}
	}
	if dst, _ := os.Readlink(link); dst != templateLink {
		t.Errorf("link → %q", dst)
	}
}

func TestEnableUnknownStage(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"early", "cloud-user", "graphical", "bogus", ""} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			l := paths.Layout{Root: t.TempDir()}
			_, err := Enable(l, "docker", StageSpec{Stage: stage})
			if cerr.ExitCode(err) != cerr.ExitData || cerr.CodeOf(err) != CodeStageUnsupported {
				t.Fatalf("err = %v (exit %d, code %q)", err, cerr.ExitCode(err), cerr.CodeOf(err))
			}
			if _, err := os.Lstat(l.DropIn("docker")); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("drop-in written on error: %v", err)
			}
		})
	}
}

func TestEnableRejectsBadUnitName(t *testing.T) {
	t.Parallel()
	l := paths.Layout{Root: t.TempDir()}
	_, err := Enable(l, "docker", StageSpec{Stage: "system", After: []string{"a.service\nExecStart=/bin/x"}})
	if cerr.ExitCode(err) != cerr.ExitData {
		t.Fatalf("err = %v", err)
	}
}

func TestDisable(t *testing.T) {
	t.Parallel()
	l := paths.Layout{Root: t.TempDir()}
	if _, err := Enable(l, "docker", StageSpec{Stage: "system"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Enable(l, "teleport", StageSpec{Stage: "system"}); err != nil {
		t.Fatal(err)
	}
	// A drop-in of the administrator next to ours survives with its directory.
	own := filepath.Join(filepath.Dir(l.DropIn("teleport")), "20-local.conf")
	if err := os.WriteFile(own, []byte("[Service]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, ext := range []string{"docker", "teleport"} {
		for range 2 { // repeat is a no-op
			if err := Disable(l, ext); err != nil {
				t.Fatal(err)
			}
		}
	}
	wants := l.WantsDir("multi-user.target")
	for _, ext := range []string{"docker", "teleport"} {
		if _, err := os.Lstat(filepath.Join(wants, "cream-configure@"+ext+".service")); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s: link left: %v", ext, err)
		}
		if _, err := os.Lstat(l.DropIn(ext)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s: drop-in left: %v", ext, err)
		}
	}
	if _, err := os.Lstat(filepath.Dir(l.DropIn("docker"))); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("empty drop-in dir left: %v", err)
	}
	if _, err := os.Stat(own); err != nil {
		t.Errorf("foreign drop-in removed: %v", err)
	}
}

func TestDisableKeepsForeignFile(t *testing.T) {
	t.Parallel()
	l := paths.Layout{Root: t.TempDir()}
	wants := l.WantsDir("multi-user.target")
	if err := os.MkdirAll(wants, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(wants, "cream-configure@docker.service")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Disable(l, "docker"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("regular file removed: %v", err)
	}
}

func assertMode(t *testing.T, p string, want fs.FileMode) {
	t.Helper()
	st, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Mode().Perm(); got != want {
		t.Errorf("%s: mode %#o, want %#o", p, got, want)
	}
}
