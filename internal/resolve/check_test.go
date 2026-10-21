package resolve

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/semver"
	"github.com/iamletenkov/cream/internal/source"
)

// checkFixtures — каталоги-фикстуры cream check (testdata/check в корне).
const checkFixtures = "../../testdata/check"

var checkCream = manifest.ParseCreamBuild("1.2.0")

// checkCatalogs открывает фикстуры core и local каталогами-директориями.
func checkCatalogs(t *testing.T) []*CheckCatalog {
	t.Helper()
	var out []*CheckCatalog
	for _, name := range []string{"core", "local"} {
		c, err := OpenCheckCatalog(name, filepath.Join(checkFixtures, name))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		out = append(out, c)
	}
	return out
}

// packCatalog пакует каталог в tar.gz во временный файл.
func packCatalog(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), filepath.Base(dir)+".tar.gz")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Pack(dir, f, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

// checkHost — хост проверки: debian 12, amd64.
func checkHost() Host {
	return Host{Phase: PhaseBuild, TargetArch: "amd64", Family: FamilyDeb, OSID: "debian", OSVersion: "12"}
}

func runCheck(t *testing.T, installed []Node, steps ...string) CheckResult {
	t.Helper()
	return Check(CheckRequest{Catalogs: checkCatalogs(t), Host: checkHost(), Installed: installed, Steps: steps,
		Cream: checkCream})
}

// stepCode — «ok», «skipped» или код ошибки шага.
func stepCode(s CheckStep) string {
	if s.Error != nil {
		return s.Error.Code
	}
	return s.Status
}

func stepLayers(s CheckStep) []string {
	out := []string{}
	for _, l := range s.Layers {
		out = append(out, l.Name+"@"+l.Version)
	}
	return out
}

func installedNames(r CheckResult) []string {
	out := []string{}
	for _, x := range r.Installed {
		out = append(out, x.Name+"@"+x.Version)
	}
	return out
}

func warnCodes(s CheckStep) []string {
	out := []string{}
	for _, w := range s.Warnings {
		out = append(out, w.Code)
	}
	return out
}

func TestCheckSteps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		steps []string
		want  []string // код каждого шага
		exit  []int    // код выхода шагов с ошибкой (0 — без ошибки)
	}{
		{"chain", []string{"install base-lib", "install app MODE=slow TOKEN=-"}, []string{"ok", "ok"}, nil},
		{"not-found", []string{"install nope"}, []string{"not_found.extension"}, []int{66}},
		{"version-mismatch", []string{"install app@1.0.0"}, []string{"version.mismatch"}, []int{66}},
		{"source-missing", []string{"install other/app"}, []string{"not_found.source"}, []int{66}},
		{"deps-missing", []string{"install app"}, []string{"deps.missing"}, []int{67}},
		{"with-deps", []string{"install --with-deps app"}, []string{"ok"}, nil},
		{"with-deps-transitive", []string{"install top --with-deps"}, []string{"ok"}, nil},
		{"with-deps-own-source", []string{"install --with-deps local/local-app"}, []string{"deps.missing"}, []int{67}},
		{"with-deps-no-requires", []string{"install --with-deps needs-cap"}, []string{"deps.requires"}, []int{67}},
		{"with-deps-version", []string{"install --with-deps wants-new-lib"}, []string{"deps.version"}, []int{67}},
		{"requires-closed", []string{"install cap-provider", "install needs-cap"}, []string{"ok", "ok"}, nil},
		{"conflict", []string{"install --with-deps app", "install conflict-app"}, []string{"ok", "conflict.extension"},
			[]int{0, 67}},
		{"param-invalid", []string{"install --with-deps app MODE=bogus"}, []string{"config.invalid"}, []int{65}},
		{"param-unknown", []string{"install --with-deps app NOPE=1"}, []string{"config.unknown_key"}, []int{65}},
		{"secret-in-argv", []string{"install --with-deps app TOKEN=s3cr3t"}, []string{"secret.in_argv"}, []int{64}},
		{"secret-by-name", []string{"install --with-deps app TOKEN=-"}, []string{"ok"}, nil},
		{"secret-bare", []string{"install --with-deps app TOKEN"}, []string{"usage.ref"}, []int{64}},
		{"stdin-twice", []string{"install --with-deps app TOKEN=- MODE=-"}, []string{"usage.stdin_twice"}, []int{64}},
		{"duplicate-key", []string{"install --with-deps app MODE=fast MODE=slow"}, []string{"usage.duplicate_key"},
			[]int{64}},
		{"no-ref", []string{"install MODE=slow"}, []string{"usage.args"}, []int{64}},
		{"source-flag-mismatch", []string{"install --source core local/app"}, []string{"usage.source"}, []int{64}},
		{"phase-live", []string{"install --phase live base-lib"}, []string{"usage.phase"}, []int{64}},
		{"params-file", []string{"install --with-deps app --params-file /run/secrets/app"}, []string{"ok"}, nil},
		{"params-several-refs", []string{"install base-lib app MODE=slow"}, []string{"usage.args"}, []int{64}},
		{"arch", []string{"install arm-only"}, []string{"platform.arch"}, []int{68}},
		{"os", []string{"install debian-only"}, []string{"ok"}, nil},
		{"remove", []string{"install --with-deps app", "remove app", "remove base-lib"},
			[]string{"ok", "ok", "ok"}, nil},
		{"remove-dependents", []string{"install --with-deps app", "remove base-lib"},
			[]string{"ok", "deps.dependents"}, []int{0, 67}},
		{"remove-force", []string{"install --with-deps app", "remove --force base-lib"}, []string{"ok", "ok"}, nil},
		{"remove-not-installed", []string{"remove app"}, []string{"not_found.extension"}, []int{66}},
		{"repeat-noop", []string{"install base-lib", "install base-lib"}, []string{"ok", "ok"}, nil},
		{"downgrade", []string{"install local/app", "install --with-deps core/app"}, []string{"ok", "version.downgrade"},
			[]int{0, 65}},
		{"bad-flag", []string{"install --frobnicate app"}, []string{"usage.flag"}, []int{64}},
		{"shell", []string{"install app; reboot"}, []string{"skipped"}, nil},
		{"secret-stdin", []string{"install --with-deps app TOKEN=- < /run/secrets/app-token"}, []string{"ok"}, nil},
		{"other-redirect", []string{"install --with-deps app TOKEN=- < /etc/shadow"}, []string{"skipped"}, nil},
		{"source-add-twice", []string{"source add e --archive https://x/e.tar.gz --sha256 " + strings.Repeat("a", 64),
			"source add e --archive https://x/e2.tar.gz --sha256 " + strings.Repeat("a", 64)},
			[]string{"ok", "source.conflict"}, []int{0, 65}},
		{"source-add-replace", []string{"source add e --archive https://x/e.tar.gz --sha256 " + strings.Repeat("a", 64),
			"source add e --archive https://x/e2.tar.gz --sha256 " + strings.Repeat("a", 64) + " --replace",
			"source add core --replace --archive https://x/c.tar.gz --sha256 " + strings.Repeat("b", 64),
			"install base-lib"},
			[]string{"ok", "ok", "ok", "skipped"}, nil},
		{"nonstandard-no-cascade", []string{"install base-lib; true", "install app"}, []string{"skipped", "ok"}, nil},
		{"unsupported", []string{"set app MODE=slow"}, []string{"skipped"}, nil},
		{"empty", []string{"   "}, []string{"usage.step"}, []int{64}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := runCheck(t, nil, tc.steps...)
			if len(r.Steps) != len(tc.steps) {
				t.Fatalf("шагов %d, ждали %d", len(r.Steps), len(tc.steps))
			}
			for i, s := range r.Steps {
				if got := stepCode(s); got != tc.want[i] {
					t.Errorf("шаг %d %q: %s (%+v), ждали %s", i+1, tc.steps[i], got, s.Error, tc.want[i])
				}
				wantExit := 0
				if i < len(tc.exit) {
					wantExit = tc.exit[i]
				}
				if s.Error != nil && s.Error.Exit != wantExit {
					t.Errorf("шаг %d: exit %d, ждали %d", i+1, s.Error.Exit, wantExit)
				}
				if s.Step != i+1 || s.Command != tc.steps[i] {
					t.Errorf("шаг %d: step=%d command=%q", i+1, s.Step, s.Command)
				}
			}
			// command — вход вызывающего как есть; остальное ответа секрет не повторяет.
			var out string
			for _, s := range r.Steps {
				out += fmt.Sprintf("%+v %+v %+v", s.Error, s.Warnings, s.Layers)
			}
			if strings.Contains(out, "s3cr3t") {
				t.Errorf("секрет в ответе: %s", out)
			}
		})
	}
}

