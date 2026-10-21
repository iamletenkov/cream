package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iamletenkov/cream/internal/catalog"
	"github.com/iamletenkov/cream/internal/fsx"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/paths"
	"github.com/iamletenkov/cream/internal/resolve"
	"github.com/iamletenkov/cream/internal/state"
)

// buildCLISecret — значение секрета в CLI-тестах фазы build.
const buildCLISecret = "cli-build-secret-value"

// buildCLIIncoming раскладывает под root приёмник incoming/<id> из фикстуры
// ext (дерево, params.env 0600 с телом params, record.json с отпечатками) и
// возвращает аргументы `cream install --phase build …`. digestParams — от
// каких параметров посчитать params_digest в record.json (nil — от params).
func buildCLIIncoming(t *testing.T, root, id, ext, params string, digestParams map[string]string) []string {
	t.Helper()
	l := paths.Layout{Root: root}
	inc := l.Incoming(id)
	dir := filepath.Join(inc, ext)
	if err := os.MkdirAll(inc, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := fsx.CopyTree(fixtureDir(t, ext), dir); err != nil {
		t.Fatal(err)
	}
	en, err := catalog.LoadExtension(dir, manifest.ParseCreamBuild("1.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	vals := digestParams
	if vals == nil {
		vals = map[string]string{}
		for _, line := range strings.Split(strings.TrimSpace(params), "\n") {
			if k, v, ok := strings.Cut(line, "="); ok {
				vals[k] = v
			}
		}
	}
	secret := func(k string) bool { sp, ok := en.Manifest.Config.Lookup(k); return ok && sp.Secret != "" }
	layer := resolve.Layer{Name: ext, Version: en.Manifest.Version.String(), Source: "core",
		SourceResolved: "sha256:" + strings.Repeat("cd", 32), CatalogVersion: "4.0.0", ExtDigest: en.Digest,
		ParamsDigest: state.ParamsDigest(vals, secret), Deps: []string{}, Mode: resolve.ModeExec,
		Token: "core/" + ext + "@1.0.0#cdcdcdcdcdcd", Pinned: true, SecretParams: []string{}}
	data, err := json.Marshal(layer)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inc, "record.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	writeRootFile(t, root, "/var/lib/cream/incoming/"+id+"/params.env", params, 0o600)
	return []string{"install", "--phase", "build", "--from-dir", dir, "--record", filepath.Join(inc, "record.json"),
		"--params-file", filepath.Join(inc, "params.env"), "--remove-params-file"}
}

// buildCLIRoot — корень гостя «Debian 12» без источников.
func buildCLIRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeRootFile(t, root, "/etc/os-release", "ID=debian\nVERSION_ID=12\n", 0o644)
	return root
}

// TestGoldenInstallBuild — формы result фазы build (§11.4): запись реестра
// с phase: build, source из record.json; расхождение — 65 record.mismatch.
func TestGoldenInstallBuild(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		ext      string
		params   string
		digest   map[string]string
		wantExit int
	}{
		{name: "install-build", ext: "configure-system", params: "MESSAGE=build\n"},
		{name: "install-build-mismatch", ext: "configure-system", params: "MESSAGE=build\n",
			digest: map[string]string{"MESSAGE": "other"}, wantExit: 65},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := buildCLIRoot(t)
			args := buildCLIIncoming(t, root, "golden", tc.ext, tc.params, tc.digest)
			out := runCLI(t, cliIn{Root: root}, append([]string{"--json"}, args...)...)
			if out.Code != tc.wantExit {
				t.Errorf("exit = %d, want %d; stderr:\n%s", out.Code, tc.wantExit, out.Stderr)
			}
			golden(t, tc.name, normRoot(out.Stdout, root))
		})
	}
}

func TestInstallBuildCommand(t *testing.T) {
	t.Parallel()
	t.Run("текст и секрет", func(t *testing.T) {
		t.Parallel()
		root := buildCLIRoot(t)
		args := buildCLIIncoming(t, root, "s", "with-config", "TOKEN="+buildCLISecret+"\nCOUNT=7\n", nil)
		out := runCLI(t, cliIn{Root: root}, args...)
		if out.Code != 0 || out.Stdout != "with-config 1.0.0: installed (изменено)\n" {
			t.Fatalf("build: %+v", out)
		}
		if strings.Contains(out.Stdout+out.Stderr, buildCLISecret) {
			t.Errorf("секрет в выводе: %+v", out)
		}
		l := paths.Layout{Root: root}
		reg, err := os.ReadFile(l.InstalledFile("with-config"))
		if err != nil || strings.Contains(string(reg), buildCLISecret) || !strings.Contains(string(reg), `"phase": "build"`) {
			t.Errorf("реестр: %s %v", reg, err)
		}
		if _, err := os.Lstat(l.Incoming("s")); !os.IsNotExist(err) {
			t.Errorf("incoming не удалён: %v", err)
		}
	})
	root := buildCLIRoot(t)
	args := buildCLIIncoming(t, root, "u", "noop", "", nil)
	tests := []struct {
		name string
		args []string
	}{
		{name: "--from-dir без --phase build", args: []string{"install", "--from-dir", args[4], "--record", args[6]}},
		{name: "--phase build со ссылкой", args: append(append([]string{}, args...), "noop")},
		{name: "--phase build с KEY=VALUE", args: append(append([]string{}, args...), "X=1")},
		{name: "--phase build с --source", args: append([]string{"install", "--source", "core"}, args[1:]...)},
		{name: "неизвестная фаза", args: []string{"install", "--phase", "stage", "noop"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if out := runCLI(t, cliIn{Root: root}, tc.args...); out.Code != 64 {
				t.Errorf("%v: %+v", tc.args, out)
			}
		})
	}
}

