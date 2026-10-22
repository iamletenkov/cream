package manifest

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
)

// Фикстуры §14 лежат в корне репозитория: testdata/{catalog,catalog-next,invalid}.
const fixturesRoot = "../../testdata"

// catalogExts — расширения каталога, найденные по roots его cream-catalog.yaml:
// имя каталога → путь.
func catalogExts(t *testing.T, root string) map[string]string {
	t.Helper()
	c, err := LoadCatalog(root)
	if err != nil {
		t.Fatalf("LoadCatalog(%s): %v", root, err)
	}
	out := map[string]string{}
	for _, r := range c.Roots {
		entries, err := os.ReadDir(filepath.Join(root, r))
		if err != nil {
			t.Fatalf("root %s: %v", r, err)
		}
		for _, e := range entries {
			if e.IsDir() {
				out[e.Name()] = filepath.Join(root, r, e.Name())
			}
		}
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// checkTree — минимальная сверка дерева фикстуры с манифестом, чтобы валидные
// фикстуры оставались валидными и для T10: имя каталога = name, объявленные хуки
// есть файлами с битом исполнения, лишних файлов в hooks/ нет, у действий есть
// исполняемые файлы.
func checkTree(t *testing.T, dir string, m *Manifest) {
	t.Helper()
	if filepath.Base(dir) != m.Name {
		t.Errorf("%s: каталог ≠ name %q", dir, m.Name)
	}
	var declared []string
	for h := range m.Hooks {
		declared = append(declared, string(h))
	}
	sort.Strings(declared)
	var present []string
	if entries, err := os.ReadDir(filepath.Join(dir, "hooks")); err == nil {
		for _, e := range entries {
			present = append(present, e.Name())
		}
	}
	if !slices.Equal(declared, present) {
		t.Errorf("%s: хуки в манифесте %v, файлы %v", dir, declared, present)
	}
	exec := func(p string) {
		st, err := os.Lstat(p)
		if err != nil {
			t.Errorf("%v", err)
			return
		}
		if !st.Mode().IsRegular() || st.Mode().Perm()&0o111 != 0o111 {
			t.Errorf("%s: не исполняемый файл (%v)", p, st.Mode())
		}
	}
	for _, h := range present {
		exec(filepath.Join(dir, "hooks", h))
	}
	for a := range m.Actions {
		exec(filepath.Join(dir, "actions", a))
	}
	for _, f := range m.Files {
		if _, err := os.Lstat(filepath.Join(dir, f.Src)); err != nil {
			t.Errorf("%s: files.src: %v", dir, err)
		}
	}
}

func TestFixturesValid(t *testing.T) {
	t.Parallel()
	cream := ParseCreamBuild("1.1.0") // фикстуры стадий configure требуют cream 1.1.0
	tests := []struct {
		catalog string
		want    []string
		version map[string]string
	}{
		{
			catalog: "catalog",
			want: []string{
				"action-bad-protocol", "action-json", "apply-hook", "configure-cloud-user", "configure-early",
				"configure-graphical", "configure-system", "conflict-cap",
				"conflict-name", "depends-lib", "failing-install", "files-mode", "install-scope",
				"lib-fixture", "live-confirm", "lock-restart", "lock-restart-exclusive", "noop",
				"not-applicable", "reboot-required", "requires-cap", "upgrade-fixture", "with-config",
			},
			version: map[string]string{"upgrade-fixture": "1.0.0", "noop": "1.0.0"},
		},
		{
			catalog: "catalog-next",
			want:    []string{"noop", "upgrade-fixture"},
			version: map[string]string{"upgrade-fixture": "2.0.0", "noop": "1.0.0"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.catalog, func(t *testing.T) {
			t.Parallel()
			exts := catalogExts(t, filepath.Join(fixturesRoot, tc.catalog))
			if got := sortedKeys(exts); !slices.Equal(got, tc.want) {
				t.Fatalf("расширения %v, ждали %v", got, tc.want)
			}
			for name, dir := range exts {
				m, err := Load(dir, cream, nil)
				if err != nil {
					t.Errorf("Load(%s): %v", name, err)
					continue
				}
				checkTree(t, dir, m)
				if v, ok := tc.version[name]; ok && m.Version.String() != v {
					t.Errorf("%s: версия %s, ждали %s", name, m.Version, v)
				}
			}
		})
	}
}

// TestFixturesSemantics закрепляет то, ради чего фикстура заведена: потребители
// (T10–T19) опираются на эти свойства.
func TestFixturesSemantics(t *testing.T) {
	t.Parallel()
	cream := creamV1(t)
	load := func(t *testing.T, name string) *Manifest {
		t.Helper()
		m, err := Load(filepath.Join(fixturesRoot, "catalog", "extensions", name), cream, nil)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	tests := []struct {
		name  string
		check func(t *testing.T, m *Manifest) bool
	}{
		{"lib-fixture", func(_ *testing.T, m *Manifest) bool { return m.Kind == KindLibrary && len(m.Hooks) == 0 }},
		{"depends-lib", func(_ *testing.T, m *Manifest) bool {
			return len(m.Depends) == 1 && m.Depends[0].Name == "lib-fixture" && m.Depends[0].Version != nil
		}},
		{"configure-system", func(_ *testing.T, m *Manifest) bool {
			h, ok := m.Hooks[HookConfigure]
			return ok && h.Stage == StageSystem
		}},
		{"apply-hook", func(_ *testing.T, m *Manifest) bool {
			_, ok := m.Hooks[HookApply]
			return ok && slices.Contains(m.Provides, "fixture-runtime")
		}},
		{"conflict-name", func(_ *testing.T, m *Manifest) bool { return slices.Equal(m.Conflicts.Extensions, []string{"noop"}) }},
		{"conflict-cap", func(_ *testing.T, m *Manifest) bool {
			return slices.Equal(m.Conflicts.Capabilities, []string{"fixture-runtime"}) && slices.Contains(m.Provides, "fixture-runtime")
		}},
		{"requires-cap", func(_ *testing.T, m *Manifest) bool { return slices.Equal(m.Requires, []string{"fixture-runtime"}) }},
		{"live-confirm", func(_ *testing.T, m *Manifest) bool { return m.Targets.Live == LiveConfirm && m.Targets.LiveNote != "" }},
		{"install-scope", func(_ *testing.T, m *Manifest) bool {
			return m.Config["BIN_VERSION"].Scope == "install" && m.Config["LEVEL"].Scope == "config"
		}},
		{"with-config", func(_ *testing.T, m *Manifest) bool {
			kinds := map[string]bool{}
			for _, s := range m.Config {
				kinds[string(s.Type.Kind)] = true
			}
			for _, k := range []string{"bool", "int", "port", "host", "path", "str", "enum", "re", "list"} {
				if !kinds[k] {
					return false
				}
			}
			return m.Config["TOKEN"].Secret == "file" && m.Config["RO_KEY"].Readonly && m.Config["LBL_*"].Pattern
		}},
		{"files-mode", func(_ *testing.T, m *Manifest) bool {
			modes := map[string]bool{}
			r11 := false
			for _, f := range m.Files {
				modes[f.Mode.String()] = true
				if f.Dst == "/etc/cream/noop/labels.d/files-mode" {
					r11 = true
				}
			}
			return r11 && len(m.Files) >= 4 && modes["-rwxr-xr-x"] && modes["-rw-------"] && modes["----------"]
		}},
		{"action-json", func(_ *testing.T, m *Manifest) bool {
			j := m.Actions["join"]
			return m.Actions["status"].Output == OutputJSON && j.Stdin == StdinSecret && j.Params["FIXTURE_SECRET"].Secret
		}},
		{"action-bad-protocol", func(_ *testing.T, m *Manifest) bool { return m.Actions["bad-json"].Output == OutputJSON }},
		{"lock-restart", func(_ *testing.T, m *Manifest) bool {
			return m.Actions["render"].Lock == LockNone && m.Actions["set-managed"].Lock == LockExclusive && len(m.Files) == 1
		}},
		{"lock-restart-exclusive", func(_ *testing.T, m *Manifest) bool {
			return m.Actions["render"].Lock == LockExclusive && m.Actions["set-managed"].Lock == LockExclusive
		}},
		{"upgrade-fixture", func(_ *testing.T, m *Manifest) bool {
			_, ok := m.Hooks[HookUpgrade]
			return ok
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if m := load(t, tc.name); !tc.check(t, m) {
				t.Errorf("фикстура %s не обладает нужным свойством: %+v", tc.name, m)
			}
		})
	}
}

// TestFixturesInvalid: у дефектов дерева манифест валиден (их ловит T10), у
// манифестных дефектов — ожидаемый код.
func TestFixturesInvalid(t *testing.T) {
	t.Parallel()
	cream := creamV1(t)
	tests := []struct {
		dir      string
		wantExit int // 0 — манифест валиден, дефект в дереве или каталоге
		wantCode string
	}{
		{dir: "dir-name-mismatch"},
		{dir: "hook-not-executable"},
		{dir: "hook-declared-missing"},
		{dir: "hook-undeclared"},
		{dir: "link-outside"},
		{dir: "not-elf-no-shebang"},
		{dir: "duplicate-name-roots"},
		{dir: "unknown-field", wantExit: cerr.ExitData, wantCode: "manifest.invalid"},
		{dir: "reserved-bin", wantExit: cerr.ExitData, wantCode: "manifest.reserved"},
		{dir: "stage-cream-too-low", wantExit: cerr.ExitData, wantCode: "manifest.invalid"},
		{dir: "every-boot-cream-too-low", wantExit: cerr.ExitData, wantCode: "manifest.invalid"},
		{dir: "scope-build-cream-too-low", wantExit: cerr.ExitData, wantCode: "manifest.invalid"},
		{dir: "too-new-cream", wantExit: cerr.ExitNotApplicable, wantCode: "platform.cream_version"},
	}
	entries, err := os.ReadDir(filepath.Join(fixturesRoot, "invalid"))
	if err != nil {
		t.Fatal(err)
	}
	var onDisk, inTable []string
	for _, e := range entries {
		onDisk = append(onDisk, e.Name())
	}
	for _, tc := range tests {
		inTable = append(inTable, tc.dir)
	}
	sort.Strings(inTable)
	if !slices.Equal(onDisk, inTable) {
		t.Fatalf("каталоги testdata/invalid %v, в таблице %v", onDisk, inTable)
	}
	for _, tc := range tests {
		t.Run(tc.dir, func(t *testing.T) {
			t.Parallel()
			exts := catalogExts(t, filepath.Join(fixturesRoot, "invalid", tc.dir))
			if len(exts) == 0 {
				t.Fatal("в каталоге нет расширений")
			}
			for _, dir := range exts {
				_, err := Load(dir, cream, nil)
				if tc.wantExit == 0 {
					if err != nil {
						t.Errorf("Load(%s): %v", dir, err)
					}
					continue
				}
				if cerr.ExitCode(err) != tc.wantExit || cerr.CodeOf(err) != tc.wantCode {
					t.Errorf("Load(%s) = %v (%d %s), ждали %d %s", dir, err, cerr.ExitCode(err), cerr.CodeOf(err),
						tc.wantExit, tc.wantCode)
				}
			}
		})
	}
}

// TestFixturesContract: контрактная фикстура ADR-0004 (testdata/contract/cream-1.2)
// валидна в cream 1.2.0, а cream 1.1.x отказывает ей по cream: — 68
// platform.cream_version, а не «неизвестное поле». Тот же отказ настоящего
// выпуска 1.1.4 проверяет make contract-old.
func TestFixturesContract(t *testing.T) {
	t.Parallel()
	exts := catalogExts(t, filepath.Join(fixturesRoot, "contract", "cream-1.2"))
	if got := sortedKeys(exts); !slices.Equal(got, []string{"every-boot", "scope-build"}) {
		t.Fatalf("расширения %v", got)
	}
	for name, dir := range exts {
		m, err := Load(dir, ParseCreamBuild("1.2.0"), nil)
		if err != nil {
			t.Errorf("Load(%s) на 1.2.0: %v", name, err)
			continue
		}
		checkTree(t, dir, m)
		_, err = Load(dir, ParseCreamBuild("1.1.4"), nil)
		if cerr.ExitCode(err) != cerr.ExitNotApplicable || cerr.CodeOf(err) != "platform.cream_version" {
			t.Errorf("Load(%s) на 1.1.4 = %v, ждали 68 platform.cream_version", name, err)
		}
	}
}

func TestLoadErrors(t *testing.T) {
	t.Parallel()
	cream := creamV1(t)
	dir := t.TempDir()
	_, err := Load(dir, cream, nil)
	if cerr.ExitCode(err) != cerr.ExitNotFound || cerr.CodeOf(err) != "not_found.manifest" {
		t.Errorf("нет extension.yaml: %v (%d %s)", err, cerr.ExitCode(err), cerr.CodeOf(err))
	}
	big := make([]byte, maxManifestSize+1)
	for i := range big {
		big[i] = '#'
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), big, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Load(dir, cream, nil)
	if cerr.ExitCode(err) != cerr.ExitData {
		t.Errorf("большой манифест: %v", err)
	}
	if _, err := LoadCatalog(dir); cerr.ExitCode(err) != cerr.ExitNotFound || cerr.CodeOf(err) != "not_found.catalog" {
		t.Errorf("нет cream-catalog.yaml: %v", err)
	}
}

// TestFixturesContract13: контрактная фикстура ADR-0005 (testdata/contract/cream-1.3)
// валидна в cream 1.3.0, а cream 1.2.0 отказывает ей по cream: — 68
// platform.cream_version, а не по platform.family.
func TestFixturesContract13(t *testing.T) {
	t.Parallel()
	exts := catalogExts(t, filepath.Join(fixturesRoot, "contract", "cream-1.3"))
	if got := sortedKeys(exts); !slices.Equal(got, []string{"openwrt-family"}) {
		t.Fatalf("расширения %v", got)
	}
	for name, dir := range exts {
		m, err := Load(dir, ParseCreamBuild("1.3.0"), nil)
		if err != nil {
			t.Errorf("Load(%s) на 1.3.0: %v", name, err)
			continue
		}
		checkTree(t, dir, m)
		_, err = Load(dir, ParseCreamBuild("1.2.0"), nil)
		if cerr.ExitCode(err) != cerr.ExitNotApplicable || cerr.CodeOf(err) != "platform.cream_version" {
			t.Errorf("Load(%s) на 1.2.0 = %v, ждали 68 platform.cream_version", name, err)
		}
	}
}