func TestCheckOrderAndState(t *testing.T) {
	t.Parallel()
	r := runCheck(t, nil, "install top --with-deps", "install --with-deps app MODE=slow", "remove top")
	if got := stepLayers(r.Steps[0]); !slices.Equal(got, []string{"base-lib@1.2.0", "mid@1.0.0", "top@1.0.0"}) {
		t.Errorf("порядок шага 1: %v", got)
	}
	if got := stepLayers(r.Steps[1]); !slices.Equal(got, []string{"app@1.1.0"}) {
		t.Errorf("шаг 2 (base-lib уже есть): %v", got)
	}
	if got := r.Steps[2].Removed; !slices.Equal(got, []string{"top"}) {
		t.Errorf("removed: %v", got)
	}
	if got := installedNames(r); !slices.Equal(got, []string{"app@1.1.0", "base-lib@1.2.0", "mid@1.0.0"}) {
		t.Errorf("итог: %v", got)
	}
	for _, x := range r.Installed {
		if x.Source != "core" {
			t.Errorf("%s: source %q", x.Name, x.Source)
		}
		if x.Name == "base-lib" && !slices.Equal(x.Provides, []string{"base"}) {
			t.Errorf("base-lib provides %v", x.Provides)
		}
	}
	if r.Phase != PhaseBuild {
		t.Errorf("phase %q", r.Phase)
	}
}

