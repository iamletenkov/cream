package catalog

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/fsx"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/source"
)

// Фикстуры §14 — в корне репозитория (T4); «грязный» каталог — свой, в testdata пакета.
const (
	fixturesRoot = "../../testdata"
	dirtyCatalog = "testdata/dirty"
)

func problemCodes(r Report) []string {
	out := []string{}
	for _, p := range r.Errors {
		out = append(out, p.Code)
	}
	return out
}

func warningCodes(r Report) []string {
	out := []string{}
	for _, w := range r.Warnings {
		out = append(out, w.Code)
	}
	return out
}

func TestValidateCleanCatalogs(t *testing.T) {
	t.Parallel()
	cream := cream100(t)
	tests := []struct {
		dir          string
		name         string
		version      string
		exts         int
		wantWarnings []string
	}{
		// lock-restart-exclusive — негативный вариант §14 п.7: ошибкой он не
		// является, но правило §7.3 нарушает — ровно одно предупреждение.
		{dir: "catalog", name: "cream-fixtures", version: "1.0.0", exts: 20, wantWarnings: []string{"validate.unit_lock"}},
		{dir: "catalog-next", name: "cream-fixtures", version: "1.1.0", exts: 2, wantWarnings: []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.dir, func(t *testing.T) {
			t.Parallel()
			r, err := Validate(filepath.Join(fixturesRoot, tc.dir), cream)
			if err != nil {
				t.Fatalf("Validate: %v; ошибки %+v", err, r.Errors)
			}
			if !r.OK() || len(r.Errors) != 0 {
				t.Fatalf("ошибки %+v", r.Errors)
			}
			if r.Catalog != tc.name || r.CatalogVersion != tc.version {
				t.Errorf("каталог %q %q", r.Catalog, r.CatalogVersion)
			}
			if len(r.Extensions) != tc.exts {
				t.Errorf("расширений %d, ждали %d: %+v", len(r.Extensions), tc.exts, r.Extensions)
			}
			if got := warningCodes(r); !slices.Equal(got, tc.wantWarnings) {
				t.Errorf("предупреждения %v, ждали %v (%+v)", got, tc.wantWarnings, r.Warnings)
			}
		})
	}
}

func TestValidateExtensionsListed(t *testing.T) {
	t.Parallel()
	r, err := Validate(filepath.Join(fixturesRoot, "catalog-next"), cream100(t))
	if err != nil {
		t.Fatal(err)
	}
	want := []ExtensionInfo{
		{Name: "noop", Version: "1.0.0", Path: "extensions/noop"},
		{Name: "upgrade-fixture", Version: "2.0.0", Path: "extensions/upgrade-fixture"},
	}
	if !slices.Equal(r.Extensions, want) {
		t.Errorf("Extensions = %+v, ждали %+v", r.Extensions, want)
	}
}

