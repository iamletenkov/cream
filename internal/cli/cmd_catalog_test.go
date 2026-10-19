package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iamletenkov/cream/internal/paths"
)

// catalogNorm заменяет в выводе корень машины и каталог testdata.
func catalogNorm(t *testing.T, out, root string) []byte {
	t.Helper()
	td := cliTestdataDir(t)
	out = strings.ReplaceAll(out, td, "<TESTDATA>")
	if root != "" {
		out = strings.ReplaceAll(out, root, "<ROOT>")
	}
	return []byte(out)
}

// TestValidateFixtures — `cream validate --catalog` по фикстурам §14: коды.
func TestValidateFixtures(t *testing.T) {
	t.Parallel()
	d := sourceTestDeps()
	tests := []struct {
		dir      string
		wantExit int
		wantCode string
	}{
		{"dir-name-mismatch", 65, "manifest.dir_name"},
		{"duplicate-name-roots", 65, "catalog.duplicate_name"},
		{"hook-declared-missing", 65, "manifest.hook_missing"},
		{"hook-not-executable", 65, "manifest.not_executable"},
		{"hook-undeclared", 65, "manifest.hook_undeclared"},
		{"link-outside", 65, "manifest.link_outside"},
		{"not-elf-no-shebang", 65, "manifest.bad_executable"},
		{"reserved-bin", 65, "manifest.reserved"},
		{"unknown-field", 65, "manifest.invalid"},
		// Р22: расширение под более новый cream не портит каталог.
		{"too-new-cream", 0, ""},
	}
	for _, tc := range tests {
		t.Run(tc.dir, func(t *testing.T) {
			t.Parallel()
			out := sourceRunCLI(t, d, cliIn{Root: t.TempDir()}, "validate", "--json", "--catalog",
				cliTestdataDir(t, "invalid", tc.dir))
			if out.Code != tc.wantExit || sourceErrCode(t, out) != tc.wantCode {
				t.Errorf("exit %d code %q, want %d %q\n%s", out.Code, sourceErrCode(t, out), tc.wantExit,
					tc.wantCode, out.Stdout)
			}
		})
	}
	// Тот же каталог расширения отдельно (DIR) — 68: ставить его этим cream нельзя.
	out := sourceRunCLI(t, d, cliIn{Root: t.TempDir()}, "validate", "--json",
		cliTestdataDir(t, "invalid", "too-new-cream", "extensions", "too-new-cream"))
	if out.Code != 68 || sourceErrCode(t, out) != "platform.cream_version" {
		t.Errorf("too-new-cream DIR: %+v", out)
	}
}

// TestGoldenValidate — формы result и ошибки `cream validate`.
func TestGoldenValidate(t *testing.T) {
	t.Parallel()
	d := sourceTestDeps()
	tests := []struct {
		name     string
		args     []string
		wantExit int
	}{
		{name: "validate-catalog", args: []string{"validate", "--json", "--catalog", "catalog-next"}},
		{name: "validate-dirs", args: []string{"validate", "--json", "catalog/extensions/noop",
			"catalog/extensions/with-config"}},
		{name: "validate-cream-newer", args: []string{"validate", "--json", "--catalog", "invalid/too-new-cream"}},
		{name: "validate-invalid", args: []string{"validate", "--json", "--catalog", "catalog-next",
			"invalid/hook-undeclared/extensions/hook-undeclared"}, wantExit: 65},
		{name: "validate-usage", args: []string{"validate", "--json"}, wantExit: 64},
		// cream 1.1.0: граф каталога — depends (ошибки), requires и conflicts (предупреждения).
		{name: "validate-graph", args: []string{"validate", "--json", "--catalog", "graph"}, wantExit: 65},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			args := make([]string, len(tc.args))
			for i, a := range tc.args {
				if i > 0 && !strings.HasPrefix(a, "-") {
					a = cliTestdataDir(t, strings.Split(a, "/")...)
				}
				args[i] = a
			}
			out := sourceRunCLI(t, d, cliIn{Root: t.TempDir()}, args...)
			if out.Code != tc.wantExit {
				t.Errorf("exit = %d, want %d; stdout:\n%s", out.Code, tc.wantExit, out.Stdout)
			}
			golden(t, tc.name, catalogNorm(t, out.Stdout, ""))
		})
	}
}

