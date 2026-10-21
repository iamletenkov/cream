package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// upgradeCLIBump меняет версию расширения ext в каталоге dir на version.
func upgradeCLIBump(t *testing.T, dir, ext, version string) {
	t.Helper()
	mf := filepath.Join(dir, "extensions", ext, "extension.yaml")
	data, err := os.ReadFile(mf)
	if err != nil {
		t.Fatal(err)
	}
	out := strings.Replace(string(data), "version: 1.0.0", "version: "+version, 1)
	if out == string(data) {
		t.Fatalf("%s: версия не заменена", ext)
	}
	if err := os.WriteFile(mf, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
}

// upgradeCLIRoot — «Debian 12» с установленными noop, live-confirm (с
// --accept-risk) и upgrade-fixture 1.0.0; источник fixtures переключён на
// каталог, где upgrade-fixture 2.0.0, live-confirm — confirmNext ("" — та
// же 1.0.0), noop — та же версия.
func upgradeCLIRoot(t *testing.T, confirmNext string) string {
	t.Helper()
	root := t.TempDir()
	writeRootFile(t, root, "/etc/os-release", "ID=debian\nVERSION_ID=12\n", 0o644)
	first := installCLICatalog(t, root, "catalog", "1.0.0", map[string]string{
		"noop": fixtureDir(t, "noop"), "live-confirm": fixtureDir(t, "live-confirm"),
		"upgrade-fixture": fixtureDir(t, "upgrade-fixture")})
	installCLISources(t, root, first)
	for _, args := range [][]string{{"install", "noop"}, {"install", "--accept-risk", "live-confirm"},
		{"install", "upgrade-fixture"}} {
		if out := runCLI(t, cliIn{Root: root}, args...); out.Code != 0 {
			t.Fatalf("%v: %d\n%s", args, out.Code, out.Stderr)
		}
	}
	next := installCLICatalog(t, root, "next", "1.1.0", map[string]string{
		"noop": fixtureDir(t, "noop"), "live-confirm": fixtureDir(t, "live-confirm"),
		"upgrade-fixture": cliTestdataDir(t, "catalog-next", "extensions", "upgrade-fixture")})
	if confirmNext != "" {
		upgradeCLIBump(t, next, "live-confirm", confirmNext)
	}
	installCLISources(t, root, next)
	return root
}

// TestGoldenUpgrade — формы result upgrade (план, сводка) и hold/unhold.
func TestGoldenUpgrade(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		confirm  string // версия live-confirm в новом каталоге
		before   [][]string
		args     []string
		wantExit int
	}{
		// Ошибка 1.1.4: live: confirm без новой версии ронял --all с 68.
		{name: "upgrade-all-confirm-unchanged", args: []string{"upgrade", "--json", "--all"}},
		{name: "upgrade-partial", confirm: "1.1.0", args: []string{"upgrade", "--json", "--all"}, wantExit: 68},
		{name: "upgrade-accept-risk", confirm: "1.1.0",
			args: []string{"upgrade", "--json", "--all", "--accept-risk", "live-confirm"}},
		{name: "upgrade-dry-run", confirm: "1.1.0", args: []string{"upgrade", "--json", "--all", "--dry-run"}, wantExit: 68},
		{name: "upgrade-held", before: [][]string{{"hold", "upgrade-fixture"}},
			args: []string{"upgrade", "--json", "upgrade-fixture"}, wantExit: 68},
		{name: "upgrade-all-held", before: [][]string{{"hold", "upgrade-fixture"}},
			args: []string{"upgrade", "--json", "--all"}},
		{name: "hold", args: []string{"hold", "--json", "upgrade-fixture", "noop"}},
		{name: "unhold", before: [][]string{{"hold", "noop"}}, args: []string{"unhold", "--json", "noop", "upgrade-fixture"}},
		{name: "hold-not-installed", args: []string{"hold", "--json", "missing"}, wantExit: 66},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := upgradeCLIRoot(t, tc.confirm)
			for _, b := range tc.before {
				if out := runCLI(t, cliIn{Root: root}, b...); out.Code != 0 {
					t.Fatalf("%v: %d\n%s", b, out.Code, out.Stderr)
				}
			}
			out := runCLI(t, cliIn{Root: root}, tc.args...)
			if out.Code != tc.wantExit {
				t.Errorf("exit = %d, want %d; stderr:\n%s", out.Code, tc.wantExit, out.Stderr)
			}
			golden(t, tc.name, normRoot(out.Stdout, root))
		})
	}
}