func TestCheckPlatform(t *testing.T) {
	t.Parallel()
	cats := checkCatalogs(t)
	run := func(h Host, step string) CheckStep {
		return Check(CheckRequest{Catalogs: cats, Host: h, Steps: []string{step}, Cream: checkCream}).Steps[0]
	}
	ubuntu := checkHost()
	ubuntu.OSID, ubuntu.OSVersion = "ubuntu", "24.04"
	if s := run(ubuntu, "install debian-only"); stepCode(s) != "platform.os" || s.Error.Exit != 68 {
		t.Errorf("ubuntu: %+v", s)
	}
	if s := run(ubuntu, "install base-lib"); stepCode(s) != "ok" {
		t.Errorf("ubuntu — семейство deb: %+v", s)
	}
	old := checkHost()
	old.OSVersion = "11"
	if s := run(old, "install debian-only"); stepCode(s) != "platform.os" {
		t.Errorf("debian 11: %+v", s)
	}
	unknown := Host{Phase: PhaseBuild, TargetArch: "amd64"}
	if s := run(unknown, "install debian-only"); stepCode(s) != "ok" || !slices.Contains(warnCodes(s), "platform.os_unknown") {
		t.Errorf("ОС не задана — предупреждение: %+v", s)
	}
	s := run(checkHost(), "install with-commands")
	if stepCode(s) != "ok" || !slices.Contains(warnCodes(s), "check.commands") {
		t.Errorf("platform.commands — только предупреждение: %+v", s)
	}
	s = run(checkHost(), "install build-none")
	if stepCode(s) != "ok" || !slices.Contains(warnCodes(s), "install.build_none") {
		t.Errorf("targets.build: none — предупреждение: %+v", s)
	}
	if len(s.Layers) != 1 || s.Layers[0].Mode != ModeNone {
		t.Errorf("слой build-none: %+v", s.Layers)
	}
}

func TestCheckBuildNoneNotInstalled(t *testing.T) {
	t.Parallel()
	r := runCheck(t, nil, "install build-none")
	if got := installedNames(r); len(got) != 0 {
		t.Errorf("targets.build: none попало в установленное: %v", got)
	}
}