func TestValidateText(t *testing.T) {
	t.Parallel()
	d := sourceTestDeps()
	out := sourceRunCLI(t, d, cliIn{Root: t.TempDir()}, "validate", "--catalog", cliTestdataDir(t, "catalog-next"))
	if out.Code != 0 || !strings.Contains(out.Stdout, "upgrade-fixture") {
		t.Errorf("текст: %+v", out)
	}
	// Без --json все ошибки — в stderr, по строке на каждую.
	out = sourceRunCLI(t, d, cliIn{Root: t.TempDir()}, "validate", "--catalog",
		cliTestdataDir(t, "invalid", "duplicate-name-roots"), cliTestdataDir(t, "invalid", "hook-undeclared",
			"extensions", "hook-undeclared"))
	if out.Code != 65 || !strings.Contains(out.Stderr, "catalog.duplicate_name") ||
		!strings.Contains(out.Stderr, "manifest.hook_undeclared") {
		t.Errorf("ошибки: %+v", out)
	}
}

func TestCatalogPack(t *testing.T) {
	t.Parallel()
	d := sourceTestDeps()
	root := t.TempDir()
	env := map[string]string{"SOURCE_DATE_EPOCH": "1700000000"}
	cat := cliTestdataDir(t, "catalog")
	var sums []string
	for _, name := range []string{"a.tar.gz", "b.tar.gz"} {
		dst := filepath.Join(root, name)
		out := sourceRunCLI(t, d, cliIn{Root: root, Env: env}, "catalog", "pack", cat, "--out", dst, "--json")
		if out.Code != 0 {
			t.Fatalf("pack: %+v", out)
		}
		data, err := os.ReadFile(dst)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		hexSum := hex.EncodeToString(sum[:])
		var res struct {
			Result struct {
				SHA256         string `json:"sha256"`
				Size           int    `json:"size"`
				Catalog        string `json:"catalog"`
				CatalogVersion string `json:"catalog_version"`
			} `json:"result"`
		}
		if err := json.Unmarshal([]byte(out.Stdout), &res); err != nil {
			t.Fatal(err)
		}
		if res.Result.SHA256 != hexSum || res.Result.Size != len(data) || res.Result.Catalog != "cream-fixtures" ||
			res.Result.CatalogVersion != "1.0.0" {
			t.Errorf("result: %+v, sha256 файла %s, размер %d", res.Result, hexSum, len(data))
		}
		if st, err := os.Stat(dst); err != nil || st.Mode().Perm() != 0o644 {
			t.Errorf("режим архива: %v, %v", st, err)
		}
		sums = append(sums, hexSum)
	}
	if sums[0] != sums[1] {
		t.Errorf("pack не воспроизводим: %s != %s", sums[0], sums[1])
	}
	// Другая эпоха — другой архив (mtime записей).
	dst := filepath.Join(root, "c.tar.gz")
	out := sourceRunCLI(t, d, cliIn{Root: root, Env: map[string]string{"SOURCE_DATE_EPOCH": "1"}},
		"catalog", "pack", cat, "--out", dst, "--json")
	if out.Code != 0 || strings.Contains(out.Stdout, sums[0]) {
		t.Errorf("другая эпоха: %+v", out)
	}

	tests := []struct {
		name     string
		args     []string
		env      map[string]string
		wantExit int
		wantCode string
	}{
		{"битый каталог", []string{"pack", cliTestdataDir(t, "invalid", "hook-undeclared"), "--out",
			filepath.Join(root, "bad.tar.gz")}, nil, 65, "manifest.hook_undeclared"},
		{"без --out", []string{"pack", cat}, nil, 64, "usage.args"},
		{"без каталога", []string{"pack", "--out", filepath.Join(root, "x.tar.gz")}, nil, 64, "usage.args"},
		{"неверная эпоха", []string{"pack", cat, "--out", filepath.Join(root, "y.tar.gz")},
			map[string]string{"SOURCE_DATE_EPOCH": "вчера"}, 64, "usage.source_date_epoch"},
		{"без подкоманды", nil, nil, 64, "usage.args"},
		{"неизвестная подкоманда", []string{"frob"}, nil, 64, "usage.args"},
	}
	for _, tc := range tests {
		args := append([]string{"catalog", "--json"}, tc.args...)
		out := sourceRunCLI(t, d, cliIn{Root: root, Env: tc.env}, args...)
		if out.Code != tc.wantExit || sourceErrCode(t, out) != tc.wantCode {
			t.Errorf("%s: exit %d code %q, want %d %q\n%s", tc.name, out.Code, sourceErrCode(t, out),
				tc.wantExit, tc.wantCode, out.Stdout)
		}
	}
	for _, name := range []string{"bad.tar.gz", "y.tar.gz"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Errorf("%s создан при отказе: %v", name, err)
		}
	}
}

