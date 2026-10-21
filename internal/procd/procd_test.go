package procd

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/paths"
)

func layout(t *testing.T) paths.Layout {
	t.Helper()
	return paths.Layout{Root: t.TempDir(), Family: paths.FamilyOpenWrt}
}

func TestTarget(t *testing.T) {
	t.Parallel()
	for stage, want := range map[string]string{
		"early":      "S19cream-configure-early",
		"system":     "S95cream-configure",
		"cloud-user": "S95cream-configure",
	} {
		got, err := Target(stage)
		if err != nil || got != want {
			t.Errorf("Target(%s) = %q, %v; want %q", stage, got, err, want)
		}
	}
	for _, stage := range []string{"graphical", "nope", ""} {
		_, err := Target(stage)
		var ce *cerr.Error
		if !errors.As(err, &ce) || ce.Code != CodeStageUnsupported || ce.Exit != cerr.ExitData {
			t.Errorf("Target(%q) = %v; want 65 %s", stage, err, CodeStageUnsupported)
		}
	}
}

func TestInitWritesScriptsAndLinks(t *testing.T) {
	t.Parallel()
	l := layout(t)
	if err := Init(l); err != nil {
		t.Fatal(err)
	}
	for _, s := range scripts {
		p := l.InitScript(s.name)
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o755 {
			t.Errorf("%s: mode %v, want 0755", p, st.Mode().Perm())
		}
		body, _ := os.ReadFile(p)
		if !strings.HasPrefix(string(body), "#!/bin/sh /etc/rc.common\n") {
			t.Errorf("%s: no rc.common shebang:\n%s", p, body)
		}
		link := l.RcLink(s.link())
		dst, err := os.Readlink(link)
		if err != nil || dst != "../init.d/"+s.name {
			t.Errorf("%s -> %q, %v; want ../init.d/%s", link, dst, err, s.name)
		}
	}
	for _, d := range []string{l.HookTmp(), l.StageDir("early"), l.StageDir("system"), l.StageDir("cloud-user"), l.InstalledDir(), l.LockDir()} {
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			t.Errorf("%s: not a directory (%v)", d, err)
		}
	}
	if st, _ := os.Stat(l.HookTmp()); st.Mode()&fs.ModeSticky == 0 || st.Mode().Perm() != 0o777 {
		t.Errorf("HookTmp mode %v, want sticky 0777", st.Mode())
	}
	// Повтор — без изменений; чужое содержимое скрипта переписывается.
	if err := os.WriteFile(l.InitScript("cream-configure"), []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Init(l); err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(l.InitScript("cream-configure")); string(body) != string(scripts[1].body()) {
		t.Errorf("script not rewritten:\n%s", body)
	}
}

func TestScriptBody(t *testing.T) {
	t.Parallel()
	early := string(scripts[0].body())
	for _, want := range []string{"START=19\n", `stages="early"`, "/opt/cream/var/stages/", "/opt/cream/var/installed/",
		"/usr/bin/cream configure --from-unit"} {
		if !strings.Contains(early, want) {
			t.Errorf("early script lacks %q:\n%s", want, early)
		}
	}
	if !strings.Contains(early, "rm -rf "+guestTmp+"/* ") {
		t.Errorf("early script does not empty TMPDIR of hooks:\n%s", early)
	}
	system := string(scripts[1].body())
	if strings.Contains(system, "rm -rf") {
		t.Errorf("system script must not clean:\n%s", system)
	}
	for _, want := range []string{"START=95\n", `stages="system cloud-user"`} {
		if !strings.Contains(system, want) {
			t.Errorf("system script lacks %q:\n%s", want, system)
		}
	}
	if strings.Contains(system, "USE_PROCD") {
		t.Errorf("configure must run synchronously in rcS, not as a procd instance:\n%s", system)
	}
}

func TestEnableDisable(t *testing.T) {
	t.Parallel()
	l := layout(t)
	target, err := Enable(l, "demo", "system", false)
	if err != nil || target != "S95cream-configure" {
		t.Fatalf("Enable = %q, %v", target, err)
	}
	if _, err := os.Stat(l.StageMark("system", "demo")); err != nil {
		t.Fatal(err)
	}
	// Enable ставит скрипт стадии и ссылку сам: фаза build без system init.
	if _, err := os.Readlink(l.RcLink("S95cream-configure")); err != nil {
		t.Errorf("rc.d link: %v", err)
	}
	// Смена стадии убирает старую отметку.
	if target, err = Enable(l, "demo", "early", false); err != nil || target != "S19cream-configure-early" {
		t.Fatalf("Enable early = %q, %v", target, err)
	}
	if _, err := os.Stat(l.StageMark("system", "demo")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("system mark left: %v", err)
	}
	if err := Disable(l, "demo"); err != nil {
		t.Fatal(err)
	}
	for _, st := range []string{"early", "system", "cloud-user"} {
		if _, err := os.Stat(l.StageMark(st, "demo")); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s mark left: %v", st, err)
		}
	}
	// Повторное снятие — не ошибка.
	if err := Disable(l, "demo"); err != nil {
		t.Fatal(err)
	}
}

func TestEnableRefuses(t *testing.T) {
	t.Parallel()
	l := layout(t)
	for _, tc := range []struct {
		stage   string
		ordered bool
	}{{"graphical", false}, {"system", true}} {
		_, err := Enable(l, "demo", tc.stage, tc.ordered)
		var ce *cerr.Error
		if !errors.As(err, &ce) || ce.Exit != cerr.ExitData {
			t.Errorf("Enable(%s, ordered=%v) = %v; want 65", tc.stage, tc.ordered, err)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(l.Root, "opt/cream/var/stages")); len(entries) != 0 {
		t.Errorf("refused Enable left stage dirs: %v", entries)
	}
}