func TestCheckInstalledLabel(t *testing.T) {
	t.Parallel()
	// Значение метки cream.extensions базы (§8.2): лишние поля не мешают.
	p := writeInstalled(t, `[{"name":"base-lib","version":"1.0.0","source":"core","digest":"sha256:00",
	  "provides":["base"]},{"name":"app","version":"1.0.0","source":"core","digest":"sha256:01","provides":[]}]`)
	installed, err := ReadInstalled(p)
	if err != nil {
		t.Fatal(err)
	}
	r := runCheck(t, installed, "upgrade app", "install mid", "remove base-lib")
	if got := stepCode(r.Steps[0]); got != "ok" {
		t.Fatalf("upgrade: %+v", r.Steps[0])
	}
	if l := r.Steps[0].Layers; len(l) != 1 || l[0].Version != "1.1.0" || l[0].Op != "upgrade" {
		t.Errorf("upgrade: %+v", l)
	}
	if got := stepCode(r.Steps[1]); got != "ok" {
		t.Errorf("mid закрыт базой: %+v", r.Steps[1])
	}
	if got := stepCode(r.Steps[2]); got != "deps.dependents" {
		t.Errorf("remove base-lib: %+v", r.Steps[2])
	}
	if got := installedNames(r); !slices.Equal(got, []string{"app@1.1.0", "base-lib@1.0.0", "mid@1.0.0"}) {
		t.Errorf("итог: %v", got)
	}
}

func TestCheckUpgrade(t *testing.T) {
	t.Parallel()
	r := runCheck(t, nil, "install --with-deps app", "upgrade app", "upgrade --all", "upgrade nope")
	want := []string{"ok", "ok", "ok", "not_found.extension"}
	for i, s := range r.Steps {
		if stepCode(s) != want[i] {
			t.Errorf("шаг %d: %s %+v", i+1, stepCode(s), s.Error)
		}
	}
	if l := r.Steps[1].Layers; len(l) != 1 || l[0].Op != "noop" {
		t.Errorf("upgrade без новой версии: %+v", l)
	}
	installed := []Node{{Manifest: &manifest.Manifest{Name: "app", Version: mustParse(t, "1.0.0")}, Installed: true,
		Files: []string{}}}
	r = runCheck(t, installed, "upgrade app")
	if got := stepCode(r.Steps[0]); got != "source.missing" || r.Steps[0].Error.Exit != 69 {
		t.Errorf("установлено не из источника: %+v", r.Steps[0])
	}
}

func TestCheckExternalSource(t *testing.T) {
	t.Parallel()
	sum := strings.Repeat("a", 64)
	r := runCheck(t, nil,
		"source add ext --archive https://example.org/ext.tar.gz --sha256 "+sum,
		"install --source ext ghost",
		"install needs-ghost",
		"install ext/other",
		"install not-anywhere",
		"source add bad --archive https://example.org/bad.tar.gz",
		"source add dir --path /srv/catalog",
	)
	want := []string{"ok", "skipped", "ok", "skipped", "skipped", "usage.args", "skipped"}
	for i, s := range r.Steps {
		if stepCode(s) != want[i] {
			t.Errorf("шаг %d %q: %s %+v", i+1, s.Command, stepCode(s), s.Error)
		}
	}
	if !slices.Contains(warnCodes(r.Steps[1]), "check.external") {
		t.Errorf("шаг 2: нет заметки check.external: %+v", r.Steps[1].Warnings)
	}
	if !slices.Contains(warnCodes(r.Steps[2]), "check.maybe_installed") {
		t.Errorf("шаг 3: нет check.maybe_installed: %+v", r.Steps[2].Warnings)
	}
	if got := installedNames(r); !slices.Equal(got, []string{"needs-ghost@1.0.0"}) {
		t.Errorf("итог: %v", got)
	}
}

func TestCheckFailedStepDoesNotCascade(t *testing.T) {
	t.Parallel()
	r := runCheck(t, nil, "install ghost", "install needs-ghost")
	if stepCode(r.Steps[0]) != "not_found.extension" {
		t.Errorf("шаг 1: %+v", r.Steps[0])
	}
	if stepCode(r.Steps[1]) != "ok" || !slices.Contains(warnCodes(r.Steps[1]), "check.maybe_installed") {
		t.Errorf("шаг 2 не должен падать из-за шага 1: %+v", r.Steps[1])
	}
	exit, first := r.FirstError()
	if exit != 66 || first == nil || first.Code != "not_found.extension" {
		t.Errorf("FirstError: %d %+v", exit, first)
	}
}

