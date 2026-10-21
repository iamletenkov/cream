package cli

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iamletenkov/cream/internal/state"
)

// TestGoldenConfig — формы result команд настроек и system (§11.4).
func TestGoldenConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		args     []string
		stdin    string
		wantExit int
	}{
		{name: "list", args: []string{"list", "--json"}},
		{name: "show", args: []string{"show", "--json", "with-config"}},
		{name: "show-not-installed", args: []string{"show", "--json", "noop"}, wantExit: 66},
		{name: "get", args: []string{"get", "--json", "with-config", "COUNT"}},
		{name: "set", args: []string{"set", "--json", "with-config", "COUNT=7", "TOKEN=-"}, stdin: "tok\n"},
		{name: "set-apply-hook", args: []string{"set", "apply-hook", "LEVEL=high", "--json"}},
		{name: "set-no-apply", args: []string{"set", "--no-apply", "--json", "apply-hook", "LEVEL=high"}},
		{name: "set-unchanged", args: []string{"set", "--json", "with-config", "MODE=fast"}},
		{name: "set-secret-in-argv", args: []string{"set", "--json", "with-config", "TOKEN=x"}, wantExit: 64},
		{name: "config-export", args: []string{"config", "export", "--shell", "--json", "with-config"}},
		{name: "configure", args: []string{"configure", "--json", "configure-system"}},
		{name: "configure-force-all", args: []string{"configure", "--force", "--all", "--json"}},
		{name: "show-configure", args: []string{"show", "--json", "configure-system"}},
		{name: "system-init", args: []string{"system", "init", "--json"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			for _, ext := range []string{"with-config", "apply-hook", "configure-system", "action-json"} {
				seedRoot(t, root, ext)
			}
			out := runCLI(t, cliIn{Root: root, Stdin: tc.stdin}, tc.args...)
			if out.Code != tc.wantExit {
				t.Errorf("exit = %d, want %d; stderr:\n%s", out.Code, tc.wantExit, out.Stderr)
			}
			golden(t, tc.name, normRoot(out.Stdout, root))
		})
	}
}

func TestConfigCommandsText(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	seedRoot(t, root, "with-config")
	in := cliIn{Root: root}
	if out := runCLI(t, in, "get", "with-config", "MODE"); out.Code != 0 || out.Stdout != "fast\n" {
		t.Errorf("get: %+v", out)
	}
	if out := runCLI(t, in, "config", "get", "with-config", "COUNT"); out.Code != 0 || out.Stdout != "42\n" {
		t.Errorf("config get: %+v", out)
	}
	in.Stdin = "secret-tok"
	if out := runCLI(t, in, "set", "with-config", "TOKEN=-", "GREETING=hi there"); out.Code != 0 {
		t.Fatalf("set: %+v", out)
	}
	in.Stdin = ""
	if out := runCLI(t, in, "get", "--secret", "with-config", "TOKEN"); out.Code != 0 || out.Stdout != "secret-tok\n" {
		t.Errorf("get --secret: %+v", out)
	}
	out := runCLI(t, in, "show", "with-config")
	if out.Code != 0 || !strings.Contains(out.Stdout, "GREETING=hi there") || strings.Contains(out.Stdout, "secret-tok") ||
		!strings.Contains(out.Stdout, "TOKEN=(задан)") {
		t.Errorf("show:\n%s", out.Stdout)
	}
	out = runCLI(t, in, "config", "export", "with-config", "--shell")
	if out.Code != 0 || !strings.Contains(out.Stdout, "GREETING='hi there'\n") || strings.Contains(out.Stdout, "secret-tok") {
		t.Errorf("export:\n%s", out.Stdout)
	}
	if out := runCLI(t, in, "list"); out.Code != 0 || !strings.Contains(out.Stdout, "with-config") {
		t.Errorf("list: %+v", out)
	}
	if strings.Contains(out.Stderr, "secret-tok") {
		t.Errorf("секрет в stderr")
	}
}