func TestValidateInvalidFixtures(t *testing.T) {
	t.Parallel()
	cream := cream100(t)
	tests := []struct {
		dir  string
		exit int
		code string
	}{
		{dir: "dir-name-mismatch", exit: cerr.ExitData, code: "manifest.dir_name"},
		{dir: "duplicate-name-roots", exit: cerr.ExitData, code: "catalog.duplicate_name"},
		{dir: "hook-declared-missing", exit: cerr.ExitData, code: "manifest.hook_missing"},
		{dir: "hook-not-executable", exit: cerr.ExitData, code: "manifest.not_executable"},
		{dir: "hook-undeclared", exit: cerr.ExitData, code: "manifest.hook_undeclared"},
		{dir: "link-outside", exit: cerr.ExitData, code: "manifest.link_outside"},
		{dir: "not-elf-no-shebang", exit: cerr.ExitData, code: "manifest.bad_executable"},
		{dir: "reserved-bin", exit: cerr.ExitData, code: "manifest.reserved"},
		{dir: "too-new-cream"}, // Р22: каталог валиден, расширение — в предупреждениях (TestValidateCreamNewer)
		{dir: "unknown-field", exit: cerr.ExitData, code: "manifest.invalid"},
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
	if !slices.Equal(onDisk, inTable) {
		t.Fatalf("каталоги testdata/invalid %v, в таблице %v", onDisk, inTable)
	}
	for _, tc := range tests {
		if tc.code == "" {
			continue
		}
		t.Run(tc.dir, func(t *testing.T) {
			t.Parallel()
			r, err := Validate(filepath.Join(fixturesRoot, "invalid", tc.dir), cream)
			if got := problemCodes(r); !slices.Equal(got, []string{tc.code}) {
				t.Fatalf("ошибки %v, ждали ровно [%s]: %+v", got, tc.code, r.Errors)
			}
			if r.OK() {
				t.Error("OK() = true при ошибке")
			}
			if cerr.ExitCode(err) != tc.exit || cerr.CodeOf(err) != tc.code {
				t.Errorf("err = %v (%d %s), ждали %d %s", err, cerr.ExitCode(err), cerr.CodeOf(err), tc.exit, tc.code)
			}
			if p := r.Errors[0]; p.Path == "" || p.Message == "" {
				t.Errorf("у ошибки нет пути или текста: %+v", p)
			}
		})
	}
}

func TestValidateDirtyWarnings(t *testing.T) {
	t.Parallel()
	r, err := Validate(dirtyCatalog, cream100(t))
	if err != nil {
		t.Fatalf("Validate: %v (%+v)", err, r.Errors)
	}
	// Каждое «грязное» расширение даёт ровно своё предупреждение; порядок —
	// порядок расширений в каталоге (по имени каталога).
	want := []struct{ code, ext, mention string }{
		{"validate.apply_blocking", "dirty-apply", "dirty-apply-helper.service"},
		{"validate.lock_none_set", "dirty-set", "actions/write"},
		{"validate.unit_lock", "dirty-unit", "render"},
	}
	if len(r.Warnings) != len(want) {
		t.Fatalf("предупреждения %+v, ждали %d", r.Warnings, len(want))
	}
	for i, w := range want {
		got := r.Warnings[i]
		if got.Code != w.code || !strings.Contains(got.Message, w.ext) || !strings.Contains(got.Message, w.mention) {
			t.Errorf("предупреждение %d = %+v, ждали %s про %s (%s)", i, got, w.code, w.ext, w.mention)
		}
	}
	if !strings.Contains(r.Warnings[2].Message, "ExecStartPre") || !strings.Contains(r.Warnings[2].Message, "shared") {
		t.Errorf("unit_lock не называет строку и режим: %q", r.Warnings[2].Message)
	}
}

// TestValidateDirWarnings: ValidateDir даёт те же предупреждения для одного
// расширения вне каталога.
func TestValidateDirWarnings(t *testing.T) {
	t.Parallel()
	cream := cream100(t)
	tests := []struct {
		ext  string
		want []string
	}{
		{"dirty-apply", []string{"validate.apply_blocking"}},
		{"dirty-set", []string{"validate.lock_none_set"}},
		{"dirty-unit", []string{"validate.unit_lock"}},
	}
	for _, tc := range tests {
		t.Run(tc.ext, func(t *testing.T) {
			t.Parallel()
			r, err := ValidateDir(filepath.Join(dirtyCatalog, "extensions", tc.ext), cream)
			if err != nil {
				t.Fatal(err)
			}
			if got := warningCodes(r); !slices.Equal(got, tc.want) {
				t.Errorf("предупреждения %v, ждали %v", got, tc.want)
			}
			if len(r.Extensions) != 1 || r.Extensions[0].Name != tc.ext {
				t.Errorf("Extensions = %+v", r.Extensions)
			}
		})
	}
}

// writeTree создаёт дерево: путь → содержимое; режим 0755, если путь в exec.
func writeTree(t *testing.T, root string, files map[string]string, exec ...string) {
	t.Helper()
	for p, body := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o644)
		if slices.Contains(exec, p) {
			mode = 0o755
		}
		if err := os.WriteFile(full, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(full, mode); err != nil {
			t.Fatal(err)
		}
	}
}

const sh = "#!/bin/sh\nexit 0\n"