func TestCheckArchiveCatalog(t *testing.T) {
	t.Parallel()
	arch, err := OpenCheckCatalog("core", packCatalog(t, filepath.Join(checkFixtures, "core")))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = arch.Close() }()
	if arch.Version() != "2.0.0" {
		t.Errorf("версия каталога %q", arch.Version())
	}
	r := Check(CheckRequest{Catalogs: []*CheckCatalog{arch}, Host: checkHost(), Cream: checkCream,
		Steps: []string{"install top --with-deps", "install arm-only"}})
	if got := stepLayers(r.Steps[0]); !slices.Equal(got, []string{"base-lib@1.2.0", "mid@1.0.0", "top@1.0.0"}) {
		t.Errorf("архив: %v (%+v)", got, r.Steps[0].Error)
	}
	if stepCode(r.Steps[1]) != "platform.arch" {
		t.Errorf("архив, arm-only: %+v", r.Steps[1])
	}
}

func TestOpenCheckCatalogRefusals(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	notCatalog := filepath.Join(dir, "empty")
	if err := os.Mkdir(notCatalog, 0o755); err != nil {
		t.Fatal(err)
	}
	junk := filepath.Join(dir, "junk.tar.gz")
	if err := os.WriteFile(junk, []byte("not gzip"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct{ name, path, code string }{
		{"missing", filepath.Join(dir, "nope"), "source.path"},
		{"no-catalog-yaml", notCatalog, "source.catalog"},
		{"junk", junk, "source.bad_archive"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := OpenCheckCatalog("x", tc.path)
			if err == nil {
				t.Fatal("принято")
			}
			if got := cerr.CodeOf(err); got != tc.code {
				t.Errorf("код %s, ждали %s: %v", got, tc.code, err)
			}
		})
	}
}

// TestCheckSpeed — порог спеки §7.3: < 1 с на каталог размера 5.2.0 и 10 шагов.
func TestCheckSpeed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeT := func(rel, body string, mode os.FileMode) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	writeT("cream-catalog.yaml", "schema: cream/v1\nname: big\nversion: 5.2.0\nroots: [extensions]\n", 0o644)
	var steps []string
	for i := range 80 {
		name := fmt.Sprintf("ext-%02d", i)
		dep := ""
		if i > 0 {
			dep = fmt.Sprintf("depends: [{name: ext-%02d}]\n", i-1)
		}
		writeT("extensions/"+name+"/extension.yaml", "schema: cream/v1\nname: "+name+
			"\nversion: 1.0.0\nsummary: x\ncream: \">=1.0.0\"\nplatform: {family: deb, arch: [amd64]}\n"+dep+
			"hooks:\n  install: {}\n", 0o644)
		writeT("extensions/"+name+"/hooks/install", "#!/bin/sh\n"+strings.Repeat("# filler\n", 2000), 0o755)
		if i%8 == 0 && len(steps) < 10 {
			steps = append(steps, "install --with-deps "+name)
		}
	}
	arch := packCatalog(t, dir)
	start := time.Now()
	c, err := OpenCheckCatalog("core", arch)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	r := Check(CheckRequest{Catalogs: []*CheckCatalog{c}, Host: checkHost(), Steps: steps, Cream: checkCream})
	took := time.Since(start)
	if exit, first := r.FirstError(); exit != 0 {
		t.Fatalf("ошибка: %+v", first)
	}
	if len(r.Installed) != 73 {
		t.Errorf("установлено %d", len(r.Installed))
	}
	if took > time.Second {
		t.Errorf("проверка заняла %v, порог 1 с", took)
	}
}