// resolveSynced — корень с синхронизированным источником fx (фикстуры §14).
func resolveSynced(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	cat := sourceCopyFixture(t, root, "catalog", "catalog")
	out := sourceRunCLI(t, sourceTestDeps(), cliIn{Root: root}, "source", "add", "fx", "--path", cat, "--sync")
	if out.Code != 0 {
		t.Fatalf("add --sync: %+v", out)
	}
	return root
}

// TestGoldenResolve — `cream resolve --json` (§12.4): порядок слоёв и lineage.
func TestGoldenResolve(t *testing.T) {
	t.Parallel()
	root := resolveSynced(t)
	d := sourceTestDeps()
	digests := map[string]string{sourceResolvedHex(t, paths.Layout{Root: root}, "fx"): "<FX_TREE>"}
	build := []string{"resolve", "--json", "--phase", "build", "--target-arch", "amd64", "--host-arch", "x86_64",
		"--family", "deb"}
	// База FROM слоя (cream 1.1.0): lib-fixture и поставщик fixture-runtime уже в образе.
	base := filepath.Join(t.TempDir(), "base.json")
	writeFileT(t, base, `[{"token": "fx/lib-fixture@1.0.0#0123456789ab", "params_digest": "sha256:00"},
 {"name": "apply-hook", "version": "1.0.0", "provides": ["fixture-runtime"]}]`)
	tests := []struct {
		name     string
		args     []string
		wantExit int
	}{
		{name: "resolve-depends-lib", args: append(build[:len(build):len(build)], "depends-lib", "fx/lib-fixture@1.0.0")},
		{name: "resolve-params", args: append(build[:len(build):len(build)], "with-config", "COUNT=7",
			"TOKEN=-", "MODE=slow")},
		{name: "resolve-deps-missing", args: append(build[:len(build):len(build)], "depends-lib"), wantExit: 67},
		{name: "resolve-installed", args: append(build[:len(build):len(build)], "--installed", base, "depends-lib",
			"requires-cap")},
		{name: "resolve-arch-barrier", args: []string{"resolve", "--json", "--phase", "build", "--target-arch",
			"arm64", "--host-arch", "amd64", "noop"}, wantExit: 68},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := sourceRunCLI(t, d, cliIn{Root: root}, tc.args...)
			if out.Code != tc.wantExit {
				t.Errorf("exit = %d, want %d; stdout:\n%s", out.Code, tc.wantExit, out.Stdout)
			}
			if strings.Contains(out.Stdout+out.Stderr, "s3cr3t") {
				t.Errorf("секрет в выводе:\n%s\n%s", out.Stdout, out.Stderr)
			}
			golden(t, tc.name, sourceNormDigests(normRoot(out.Stdout, root), digests))
		})
	}
}

