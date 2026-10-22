package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iamletenkov/cream/internal/source"
)

// checkRunCLI — как runCLI, но с командой check на deps.
func checkRunCLI(t *testing.T, d catalogDeps, args ...string) cliOut {
	t.Helper()
	var stdout, stderr bytes.Buffer
	app := App{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr,
		Env: func(string) string { return "" }, Euid: func() int { return 1000 },
		Now: func() time.Time { return cliNow }}
	code := run(args, app, checkCommandsWith(d))
	return cliOut{Code: code, Stdout: stdout.String(), Stderr: stderr.String()}
}

// checkArchive упаковывает фикстуру testdata/check/<name> в tar.gz под dir.
func checkArchive(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name+".tar.gz")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Pack(cliTestdataDir(t, "check", name), f, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestGoldenCheck — `cream check --json` (спека 2026-10-21 §7.3, §10.3).
func TestGoldenCheck(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	core := "core=" + checkArchive(t, dir, "core")
	local := "local=" + cliTestdataDir(t, "check", "local")
	// Метка cream.extensions базы (§8.2) как файл --installed.
	base := filepath.Join(dir, "base.json")
	writeFileT(t, base, `[{"name":"base-lib","version":"1.0.0","source":"core","digest":"sha256:00","provides":["base"]},
 {"name":"cap-provider","version":"1.0.0","source":"core","digest":"sha256:01","provides":["cap-x"]}]`)
	empty := filepath.Join(dir, "empty.json")
	writeFileT(t, empty, "[]")
	sum := strings.Repeat("ab", 32)
	host := []string{"check", "--json", "--catalog", core, "--catalog", local, "--os", "debian:12", "--arch", "amd64"}
	with := func(args ...string) []string {
		return append(append(host[:len(host):len(host)], "--installed", empty), args...)
	}
	tests := []struct {
		name     string
		args     []string
		wantExit int
	}{
		{name: "check-state", args: append(host[:len(host):len(host)], "--installed", base,
			"--step", "install app MODE=slow TOKEN=-",
			"--step", "install needs-cap",
			"--step", "upgrade base-lib",
			"--step", "install --with-deps top",
			"--step", "remove top",
			"--step", "install with-commands",
			"--step", "install build-none",
			"--step", "source add ext --archive https://example.org/ext.tar.gz --sha256 "+sum,
			"--step", "install --source ext ghost",
			"--step", "install needs-ghost")},
		{name: "check-not-found", args: with("--step", "install nope", "--step", "install app@1.0.0"), wantExit: 66},
		{name: "check-graph", args: with("--step", "install --with-deps local/local-app", "--step", "install app",
			"--step", "install --with-deps app", "--step", "install conflict-app",
			"--step", "install --with-deps needs-cap"), wantExit: 67},
		{name: "check-params", args: with("--step", "install --with-deps app TOKEN=s3cr3t",
			"--step", "install --with-deps app MODE=bogus", "--step", "install --with-deps app TOKEN=-"), wantExit: 64},
		{name: "check-platform", args: []string{"check", "--json", "--catalog", core, "--os", "ubuntu:24.04",
			"--arch", "amd64", "--step", "install arm-only", "--step", "install debian-only",
			"--step", "install base-lib", "--step", "install app"}, wantExit: 68},
		{name: "check-usage", args: []string{"check", "--json", "--step", "install app"}, wantExit: 64},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := checkRunCLI(t, sourceTestDeps(), tc.args...)
			if out.Code != tc.wantExit {
				t.Errorf("exit = %d, want %d; stdout:\n%s\nstderr:\n%s", out.Code, tc.wantExit, out.Stdout, out.Stderr)
			}
			if strings.Contains(strings.ReplaceAll(out.Stdout, `"command": "install --with-deps app TOKEN=s3cr3t"`, ""),
				"s3cr3t") {
				t.Errorf("секрет в ответе вне command:\n%s", out.Stdout)
			}
			golden(t, tc.name, normRoot(out.Stdout, dir))
		})
	}
}