func mustParse(t *testing.T, s string) semver.Version {
	t.Helper()
	v, err := semver.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestSplitStep(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want []string
		ok   bool
	}{
		{`install app MODE=slow`, []string{"install", "app", "MODE=slow"}, true},
		{`install app 'GREETING=a b' "X=c d"`, []string{"install", "app", "GREETING=a b", "X=c d"}, true},
		{`install app X=a\ b`, []string{"install", "app", "X=a b"}, true},
		{`install app; reboot`, nil, false},
		{`install app && x`, nil, false},
		{`install app | x`, nil, false},
		{`install $APP`, nil, false},
		{"install `x`", nil, false},
		{`install app > /dev/null`, nil, false},
		{`install 'unterminated`, nil, false},
		{`install app '$literal'`, []string{"install", "app", "$literal"}, true},
	}
	for _, tc := range tests {
		got, err := splitStep(tc.in)
		if (err == nil) != tc.ok || (tc.ok && !slices.Equal(got, tc.want)) {
			t.Errorf("splitStep(%q) = %q, %v", tc.in, got, err)
		}
	}
}

// Без --installed база неизвестна (§7.3): незакрытое — предупреждения, не ошибки.
func TestCheckBaseUnknown(t *testing.T) {
	t.Parallel()
	r := Check(CheckRequest{Catalogs: checkCatalogs(t), Host: checkHost(), BaseUnknown: true, Cream: checkCream,
		Steps: []string{"install app", "install needs-cap", "upgrade teleport", "remove docker",
			"install other/x", "install nope"}})
	want := []string{"ok", "ok", "skipped", "skipped", "skipped", "skipped"}
	for i, s := range r.Steps {
		if stepCode(s) != want[i] {
			t.Errorf("шаг %d %q: %s %+v", i+1, s.Command, stepCode(s), s.Error)
		}
	}
	if !slices.Contains(warnCodes(r.Steps[0]), "check.maybe_installed") ||
		!slices.Contains(warnCodes(r.Steps[1]), "check.maybe_installed") {
		t.Errorf("нет check.maybe_installed: %+v %+v", r.Steps[0].Warnings, r.Steps[1].Warnings)
	}
	// Конфликты и платформа по-прежнему ошибки.
	r = Check(CheckRequest{Catalogs: checkCatalogs(t), Host: checkHost(), BaseUnknown: true, Cream: checkCream,
		Steps: []string{"install --with-deps app", "install conflict-app", "install arm-only"}})
	if stepCode(r.Steps[1]) != "conflict.extension" || stepCode(r.Steps[2]) != "platform.arch" {
		t.Errorf("ошибки потеряны: %+v", r.Steps)
	}
}

// Источник расширения базы, которого нет среди --catalog, есть в госте.
func TestCheckBaseSource(t *testing.T) {
	t.Parallel()
	p := writeInstalled(t, `[{"name":"tool","version":"1.0.0","source":"vendor"}]`)
	installed, err := ReadInstalled(p)
	if err != nil {
		t.Fatal(err)
	}
	r := runCheck(t, installed, "upgrade tool", "install vendor/other", "upgrade --all")
	for i, want := range []string{"skipped", "skipped", "ok"} {
		if got := stepCode(r.Steps[i]); got != want {
			t.Errorf("шаг %d: %s %+v", i+1, got, r.Steps[i].Error)
		}
	}
}

// Корень каталога «.» (roots: ["."]) находится и в архиве, и в каталоге.
func TestCheckRootDot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cream-catalog.yaml"),
		[]byte("schema: cream/v1\nname: dot\nversion: 1.0.0\nroots: [\".\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x", "extension.yaml"), []byte("schema: cream/v1\nname: x\n"+
		"version: 1.0.0\nsummary: x\ncream: \">=1.0.0\"\nplatform: {family: deb, arch: [amd64]}\nkind: library\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{dir, packCatalog(t, dir)} {
		c, err := OpenCheckCatalog("dot", p)
		if err != nil {
			t.Fatal(err)
		}
		r := Check(CheckRequest{Catalogs: []*CheckCatalog{c}, Host: checkHost(), Cream: checkCream,
			Steps: []string{"install x"}})
		_ = c.Close()
		if got := stepCode(r.Steps[0]); got != "ok" {
			t.Errorf("%s: %s %+v", p, got, r.Steps[0].Error)
		}
	}
}