func TestListTextRebootRequired(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	seedRoot(t, root, "noop", func(r *state.Record) { r.RebootRequired = true })
	out := runCLI(t, cliIn{Root: root}, "list")
	if out.Code != 0 || !strings.Contains(out.Stdout, "noop") || !strings.Contains(out.Stdout, "нужна перезагрузка") {
		t.Errorf("list: %+v", out)
	}
}

func TestConfigCommandsParamsFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	seedRoot(t, root, "with-config")
	pf := filepath.Join(t.TempDir(), "params")
	if err := os.WriteFile(pf, []byte("TOKEN=from-file\nCOUNT=9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := runCLI(t, cliIn{Root: root}, "set", "with-config", "--params-file", pf); out.Code != 0 {
		t.Fatalf("set --params-file: %+v", out)
	}
	if out := runCLI(t, cliIn{Root: root}, "get", "--secret", "with-config", "TOKEN"); out.Stdout != "from-file\n" {
		t.Errorf("TOKEN: %+v", out)
	}
}

func TestConfigCommandsCodes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		root     string // "/" — настоящий корень, иначе временный
		euid     int
		args     []string
		wantExit int
		wantCode string
	}{
		{name: "show-no-ext", args: []string{"show"}, wantExit: 64, wantCode: "usage.args"},
		{name: "show-extra", args: []string{"show", "a", "b"}, wantExit: 64, wantCode: "usage.args"},
		{name: "show-bad-name", args: []string{"show", "../etc"}, wantExit: 64, wantCode: "usage.ext_name"},
		{name: "show-other", args: []string{"show", "noop"}, wantExit: 66, wantCode: "not_found.extension"},
		{name: "get-no-key", args: []string{"get", "with-config"}, wantExit: 64, wantCode: "usage.args"},
		{name: "get-unknown-key", args: []string{"get", "with-config", "NOPE"}, wantExit: 66, wantCode: "not_found.key"},
		{name: "get-secret-no-flag", args: []string{"get", "with-config", "TOKEN"}, wantExit: 64, wantCode: "usage.secret"},
		{name: "get-secret-not-root", root: "/", euid: 1000, args: []string{"get", "--secret", "with-config", "TOKEN"}, wantExit: 77, wantCode: "perm.root"},
		{name: "set-not-root", root: "/", euid: 1000, args: []string{"set", "with-config", "COUNT=1"}, wantExit: 77, wantCode: "perm.root"},
		{name: "set-no-changes", args: []string{"set", "with-config"}, wantExit: 64, wantCode: "usage.args"},
		{name: "set-bad-assignment", args: []string{"set", "with-config", "COUNT"}, wantExit: 64, wantCode: "usage.assignment"},
		{name: "set-apply-no-apply", args: []string{"set", "--apply", "--no-apply", "with-config", "COUNT=1"}, wantExit: 64, wantCode: "usage.flag"},
		{name: "set-invalid", args: []string{"set", "with-config", "COUNT=x"}, wantExit: 65, wantCode: "config.invalid"},
		{name: "set-readonly", args: []string{"set", "with-config", "RO_KEY=x"}, wantExit: 65, wantCode: "config.readonly"},
		{name: "set-unknown", args: []string{"set", "with-config", "NOPE=x"}, wantExit: 65, wantCode: "config.unknown_key"},
		{name: "set-other", args: []string{"set", "noop", "A=1"}, wantExit: 66, wantCode: "not_found.extension"},
		{name: "config-no-sub", args: []string{"config"}, wantExit: 64, wantCode: "usage.args"},
		{name: "config-bad-sub", args: []string{"config", "edit", "with-config"}, wantExit: 64, wantCode: "usage.args"},
		{name: "export-no-shell", args: []string{"config", "export", "with-config"}, wantExit: 64, wantCode: "usage.format"},
		{name: "configure-not-root", root: "/", euid: 1000, args: []string{"configure", "configure-system"}, wantExit: 77, wantCode: "perm.root"},
		{name: "configure-from-unit-wait", args: []string{"configure", "--from-unit", "--wait", "with-config"}, wantExit: 64, wantCode: "usage.flag"},
		{name: "configure-from-unit-force", args: []string{"configure", "--from-unit", "--force", "with-config"}, wantExit: 64, wantCode: "usage.flag"},
		{name: "configure-again-force", args: []string{"configure", "--again", "--force", "with-config"}, wantExit: 64, wantCode: "usage.flag"},
		{name: "configure-all-no-force", args: []string{"configure", "--all"}, wantExit: 64, wantCode: "usage.flag"},
		{name: "configure-all-with-ext", args: []string{"configure", "--force", "--all", "with-config"}, wantExit: 64, wantCode: "usage.args"},
		{name: "system-no-sub", args: []string{"system"}, wantExit: 64, wantCode: "usage.args"},
		{name: "system-not-root", root: "/", euid: 1000, args: []string{"system", "init"}, wantExit: 77, wantCode: "perm.root"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := tc.root
			if root == "" {
				root = t.TempDir()
				seedRoot(t, root, "with-config")
			}
			out := runCLI(t, cliIn{Root: root, Euid: tc.euid}, append([]string{"--json"}, tc.args...)...)
			if out.Code != tc.wantExit {
				t.Errorf("exit = %d, want %d; stdout:\n%s", out.Code, tc.wantExit, out.Stdout)
			}
			if !strings.Contains(out.Stdout, `"code": "`+tc.wantCode+`"`) {
				t.Errorf("нет error.code %s:\n%s", tc.wantCode, out.Stdout)
			}
		})
	}
}