// cream check ничего не пишет: корень машины пуст после прогона, root не нужен.
func TestCheckWritesNothing(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := t.TempDir()
	out := checkRunCLI(t, sourceTestDeps(), "--root", root, "check", "--catalog", "core="+checkArchive(t, dir, "core"),
		"--step", "install --with-deps app", "--step", "remove app")
	if out.Code != 0 {
		t.Fatalf("exit %d: %s %s", out.Code, out.Stdout, out.Stderr)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Errorf("корень после check: %v, %v", entries, err)
	}
	entries, err = os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Errorf("рядом с архивом появились файлы: %v, %v", entries, err)
	}
}

func TestCheckText(t *testing.T) {
	t.Parallel()
	core := "core=" + cliTestdataDir(t, "check", "core")
	out := checkRunCLI(t, sourceTestDeps(), "check", "--catalog", core, "--step", "install --with-deps app",
		"--step", "install with-commands")
	if out.Code != 0 {
		t.Fatalf("exit %d: %s", out.Code, out.Stderr)
	}
	for _, want := range []string{"1\tok\tinstall --with-deps app", "base-lib 1.2.0", "app 1.1.0",
		"2\tok\tinstall with-commands", "check.commands"} {
		if !strings.Contains(out.Stdout, want) {
			t.Errorf("нет %q в выводе:\n%s", want, out.Stdout)
		}
	}
	empty := filepath.Join(t.TempDir(), "empty.json")
	writeFileT(t, empty, "[]")
	out = checkRunCLI(t, sourceTestDeps(), "check", "--catalog", core, "--installed", empty, "--step", "install nope",
		"--step", "install arm-only", "--arch", "amd64")
	if out.Code != 66 {
		t.Fatalf("exit %d", out.Code)
	}
	for _, want := range []string{"1\terror\tinstall nope", "not_found.extension", "2\terror\tinstall arm-only",
		"platform.arch", "и ещё ошибок: 1"} {
		if !strings.Contains(out.Stderr, want) {
			t.Errorf("нет %q в stderr:\n%s", want, out.Stderr)
		}
	}
}

func TestCheckFlags(t *testing.T) {
	t.Parallel()
	core := "core=" + cliTestdataDir(t, "check", "core")
	tests := []struct {
		name string
		args []string
		code string
	}{
		{"no-catalog", []string{"check", "--step", "install app"}, "usage.args"},
		{"no-step", []string{"check", "--catalog", core}, "usage.args"},
		{"catalog-form", []string{"check", "--catalog", "core", "--step", "install app"}, "usage.flag"},
		{"catalog-name", []string{"check", "--catalog", "Core=/x"}, "usage.flag"},
		{"catalog-twice", []string{"check", "--catalog", core, "--catalog", core}, "usage.flag"},
		{"positional", []string{"check", "--catalog", core, "install"}, "usage.args"},
		{"os", []string{"check", "--catalog", core, "--os", "debian"}, "usage.flag"},
		{"arch", []string{"check", "--catalog", core, "--arch", "mips"}, "usage.flag"},
		{"catalog-missing", []string{"check", "--catalog", "core=/nonexistent/cat"}, "source.path"},
		{"installed-missing", []string{"check", "--catalog", core, "--installed", "/nonexistent.json"},
			"not_found.installed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			args := slices.Concat(tc.args, []string{"--json"})
			if tc.name != "no-step" && !slices.Contains(args, "--step") {
				args = append(args, "--step", "install app")
			}
			out := checkRunCLI(t, sourceTestDeps(), args...)
			if out.Code == 0 || !strings.Contains(out.Stdout, `"code": "`+tc.code+`"`) {
				t.Errorf("exit %d, ждали %s:\n%s", out.Code, tc.code, out.Stdout)
			}
		})
	}
}

// check больше не зарезервирована (cream 1.2.0).
func TestCheckNotReserved(t *testing.T) {
	t.Parallel()
	if isReservedCommand("check") {
		t.Error("check всё ещё в reservedCommands")
	}
	if _, ok := findCommand(App{}.commands(), "check"); !ok {
		t.Error("check нет в таблице команд")
	}
}