// TestInstallBuildFromSourceCommand — фаза build без --from-dir (спека
// 2026-10-21 §10.2): install, upgrade и remove берут дерево из источника;
// отказ — одна строка stderr с error.code без --verbose (§17 р. 4).
func TestInstallBuildFromSourceCommand(t *testing.T) {
	t.Parallel()
	build := map[string]string{"CREAM_PHASE": "build"}
	t.Run("install, повтор, upgrade и remove", func(t *testing.T) {
		t.Parallel()
		root := installCLIRoot(t)
		in := cliIn{Root: root, Env: build}
		if out := runCLI(t, in, "install", "upgrade-fixture"); out.Code != 0 ||
			out.Stdout != "upgrade-fixture 1.0.0: installed (изменено)\n" {
			t.Fatalf("install: %+v", out)
		}
		l := paths.Layout{Root: root}
		reg, err := os.ReadFile(l.InstalledFile("upgrade-fixture"))
		if err != nil || !strings.Contains(string(reg), `"phase": "build"`) {
			t.Errorf("реестр: %s %v", reg, err)
		}
		if out := runCLI(t, in, "install", "upgrade-fixture"); out.Code != 0 ||
			out.Stdout != "upgrade-fixture 1.0.0: installed (без изменений)\n" {
			t.Errorf("повтор: %+v", out)
		}
		dir := installCLICatalog(t, root, "next", "1.1.0", map[string]string{
			"upgrade-fixture": cliTestdataDir(t, "catalog-next", "extensions", "upgrade-fixture")})
		installCLISources(t, root, dir)
		if out := runCLI(t, in, "upgrade", "-y", "upgrade-fixture"); out.Code != 0 || !strings.Contains(out.Stdout, "2.0.0") {
			t.Errorf("upgrade: %+v", out)
		}
		if out := runCLI(t, in, "remove", "upgrade-fixture"); out.Code != 0 {
			t.Errorf("remove: %+v", out)
		}
		hist, err := os.ReadFile(l.History())
		if err != nil || strings.Count(string(hist), `"phase":"build"`) != 3 {
			t.Errorf("history: %s %v", hist, err)
		}
	})
	t.Run("явный --phase build без --from-dir", func(t *testing.T) {
		t.Parallel()
		root := installCLIRoot(t)
		out := runCLI(t, cliIn{Root: root}, "install", "--phase", "build", "noop")
		if out.Code != 0 || !strings.Contains(out.Stderr, "cream: фаза build") {
			t.Fatalf("install --phase build: %+v", out)
		}
		reg, err := os.ReadFile(paths.Layout{Root: root}.InstalledFile("noop"))
		if err != nil || !strings.Contains(string(reg), `"phase": "build"`) {
			t.Errorf("реестр: %s %v", reg, err)
		}
	})
	t.Run("--with-deps", func(t *testing.T) {
		t.Parallel()
		root := installCLIRoot(t)
		in := cliIn{Root: root, Env: build}
		if out := runCLI(t, in, "install", "depends-lib"); out.Code != 67 {
			t.Errorf("без --with-deps: %+v", out)
		}
		out := runCLI(t, in, "install", "--with-deps", "depends-lib")
		if out.Code != 0 || out.Stdout != "lib-fixture 1.0.0: installed (изменено)\ndepends-lib 1.0.0: installed (изменено)\n" {
			t.Errorf("--with-deps: %+v", out)
		}
	})
	t.Run("отказ — одна строка с error.code", func(t *testing.T) {
		t.Parallel()
		root := installCLIRoot(t)
		out := runCLI(t, cliIn{Root: root, Env: build}, "install", "noop@9.9.9")
		if out.Code != 66 {
			t.Fatalf("exit: %+v", out)
		}
		var fail []string
		for _, line := range strings.Split(strings.TrimRight(out.Stderr, "\n"), "\n") {
			if strings.HasPrefix(line, "cream: ") && !strings.HasPrefix(line, "cream: предупреждение") &&
				!strings.HasPrefix(line, "cream: фаза") {
				fail = append(fail, line)
			}
		}
		if len(fail) != 1 || !strings.HasSuffix(fail[0], "(version.mismatch)") || !strings.Contains(fail[0], "9.9.9") {
			t.Errorf("строка отказа: %q", out.Stderr)
		}
	})
}