func manifestYAML(name, extra string) string {
	return "schema: cream/v1\nname: " + name + "\nversion: 1.0.0\nsummary: s\ncream: \">=1.0.0\"\n" +
		"platform: {family: deb, arch: [amd64]}\n" + extra
}

// TestValidateDirTree — правила §4.1 на сгенерированных деревьях.
func TestValidateDirTree(t *testing.T) {
	t.Parallel()
	cream := cream100(t)
	tests := []struct {
		name  string
		files map[string]string
		exec  []string
		setup func(t *testing.T, dir string)
		want  []string // коды ошибок; пусто — валидно
	}{
		{
			name:  "ok-minimal",
			files: map[string]string{"extension.yaml": manifestYAML("ok-minimal", "hooks: {install: {}}\n"), "hooks/install": sh},
			exec:  []string{"hooks/install"},
		},
		{
			name: "ok-library",
			files: map[string]string{
				"extension.yaml": manifestYAML("ok-library", "kind: library\n"),
				"lib.sh":         "# библиотека без бита исполнения\n",
			},
		},
		{
			name: "library-with-hook-file",
			files: map[string]string{
				"extension.yaml": manifestYAML("library-with-hook-file", "kind: library\n"),
				"hooks/install":  sh,
			},
			exec: []string{"hooks/install"},
			want: []string{"manifest.hook_undeclared"},
		},
		{
			name: "ok-elf-and-inner-links",
			files: map[string]string{
				"extension.yaml": manifestYAML("ok-elf-and-inner-links", "hooks: {install: {}, remove: {}}\n"),
				"hooks/install":  "\x7fELF\x02\x01\x01\x00\xff\xfe",
				"files/real":     sh,
				"files/sub/data": "x",
			},
			exec: []string{"hooks/install", "files/real"},
			setup: func(t *testing.T, dir string) {
				symlink(t, "../files/real", filepath.Join(dir, "hooks/remove"))
				symlink(t, "sub/data", filepath.Join(dir, "files/inner"))
				symlink(t, "../real", filepath.Join(dir, "files/sub/up"))
			},
		},
		{
			name: "absolute-link",
			files: map[string]string{
				"extension.yaml": manifestYAML("absolute-link", "hooks: {install: {}}\n"),
				"hooks/install":  sh,
			},
			exec:  []string{"hooks/install"},
			setup: func(t *testing.T, dir string) { symlink(t, "/etc/passwd", filepath.Join(dir, "files/abs")) },
			want:  []string{"manifest.link_outside"},
		},
		{
			name: "dotdot-after-name-link",
			files: map[string]string{
				"extension.yaml": manifestYAML("dotdot-after-name-link", "hooks: {install: {}}\n"),
				"hooks/install":  sh,
			},
			exec:  []string{"hooks/install"},
			setup: func(t *testing.T, dir string) { symlink(t, "hooks/../../x", filepath.Join(dir, "esc")) },
			want:  []string{"manifest.link_outside"},
		},
		{
			name: "action-missing-and-not-exec",
			files: map[string]string{
				"extension.yaml": manifestYAML("action-missing-and-not-exec",
					"hooks: {install: {}}\nactions: {a: {}, b: {}}\n"),
				"hooks/install": sh,
				"actions/b":     sh,
				"actions/extra": "helper\n",
			},
			exec: []string{"hooks/install"},
			want: []string{"manifest.action_missing", "manifest.not_executable", "manifest.not_executable"},
		},
		{
			name: "files-src-missing",
			files: map[string]string{
				"extension.yaml": manifestYAML("files-src-missing",
					"hooks: {install: {}}\nfiles: [{src: files/none, dst: /etc/x}]\n"),
				"hooks/install": sh,
			},
			exec: []string{"hooks/install"},
			want: []string{"manifest.file_missing"},
		},
		{
			name: "script-not-utf8",
			files: map[string]string{
				"extension.yaml": manifestYAML("script-not-utf8", "hooks: {install: {}}\n"),
				"hooks/install":  "#!/bin/sh\necho \xff\xfe\n",
			},
			exec: []string{"hooks/install"},
			want: []string{"manifest.bad_executable"},
		},
		{
			name: "exec-data-in-files",
			files: map[string]string{
				"extension.yaml": manifestYAML("exec-data-in-files", "hooks: {install: {}}\n"),
				"hooks/install":  sh,
				"files/blob":     "просто данные\n",
			},
			exec: []string{"hooks/install", "files/blob"},
			want: []string{"manifest.bad_executable"},
		},
		{
			name: "fifo",
			files: map[string]string{
				"extension.yaml": manifestYAML("fifo", "hooks: {install: {}}\n"),
				"hooks/install":  sh,
			},
			exec: []string{"hooks/install"},
			setup: func(t *testing.T, dir string) {
				if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0o644); err != nil {
					t.Skipf("mkfifo: %v", err)
				}
			},
			want: []string{"manifest.special_file"},
		},
		{
			name:  "no-manifest",
			files: map[string]string{"hooks/install": sh},
			exec:  []string{"hooks/install"},
			want:  []string{"not_found.manifest"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := filepath.Join(t.TempDir(), tc.name)
			writeTree(t, dir, tc.files, tc.exec...)
			if tc.setup != nil {
				tc.setup(t, dir)
			}
			r, err := ValidateDir(dir, cream)
			got := problemCodes(r)
			if len(tc.want) == 0 {
				if err != nil || len(got) != 0 {
					t.Fatalf("ValidateDir: %v, ошибки %+v", err, r.Errors)
				}
				return
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("ошибки %v, ждали %v: %+v", got, tc.want, r.Errors)
			}
			if cerr.CodeOf(err) != tc.want[0] {
				t.Errorf("err = %v, ждали код %s", err, tc.want[0])
			}
		})
	}
}