func TestResolveRefusals(t *testing.T) {
	t.Parallel()
	root := resolveSynced(t)
	d := sourceTestDeps()
	b := []string{"--phase", "build", "--family", "deb"}
	tests := []struct {
		name     string
		args     []string
		wantExit int
		wantCode string
	}{
		{"без ссылок", b, 64, "usage.args"},
		{"параметр до ссылки", append(b[:3:3], "deb", "COUNT=1", "with-config"), 64, "usage.args"},
		{"неверная ссылка", append(b[:4:4], "Bad/Name"), 64, "usage.ref"},
		{"неизвестный ключ", append(b[:4:4], "with-config", "NOPE=1"), 65, "config.unknown_key"},
		{"неверное значение", append(b[:4:4], "with-config", "COUNT=many"), 65, "config.invalid"},
		{"ключ дважды", append(b[:4:4], "with-config", "COUNT=1", "COUNT=2"), 64, "usage.duplicate_key"},
		{"секрет литералом", append(b[:4:4], "with-config", "TOKEN=s3cr3t"), 64, "secret.in_argv"},
		{"пустой секрет", append(b[:4:4], "with-config", "TOKEN="), 64, "secret.in_argv"},
		{"ключ без значения", append(b[:4:4], "with-config", "COUNT"), 64, "usage.args"},
		{"голый ключ до ссылки", append(b[:4:4], "TOKEN", "with-config"), 64, "usage.args"},
		{"нет расширения", append(b[:4:4], "nope"), 66, "not_found.extension"},
		{"версия не та", append(b[:4:4], "noop@2.0.0"), 66, "version.mismatch"},
		{"неверная фаза", []string{"--phase", "boot", "noop"}, 64, "usage.phase"},
		{"--require-pinned зарезервирован", append(b[:4:4], "--require-pinned", "noop"), 64, "usage.reserved"},
		{"--installed в live", []string{"--installed", "/nope.json", "noop"}, 64, "usage.args"},
		{"--installed нет файла", append(b[:4:4], "--installed", "/nope.json", "noop"), 66, "not_found.installed"},
		{"семейство", []string{"--phase", "build", "--family", "rpm", "noop"}, 68, "platform.family"},
		{"live без os-release", []string{"noop"}, 68, "platform.family"},
	}
	for _, tc := range tests {
		args := append([]string{"resolve", "--json"}, tc.args...)
		out := sourceRunCLI(t, d, cliIn{Root: root}, args...)
		if out.Code != tc.wantExit || sourceErrCode(t, out) != tc.wantCode {
			t.Errorf("%s: exit %d code %q, want %d %q\n%s", tc.name, out.Code, sourceErrCode(t, out),
				tc.wantExit, tc.wantCode, out.Stdout)
		}
	}

	// Р28: секрет — только KEY=- или голое KEY; stdin не читается, оба дают один params_digest.
	secretForms := map[string]string{}
	for _, form := range []string{"TOKEN=-", "TOKEN"} {
		out := sourceRunCLI(t, d, cliIn{Root: root, Stdin: "never-read"}, "resolve", "--json", "--phase", "build",
			"--family", "deb", "with-config", form)
		if out.Code != 0 || strings.Contains(out.Stdout, "never-read") {
			t.Fatalf("%s: exit %d\n%s", form, out.Code, out.Stdout)
		}
		secretForms[form] = out.Stdout
	}
	if secretForms["TOKEN=-"] != secretForms["TOKEN"] {
		t.Errorf("TOKEN=- и TOKEN дают разный вывод:\n%s\n%s", secretForms["TOKEN=-"], secretForms["TOKEN"])
	}

	// Источник не синхронизирован — 69, а не 66.
	fresh := t.TempDir()
	cat := sourceCopyFixture(t, fresh, "catalog", "catalog")
	if out := sourceRunCLI(t, d, cliIn{Root: fresh}, "source", "add", "fx", "--path", cat); out.Code != 0 {
		t.Fatalf("add: %+v", out)
	}
	out := sourceRunCLI(t, d, cliIn{Root: fresh}, "resolve", "--json", "--phase", "build", "noop")
	if out.Code != 69 || sourceErrCode(t, out) != "source.not_synced" {
		t.Errorf("не синхронизирован: %+v", out)
	}
}

func writeFileT(t *testing.T, p, body string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Фаза live по умолчанию определяет хост сама (os-release под --root).
func TestResolveLiveDetects(t *testing.T) {
	t.Parallel()
	root := resolveSynced(t)
	writeRootFile(t, root, "/etc/os-release", "ID=debian\nVERSION_ID=13\n", 0o644)
	out := sourceRunCLI(t, sourceTestDeps(), cliIn{Root: root}, "resolve", "noop")
	if out.Code != 0 || !strings.Contains(out.Stdout, "fx/noop@1.0.0") {
		t.Errorf("live: %+v", out)
	}
	// Установленное участвует в графе live: depends-lib находит lib-fixture в реестре.
	seedRoot(t, root, "lib-fixture")
	out = sourceRunCLI(t, sourceTestDeps(), cliIn{Root: root}, "resolve", "--json", "depends-lib")
	if out.Code != 0 || !strings.Contains(out.Stdout, `"lib-fixture@1.0.0"`) {
		t.Errorf("live с реестром: %+v", out)
	}
}

// Настоящий набор команд содержит группы задачи 17.
func TestSourceCatalogCommandsRegistered(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"source", "list", "--json"}, {"validate", "--json"}, {"catalog", "--json"},
		{"resolve", "--json"}} {
		out := runCLI(t, cliIn{Root: t.TempDir()}, args...)
		if strings.Contains(out.Stdout, "usage.unknown_command") {
			t.Errorf("%v: команды нет: %s", args, out.Stdout)
		}
	}
}
