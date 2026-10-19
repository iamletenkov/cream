package systemd

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
		name, ext, golden, target string
		st                        StageSpec
	}{
		{"plain", "docker", "system-plain.conf", "multi-user.target", StageSpec{Stage: "system"}},
		{"ordered", "x11vnc", "system-ordered.conf", "multi-user.target", StageSpec{
			Stage:  "system",
			After:  []string{"display-manager.service", "docker.service", "network.target"},
			Before: []string{"teleport.service"},
			Wants:  []string{"display-manager.service"},
		}},
		// §6.4, cream 1.1.0: early — до сети, без зависимостей по умолчанию.
		{"early", "firewall", "early-plain.conf", "sysinit.target", StageSpec{Stage: "early"}},
		{"early ordered", "firewall", "early-ordered.conf", "sysinit.target", StageSpec{
			Stage:  "early",
			After:  []string{"systemd-modules-load.service", "local-fs.target"},
			Before: []string{"sysinit.target", "ssh.service"},
			Wants:  []string{"network-pre.target"},
		}},
		// cream 1.1.1: экземпляр тянет вспомогательный юнит (TestEnableCloudUserTarget).
		{"cloud-user", "code-server", "cloud-user-plain.conf", CloudUserUnit, StageSpec{Stage: "cloud-user"}},
		{"graphical", "x11vnc", "graphical-ordered.conf", "graphical.target", StageSpec{
			Stage: "graphical",
			After: []string{"gdm.service", "display-manager.service"},
			Wants: []string{"display-manager.service"},
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
			if target != tc.target {
				t.Errorf("target = %q, want %q", target, tc.target)
			}
			got, err := os.ReadFile(l.DropIn(tc.ext))
			if err != nil {
				t.Fatal(err)
			}
			if want := readGolden(t, tc.golden); !bytes.Equal(got, want) {
				t.Errorf("drop-in mismatch\n--- got\n%s\n--- want\n%s", got, want)
			}
			assertMode(t, l.DropIn(tc.ext), 0o644)
			link := filepath.Join(l.WantsDir(tc.target), "cream-configure@"+tc.ext+".service")
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
	for _, stage := range []string{"bogus", "late", "System", ""} {
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

// cloud-user (§6.4, cream 1.1.1): экземпляр — в cream-configure-cloud-user.service.wants
// при любом состоянии cloud-init; вспомогательный юнит (без After= на экземпляры) —
// в multi-user.target.wants. Так configure запускается и при выключенном cloud-init
// (cloud-init.target на загрузке не достигается), а при работающем ждёт cloud-final
// своим After=. Ссылка вспомогательного юнита уходит с последним экземпляром.
func TestEnableCloudUserTarget(t *testing.T) {
	t.Parallel()
	l := paths.Layout{Root: t.TempDir()}
	wants := func(target, ext string) string {
		return filepath.Join(l.WantsDir(target), "cream-configure@"+ext+".service")
	}
	puller := filepath.Join(l.WantsDir("multi-user.target"), CloudUserUnit)
	// Ссылка 1.1.0 в cloud-init.target.wants переезжает.
	writeFile(t, l.DropIn("wallpaper"), "[Unit]\n")
	if err := os.MkdirAll(l.WantsDir("cloud-init.target"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(templateLink, wants("cloud-init.target", "wallpaper")); err != nil {
		t.Fatal(err)
	}
	for _, ext := range []string{"wallpaper", "code-server"} {
		target, err := Enable(l, ext, StageSpec{Stage: "cloud-user"})
		if err != nil || target != CloudUserUnit {
			t.Fatalf("%s: %q %v", ext, target, err)
		}
		if dst, err := os.Readlink(wants(CloudUserUnit, ext)); err != nil || dst != templateLink {
			t.Errorf("%s: ссылка в %s.wants: %q %v", ext, CloudUserUnit, dst, err)
		}
	}
	for _, tg := range []string{"cloud-init.target", "multi-user.target"} {
		if _, err := os.Lstat(wants(tg, "wallpaper")); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("ссылка в %s.wants осталась: %v", tg, err)
		}
	}
	if dst, err := os.Readlink(puller); err != nil || dst != "/usr/lib/systemd/system/"+CloudUserUnit {
		t.Errorf("вспомогательный юнит в multi-user.target.wants: %q %v", dst, err)
	}
	if got, err := os.ReadFile(l.CloudUserUnit()); err != nil || !bytes.Equal(got, CloudUserUnitFile()) {
		t.Errorf("файл вспомогательного юнита: %v\n%s", err, got)
	}
	assertMode(t, l.CloudUserUnit(), 0o644)
	// Стадия сменилась на graphical (upgrade расширения): ссылка переезжает,
	// вспомогательный юнит остаётся — его ещё ждёт code-server.
	if target, err := Enable(l, "wallpaper", StageSpec{Stage: "graphical"}); err != nil || target != "graphical.target" {
		t.Fatalf("graphical: %q %v", target, err)
	}
	if _, err := os.Lstat(wants(CloudUserUnit, "wallpaper")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("ссылка в %s.wants осталась: %v", CloudUserUnit, err)
	}
	if _, err := os.Lstat(puller); err != nil {
		t.Errorf("вспомогательный юнит снят при живом экземпляре: %v", err)
	}
	for _, ext := range []string{"wallpaper", "code-server"} {
		if err := Disable(l, ext); err != nil {
			t.Fatal(err)
		}
		for _, tg := range knownTargets() {
			if _, err := os.Lstat(wants(tg, ext)); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("%s: %s: ссылка после Disable: %v", ext, tg, err)
			}
		}
	}
	if _, err := os.Lstat(puller); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("вспомогательный юнит в multi-user.target.wants после последнего Disable: %v", err)
	}
	if _, err := os.Lstat(l.WantsDir(CloudUserUnit)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("пустой %s.wants остался: %v", CloudUserUnit, err)
	}
}

// Вспомогательный юнит не упорядочен относительно экземпляров: After= на них
// вместе с After=cloud-final экземпляра дал бы цикл через multi-user.target.
func TestCloudUserUnitFile(t *testing.T) {
	t.Parallel()
	var body string
	for _, line := range strings.Split(string(CloudUserUnitFile()), "\n") {
		if !strings.HasPrefix(line, "#") {
			body += line + "\n"
		}
	}
	for _, bad := range []string{"After=", "Before=", "Requires=", "BindsTo=", "DefaultDependencies=no"} {
		if strings.Contains(body, bad) {
			t.Errorf("в юните есть %s:\n%s", bad, body)
		}
	}
	for _, want := range []string{"Type=oneshot", "RemainAfterExit=yes", "WantedBy=multi-user.target"} {
		if !strings.Contains(body, want) {
			t.Errorf("в юните нет %s", want)
		}
	}
}

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
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