func symlink(t *testing.T, target, at string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, at); err != nil {
		t.Fatal(err)
	}
}

func TestValidateCatalogLevel(t *testing.T) {
	t.Parallel()
	cream := cream100(t)
	catYAML := func(roots string) string {
		return "schema: cream/v1\nname: c\nversion: 2.1.0\nroots: [" + roots + "]\n"
	}
	ext := func(name string) map[string]string {
		return map[string]string{
			name + "/extension.yaml": manifestYAML(filepath.Base(name), "hooks: {install: {}}\n"),
			name + "/hooks/install":  sh,
		}
	}
	tests := []struct {
		name  string
		files map[string]string
		exec  []string
		setup func(t *testing.T, dir string)
		want  []string
	}{
		{
			name:  "no-catalog-file",
			files: ext("extensions/a"),
			exec:  []string{"extensions/a/hooks/install"},
			want:  []string{"not_found.catalog"},
		},
		{
			name: "root-missing",
			files: mergeMaps(map[string]string{"cream-catalog.yaml": catYAML("extensions, other")},
				ext("extensions/a")),
			exec: []string{"extensions/a/hooks/install"},
			want: []string{"catalog.invalid"},
		},
		{
			name:  "root-is-link",
			files: mergeMaps(map[string]string{"cream-catalog.yaml": catYAML("extensions")}, ext("real/a")),
			exec:  []string{"real/a/hooks/install"},
			setup: func(t *testing.T, dir string) { symlink(t, "real", filepath.Join(dir, "extensions")) },
			want:  []string{"catalog.invalid"},
		},
		{
			name: "entry-is-link-hidden-and-files-skipped",
			files: mergeMaps(map[string]string{
				"cream-catalog.yaml":         catYAML("extensions"),
				"extensions/README.md":       "описание\n",
				"extensions/.hidden/x":       "скрытое не расширение\n",
				"extensions/.git/HEAD":       "ref\n",
				"elsewhere/b/extension.yaml": manifestYAML("b", "hooks: {install: {}}\n"),
			}, ext("extensions/a")),
			exec:  []string{"extensions/a/hooks/install"},
			setup: func(t *testing.T, dir string) { symlink(t, "../elsewhere/b", filepath.Join(dir, "extensions/b")) },
			want:  []string{"catalog.invalid"},
		},
		{
			name:  "ok",
			files: mergeMaps(map[string]string{"cream-catalog.yaml": catYAML("extensions")}, ext("extensions/a")),
			exec:  []string{"extensions/a/hooks/install"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, tc.files, tc.exec...)
			if tc.setup != nil {
				tc.setup(t, dir)
			}
			r, err := Validate(dir, cream)
			got := problemCodes(r)
			if len(tc.want) == 0 {
				if err != nil || len(got) != 0 || r.CatalogVersion != "2.1.0" {
					t.Fatalf("Validate: %v %+v %q", err, r.Errors, r.CatalogVersion)
				}
				return
			}
			if !slices.Equal(got, tc.want) || cerr.CodeOf(err) != tc.want[0] {
				t.Fatalf("ошибки %v (%v), ждали %v: %+v", got, err, tc.want, r.Errors)
			}
		})
	}
}