func TestConfigureFromUnitCLI(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	seedRoot(t, root, "configure-system")
	writeRootFile(t, root, "/run/cream/configure-system.reason", "apply\n", 0o644)
	out := runCLI(t, cliIn{Root: root}, "configure", "--from-unit", "configure-system")
	if out.Code != 0 {
		t.Fatalf("%+v", out)
	}
	log, err := os.ReadFile(filepath.Join(root, "var/lib/cream/ext/configure-system/configure.log"))
	if err != nil || !strings.Contains(string(log), "reason=apply ") {
		t.Errorf("configure.log: %q, %v", log, err)
	}
}

// TestConfigureMarkCLI — отметка configure через CLI (§10.7): загрузка после
// успешного configure пропускает хук, show показывает configured.
func TestConfigureMarkCLI(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	seedRoot(t, root, "configure-system")
	writeRootFile(t, root, "/etc/machine-id", "machine-of-the-test-host\n", 0o644)
	in := cliIn{Root: root}
	for i := range 2 {
		if out := runCLI(t, in, "configure", "--from-unit", "configure-system"); out.Code != 0 {
			t.Fatalf("загрузка %d: %+v", i+1, out)
		}
	}
	count, err := os.ReadFile(filepath.Join(root, "var/lib/cream/ext/configure-system/configure.count"))
	if err != nil || strings.TrimSpace(string(count)) != "1" {
		t.Errorf("хук на двух загрузках: %q, %v", count, err)
	}
	out := runCLI(t, in, "show", "--json", "configure-system")
	golden(t, "show-configured", normRoot(out.Stdout, root))
	out = runCLI(t, in, "configure", "--force", "configure-system")
	if out.Code != 0 || out.Stdout != "configure-system: configure (apply): hook\n" {
		t.Errorf("--force: %+v", out)
	}
	if strings.TrimSpace(readRoot(t, root, "var/lib/cream/ext/configure-system/configure.count")) != "2" {
		t.Error("--force не запустил хук")
	}
}

func readRoot(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestCommandFlagsDoNotShadowGlobals — Р14: флаги команд не повторяют
// глобальные (повтор — паника регистрации).
func TestCommandFlagsDoNotShadowGlobals(t *testing.T) {
	t.Parallel()
	for _, c := range (App{}).commands() {
		if c.Flags == nil {
			continue
		}
		t.Run(c.Name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("флаги %s: %v", c.Name, r)
				}
			}()
			fs := flag.NewFlagSet(c.Name, flag.ContinueOnError)
			g := Globals{}
			g.register(fs)
			c.Flags(fs)
		})
	}
}