func TestUpgradeText(t *testing.T) {
	t.Parallel()
	root := upgradeCLIRoot(t, "1.1.0")
	out := runCLI(t, cliIn{Root: root}, "upgrade", "--all", "--dry-run")
	if out.Code != 68 {
		t.Errorf("dry-run: %d\n%s", out.Code, out.Stderr)
	}
	for _, want := range []string{
		"upgrade-fixture  1.0.0 → 2.0.0  будет обновлено\n",
		"noop             1.0.0          без изменений\n",
		"live-confirm     1.0.0 → 1.1.0  пропущено (риск): ",
		"--accept-risk live-confirm",
		"План: будет обновлено 1, без изменений 1, пропущено 1 (риск: live-confirm), ошибок 0\n",
	} {
		if !strings.Contains(out.Stdout, want) {
			t.Errorf("нет %q в\n%s", want, out.Stdout)
		}
	}
	out = runCLI(t, cliIn{Root: root}, "upgrade", "--all")
	if out.Code != 68 || !strings.Contains(out.Stdout, "Итого: обновлено 1, без изменений 1, пропущено 1 (риск: live-confirm), ошибок 0\n") {
		t.Errorf("upgrade --all: %d\n%s\n%s", out.Code, out.Stdout, out.Stderr)
	}
	// Единственное имя — прежняя строка install.
	root = upgradeCLIRoot(t, "")
	out = runCLI(t, cliIn{Root: root}, "upgrade", "upgrade-fixture")
	if out.Code != 0 || out.Stdout != "upgrade-fixture 2.0.0: installed (изменено)\n" {
		t.Errorf("upgrade одного: %+v", out)
	}
	if out := runCLI(t, cliIn{Root: root}, "upgrade", "--all", "--accept-risk"); out.Code != 64 {
		t.Errorf("--accept-risk без значения: %+v", out)
	}
	if out := runCLI(t, cliIn{Root: root}, "hold"); out.Code != 64 {
		t.Errorf("hold без имён: %+v", out)
	}
	if out := runCLI(t, cliIn{Root: root}, "hold", "noop"); out.Code != 0 || out.Stdout != "noop 1.0.0: закреплено\n" {
		t.Errorf("hold: %+v", out)
	}
	if out := runCLI(t, cliIn{Root: root}, "hold", "noop"); out.Code != 0 || out.Stdout != "noop 1.0.0: уже закреплено\n" {
		t.Errorf("повтор hold: %+v", out)
	}
	if out := runCLI(t, cliIn{Root: root}, "list"); !strings.Contains(out.Stdout, "закреплено") {
		t.Errorf("list не показывает hold:\n%s", out.Stdout)
	}
	if out := runCLI(t, cliIn{Euid: 1000}, "hold", "noop"); out.Code != 77 {
		t.Errorf("hold не root: %+v", out)
	}
}

func TestUpgradeInteractive(t *testing.T) {
	t.Parallel()
	t.Run("риск и план — нет: ничего не сделано, 1", func(t *testing.T) {
		t.Parallel()
		root := upgradeCLIRoot(t, "1.1.0")
		out := runCLI(t, cliIn{Root: root, TTY: true, Stdin: "n\nn\n"}, "upgrade", "--all")
		if out.Code != 1 || !strings.Contains(out.Stderr, "[y/N]") || !strings.Contains(out.Stderr, "upgrade.declined") &&
			!strings.Contains(out.Stderr, "отменено") {
			t.Errorf("%d\n%s\n%s", out.Code, out.Stdout, out.Stderr)
		}
		if !strings.Contains(out.Stderr, "live-confirm: targets.live: confirm — фикстура: на живой машине нужен --accept-risk") {
			t.Errorf("нет вопроса о риске:\n%s", out.Stderr)
		}
		if out := runCLI(t, cliIn{Root: root}, "upgrade", "--all", "--dry-run"); !strings.Contains(out.Stdout, "будет обновлено 1") {
			t.Errorf("после отказа что-то обновлено:\n%s", out.Stdout)
		}
	})
	t.Run("да на оба — всё обновлено", func(t *testing.T) {
		t.Parallel()
		root := upgradeCLIRoot(t, "1.1.0")
		out := runCLI(t, cliIn{Root: root, TTY: true, Stdin: "y\nyes\n"}, "upgrade", "--all")
		if out.Code != 0 || !strings.Contains(out.Stdout, "Итого: обновлено 2, без изменений 1, пропущено 0, ошибок 0") {
			t.Errorf("%d\n%s\n%s", out.Code, out.Stdout, out.Stderr)
		}
	})
	t.Run("-y снимает вопрос, но не риск", func(t *testing.T) {
		t.Parallel()
		root := upgradeCLIRoot(t, "1.1.0")
		out := runCLI(t, cliIn{Root: root, TTY: true}, "upgrade", "--all", "-y")
		if out.Code != 68 || strings.Contains(out.Stderr, "[y/N]") ||
			!strings.Contains(out.Stdout, "обновлено 1, без изменений 1, пропущено 1 (риск: live-confirm)") {
			t.Errorf("%d\n%s\n%s", out.Code, out.Stdout, out.Stderr)
		}
	})
	t.Run("--json не спрашивает", func(t *testing.T) {
		t.Parallel()
		root := upgradeCLIRoot(t, "")
		out := runCLI(t, cliIn{Root: root, TTY: true}, "upgrade", "--all", "--json")
		if out.Code != 0 || strings.Contains(out.Stderr, "[y/N]") {
			t.Errorf("%d\n%s", out.Code, out.Stderr)
		}
	})
}