func mergeMaps(ms ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range ms {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// TestValidateErrDetails: ошибка Validate несёт все найденные ошибки в details.
func TestValidateErrDetails(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"cream-catalog.yaml":          "schema: cream/v1\nname: c\nversion: 1.0.0\nroots: [extensions]\n",
		"extensions/a/extension.yaml": manifestYAML("wrong", "hooks: {install: {}}\n"),
		"extensions/a/hooks/install":  "no shebang\n",
	}, "extensions/a/hooks/install")
	r, err := Validate(dir, cream100(t))
	if got := problemCodes(r); !slices.Equal(got, []string{"manifest.dir_name", "manifest.bad_executable"}) {
		t.Fatalf("ошибки %v", got)
	}
	d := cerr.DetailsOf(err)
	errs, ok := d["errors"].([]Problem)
	if !ok || len(errs) != 2 {
		t.Fatalf("details = %#v", d)
	}
	if r.Err() == nil || cerr.CodeOf(r.Err()) != "manifest.dir_name" {
		t.Errorf("Err() = %v", r.Err())
	}
}

func TestLoadExtension(t *testing.T) {
	t.Parallel()
	cream := cream100(t)
	dir := filepath.Join(fixturesRoot, "catalog", "extensions", "with-config")
	e, err := LoadExtension(dir, cream)
	if err != nil {
		t.Fatal(err)
	}
	want, err := fsx.TreeDigest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if e.Manifest == nil || e.Manifest.Name != "with-config" || e.Dir != dir || e.Digest != want {
		t.Errorf("Entry = %+v, отпечаток ждали %s", e, want)
	}
	if e.Source.Dir != "" || e.Source.Source.Name != "" {
		t.Errorf("--from-dir: источника нет, получили %+v", e.Source)
	}
	_, err = LoadExtension(filepath.Join(fixturesRoot, "invalid", "hook-undeclared", "extensions", "hook-undeclared"), cream)
	if cerr.CodeOf(err) != "manifest.hook_undeclared" {
		t.Errorf("дефект дерева: %v", err)
	}
	_, err = LoadExtension(filepath.Join(fixturesRoot, "invalid", "too-new-cream", "extensions", "too-new-cream"), cream)
	if cerr.ExitCode(err) != cerr.ExitNotApplicable {
		t.Errorf("cream старее манифеста: %v", err)
	}
}

// TestValidateDevBuild: dev-сборка cream удовлетворяет любому cream: с
// предупреждением platform.dev_build (Р18).
func TestValidateDevBuild(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"cream-catalog.yaml": "schema: cream/v1\nname: c\nversion: 1.0.0\nroots: [extensions]\n",
		"extensions/new/extension.yaml": strings.Replace(manifestYAML("new", "hooks: {install: {}}\n"),
			`">=1.0.0"`, `">=1.99.0"`, 1),
		"extensions/new/hooks/install": sh,
	}, "extensions/new/hooks/install")
	if r, err := Validate(dir, cream100(t)); err != nil || !slices.Equal(warningCodes(r), []string{"catalog.cream_newer"}) {
		t.Fatalf("релизная сборка: %v, предупреждения %v", err, warningCodes(r))
	}
	r, err := Validate(dir, manifest.CreamBuild{Dev: true})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got := warningCodes(r); !slices.Equal(got, []string{"platform.dev_build"}) {
		t.Errorf("предупреждения %v, ждали [platform.dev_build]", got)
	}
	var w cerr.Warnings
	dirs := []source.SourceDir{srcDir("x", dir, true)}
	if _, err := Lookup(dirs, Ref{Name: "new"}, manifest.CreamBuild{Dev: true}, &w); err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if l := w.List(); len(l) != 1 || l[0].Code != "platform.dev_build" || !strings.Contains(l[0].Message, "new") {
		t.Errorf("Lookup: предупреждения %+v", l)
	}
}

// TestValidateCreamNewer: расширение, которому нужен cream новее текущего,
// не делает каталог невалидным (Р22) — оно в Warnings (catalog.cream_newer)
// и в NotApplicable; LoadExtension на нём — по-прежнему 68.
func TestValidateCreamNewer(t *testing.T) {
	t.Parallel()
	root := filepath.Join(fixturesRoot, "invalid", "too-new-cream")
	r, err := Validate(root, cream100(t))
	if err != nil || !r.OK() {
		t.Fatalf("Validate: %v (%+v)", err, r.Errors)
	}
	if len(r.Warnings) != 1 || r.Warnings[0].Code != "catalog.cream_newer" {
		t.Fatalf("предупреждения %+v", r.Warnings)
	}
	d := r.Warnings[0].Details
	if d["ext"] != "too-new-cream" || d["required"] == nil || d["required"] == "" || d["cream"] != "1.0.0" {
		t.Errorf("details = %v", d)
	}
	if len(r.Extensions) != 0 {
		t.Errorf("неприменимое расширение в Extensions: %+v", r.Extensions)
	}
	if len(r.NotApplicable) != 1 {
		t.Fatalf("NotApplicable = %+v", r.NotApplicable)
	}
	if p := r.NotApplicable[0]; p.Ext != "too-new-cream" || p.Exit != cerr.ExitNotApplicable ||
		p.Code != "platform.cream_version" || p.Path != "extensions/too-new-cream/extension.yaml" {
		t.Errorf("NotApplicable[0] = %+v", p)
	}
	_, err = LoadExtension(filepath.Join(root, "extensions", "too-new-cream"), cream100(t))
	if cerr.ExitCode(err) != cerr.ExitNotApplicable || cerr.CodeOf(err) != "platform.cream_version" {
		t.Errorf("LoadExtension = %v, ждали 68 platform.cream_version", err)
	}
	// Отдельный каталог расширения (cream validate DIR) — строгий, как установка.
	if _, err := ValidateDir(filepath.Join(root, "extensions", "too-new-cream"), cream100(t)); cerr.ExitCode(err) != cerr.ExitNotApplicable {
		t.Errorf("ValidateDir = %v, ждали 68", err)
	}
}

// TestValidateCreamNewerKeepsOtherErrors: остальные ошибки каталога — как были.
func TestValidateCreamNewerKeepsOtherErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"cream-catalog.yaml": "schema: cream/v1\nname: c\nversion: 1.0.0\nroots: [extensions]\n",
		"extensions/new/extension.yaml": strings.Replace(manifestYAML("new", "hooks: {install: {}}\n"),
			`">=1.0.0"`, `">=1.99.0"`, 1),
		"extensions/new/hooks/install":  sh,
		"extensions/bad/extension.yaml": manifestYAML("bad", "hooks: {install: {}}\n"),
	}, "extensions/new/hooks/install")
	r, err := Validate(dir, cream100(t))
	if cerr.CodeOf(err) != "manifest.hook_missing" || !slices.Equal(problemCodes(r), []string{"manifest.hook_missing"}) {
		t.Fatalf("Validate = %v, ошибки %v", err, problemCodes(r))
	}
	if !slices.Equal(warningCodes(r), []string{"catalog.cream_newer"}) {
		t.Errorf("предупреждения %v", warningCodes(r))
	}
}
