package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/iamletenkov/cream/internal/catalog"
	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/config"
	"github.com/iamletenkov/cream/internal/fsx"
	"github.com/iamletenkov/cream/internal/source"
	"github.com/iamletenkov/cream/internal/state"
)

// installSrc — источник path для теста: имя и каталог каталога.
type installSrc struct{ name, dir string }

// installEngine — тестовый движок на «Debian 12» (os-release под корнем) с
// источниками srcs, синхронизированными через source.Syncer; без srcs —
// один источник fixtures = testdata/catalog.
func installEngine(t *testing.T, srcs ...installSrc) *testEngine {
	t.Helper()
	te := newTestEngine(t)
	writeFile(t, te, "/etc/os-release", "ID=debian\nVERSION_ID=12\n", 0o644)
	if len(srcs) == 0 {
		srcs = []installSrc{{"fixtures", testdataDir(t, "catalog")}}
	}
	installSync(t, te, srcs...)
	return te
}

// installSync переписывает sources.yaml списком srcs и синхронизирует каждый.
func installSync(t *testing.T, te *testEngine, srcs ...installSrc) {
	t.Helper()
	f := &source.File{}
	for _, s := range srcs {
		f.Sources = append(f.Sources, source.Source{Name: s.name, Type: source.TypePath, Path: s.dir})
	}
	if err := os.MkdirAll(filepath.Dir(te.L.Sources()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(te.L.Sources()); err != nil {
		t.Fatal(err)
	}
	sy := &source.Syncer{Layout: te.L, Now: te.Now, Validate: func(dir string) (string, error) {
		r, err := catalog.Validate(dir, te.Cream)
		return r.CatalogVersion, err
	}}
	for _, s := range f.Sources {
		if _, err := sy.Sync(context.Background(), s); err != nil {
			t.Fatalf("sync %s: %v", s.Name, err)
		}
	}
}

// installCatalog собирает во временном каталоге каталог из расширений
// (имя → каталог-источник) с версией каталога version.
func installCatalog(t *testing.T, version string, exts map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	body := "schema: cream/v1\nname: cream-fixtures\nversion: " + version + "\nroots: [extensions]\n"
	if err := os.WriteFile(filepath.Join(dir, "cream-catalog.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, src := range exts {
		if err := fsx.CopyTree(src, filepath.Join(dir, "extensions", name)); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// installReq — запрос установки ссылок refs с параметрами args (KEY=VAL).
func installReq(refs []string, args ...string) InstallRequest {
	return InstallRequest{Refs: refs, Params: config.AssignInput{Args: args, Stdin: strings.NewReader("")}}
}

// installRun ставит одну ссылку с параметрами.
func installRun(t *testing.T, te *testEngine, ref string, args ...string) (*InstallResult, error) {
	t.Helper()
	return te.Install(context.Background(), installReq([]string{ref}, args...))
}

// installMust ставит и требует успеха.
func installMust(t *testing.T, te *testEngine, ref string, args ...string) *InstallResult {
	t.Helper()
	res, err := installRun(t, te, ref, args...)
	if err != nil {
		t.Fatalf("install %s: %v (stderr: %s)", ref, err, te.Err.String())
	}
	return res
}

// installRecord читает запись реестра (должна быть).
func installRecord(t *testing.T, te *testEngine, ext string) state.Record {
	t.Helper()
	rec, ok, err := te.Store.Get(ext)
	if err != nil || !ok {
		t.Fatalf("реестр %s: ok=%v err=%v", ext, ok, err)
	}
	return *rec
}

// installHistory — строки history.jsonl.
func installHistory(t *testing.T, te *testEngine) string {
	t.Helper()
	data, err := os.ReadFile(te.L.History())
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(data)
}

func installExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func TestInstallFirst(t *testing.T) {
	t.Parallel()
	te := installEngine(t)
	res := installMust(t, te, "noop")
	if !res.Changed || res.Status != state.StatusInstalled || res.Name != "noop" || res.Version != "1.0.0" {
		t.Fatalf("результат: %+v", res)
	}
	rec := installRecord(t, te, "noop")
	digest, err := fsx.TreeDigest(fixtureDir(t, "noop"))
	if err != nil {
		t.Fatal(err)
	}
	if rec.Schema != state.SchemaV1 || rec.Kind != "extension" || rec.Phase != PhaseLive ||
		rec.ExtDigest != digest || rec.CreamVersion != "1.0.0" || rec.Configure != nil ||
		!strings.HasPrefix(rec.ParamsDigest, "sha256:") || !rec.InstalledAt.Equal(testNow) {
		t.Errorf("запись: %+v", rec)
	}
	if rec.Source.Name != "fixtures" || rec.Source.Type != "path" || rec.Source.Origin != testdataDir(t, "catalog") ||
		!strings.HasPrefix(rec.Source.Resolved, "sha256:") || rec.Source.CatalogVersion != "1.0.0" {
		t.Errorf("source: %+v", rec.Source)
	}
	if res.ExtDigest != rec.ExtDigest {
		t.Errorf("результат и реестр расходятся: %+v / %+v", res.Record, rec)
	}
	if n := hookCount(t, te, "noop", "install"); n != 1 {
		t.Errorf("install.count = %d", n)
	}
	if log := readState(t, te, "noop", "install.log"); !strings.Contains(log, "phase=live ") || !strings.Contains(log, "version=1.0.0") {
		t.Errorf("install.log: %q", log)
	}
	if !installExists(filepath.Join(te.L.ExtTree("noop"), "extension.yaml")) || installExists(te.L.ExtNew("noop")) {
		t.Error("дерево не на месте или остался .new")
	}
	if h := installHistory(t, te); !strings.Contains(h, `"op":"install","ext":"noop","from":"","to":"1.0.0","status":"installed","phase":"live"`) {
		t.Errorf("history: %s", h)
	}
}

func TestInstallRepeatUnchanged(t *testing.T) {
	t.Parallel()
	te := installEngine(t)
	installMust(t, te, "noop")
	res := installMust(t, te, "noop")
	if res.Changed || res.Status != state.StatusInstalled {
		t.Errorf("повтор: %+v", res)
	}
	if n := hookCount(t, te, "noop", "install"); n != 1 {
		t.Errorf("хук вызван повторно: %d", n)
	}
	if h := installHistory(t, te); strings.Count(h, "\n") != 1 {
		t.Errorf("повтор без изменений попал в журнал: %s", h)
	}
	// --reinstall вызывает хук всегда.
	r := installReq([]string{"noop"})
	r.Reinstall = true
	res, err := te.Install(context.Background(), r)
	if err != nil || !res.Changed {
		t.Fatalf("--reinstall: %+v %v", res, err)
	}
	if n := hookCount(t, te, "noop", "install"); n != 2 {
		t.Errorf("--reinstall: install.count = %d", n)
	}
}

func TestInstallFailedRetries(t *testing.T) {
	t.Parallel()
	te := installEngine(t)
	_, err := installRun(t, te, "failing-install")
	wantCode(t, err, cerr.ExitHook, "hook.failed")
	rec := installRecord(t, te, "failing-install")
	if rec.Status != state.StatusFailed || !installExists(te.L.ExtTree("failing-install")) {
		t.Fatalf("после сбоя: %+v", rec)
	}
	if h := installHistory(t, te); !strings.Contains(h, `"status":"failed"`) {
		t.Errorf("history: %s", h)
	}
	// Повтор без флагов вызывает хук снова.
	_, err = installRun(t, te, "failing-install")
	wantCode(t, err, cerr.ExitHook, "hook.failed")
	if n := hookCount(t, te, "failing-install", "install"); n != 2 {
		t.Errorf("повтор после сбоя: install.count = %d", n)
	}
	res := installMust(t, te, "failing-install", "FAIL=0")
	if !res.Changed || res.Status != state.StatusInstalled || hookCount(t, te, "failing-install", "install") != 3 {
		t.Errorf("исправленный повтор: %+v", res)
	}
}

func TestInstallScopeKey(t *testing.T) {
	t.Parallel()
	te := installEngine(t)
	installMust(t, te, "install-scope", "BIN_VERSION=1.0.0")
	tests := []struct {
		name      string
		args      []string
		wantCount int
		changed   bool
	}{
		{name: "same install-scope value", args: []string{"BIN_VERSION=1.0.0"}, wantCount: 1},
		{name: "new install-scope value", args: []string{"BIN_VERSION=2.0.0"}, wantCount: 2, changed: true},
		{name: "same again", args: []string{"BIN_VERSION=2.0.0"}, wantCount: 2},
		{name: "other key only", args: []string{"LEVEL=high"}, wantCount: 2},
		{name: "invalid install-scope value", args: []string{"BIN_VERSION=x"}, wantCount: 2},
	}
	for _, tc := range tests {
		res, err := installRun(t, te, "install-scope", tc.args...)
		if tc.name == "invalid install-scope value" {
			wantCode(t, err, cerr.ExitData, "config.invalid")
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if res.Changed != tc.changed {
			t.Errorf("%s: changed = %v", tc.name, res.Changed)
		}
		if n := hookCount(t, te, "install-scope", "install"); n != tc.wantCount {
			t.Errorf("%s: install.count = %d, ждали %d", tc.name, n, tc.wantCount)
		}
	}
	if got := strings.TrimSpace(readState(t, te, "install-scope", "bin_version")); got != "2.0.0" {
		t.Errorf("bin_version = %q", got)
	}
	if v, err := te.Get("install-scope", "LEVEL", false); err != nil || v != "high" {
		t.Errorf("LEVEL = %q, %v", v, err)
	}
}

func TestInstallDepsMissingCandidates(t *testing.T) {
	t.Parallel()
	only := installCatalog(t, "1.0.0", map[string]string{"depends-lib": fixtureDir(t, "depends-lib")})
	tests := []struct {
		name  string
		srcs  []installSrc
		wantC []installCandidate
	}{
		{name: "candidate in a later source",
			srcs:  []installSrc{{"only", only}, {"fixtures", testdataDir(t, "catalog")}},
			wantC: []installCandidate{{Name: "lib-fixture", Version: "1.0.0", Source: "fixtures", Matches: true}}},
		{name: "no candidate", srcs: []installSrc{{"only", only}}, wantC: []installCandidate{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			te := installEngine(t, tc.srcs...)
			_, err := installRun(t, te, "depends-lib")
			wantCode(t, err, cerr.ExitGraph, "deps.missing")
			details := cerr.DetailsOf(err)
			got, ok := details["candidates"].([]installCandidate)
			if !ok || !slices.Equal(got, tc.wantC) {
				t.Errorf("candidates = %#v, ждали %#v", details["candidates"], tc.wantC)
			}
			if _, ok, _ := te.Store.Get("depends-lib"); ok {
				t.Error("запись реестра появилась при deps.missing")
			}
		})
	}
}

func TestInstallDependsOrder(t *testing.T) {
	t.Parallel()
	te := installEngine(t)
	res, err := te.InstallAll(context.Background(), installReq([]string{"depends-lib", "lib-fixture"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].Name != "lib-fixture" || res[1].Name != "depends-lib" {
		t.Fatalf("порядок: %+v", res)
	}
	if g := readState(t, te, "depends-lib", "greeting"); g != "lib-fixture: depends-lib\n" {
		t.Errorf("greeting = %q", g)
	}
	if rec := installRecord(t, te, "lib-fixture"); rec.Kind != "library" || rec.Status != state.StatusInstalled {
		t.Errorf("библиотека: %+v", rec)
	}
	// Поштучно: сначала библиотека, потом потребитель.
	te2 := installEngine(t)
	installMust(t, te2, "lib-fixture")
	installMust(t, te2, "depends-lib")
}

func TestInstallGraphAndTargets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		before   []string
		ref      string
		mutate   func(*InstallRequest)
		wantExit int
		wantCode string
	}{
		{name: "conflict by name", before: []string{"noop"}, ref: "conflict-name", wantExit: 67, wantCode: "conflict.extension"},
		{name: "conflict by name reverse", before: []string{"conflict-name"}, ref: "noop", wantExit: 67, wantCode: "conflict.extension"},
		{name: "conflict by capability", before: []string{"apply-hook"}, ref: "conflict-cap", wantExit: 67, wantCode: "conflict.capability"},
		{name: "requires without provider", ref: "requires-cap", wantExit: 67, wantCode: "deps.requires"},
		{name: "requires with provider", before: []string{"apply-hook"}, ref: "requires-cap"},
		{name: "live confirm without accept-risk", ref: "live-confirm", wantExit: 68, wantCode: "target.confirm"},
		{name: "live confirm with accept-risk", ref: "live-confirm", mutate: func(r *InstallRequest) { r.AcceptRisk = true }},
		{name: "not found", ref: "nope", wantExit: 66, wantCode: "not_found.extension"},
		{name: "version assertion", ref: "noop@2.0.0", wantExit: 66, wantCode: "version.mismatch"},
		{name: "bad ref", ref: "Bad Name", wantExit: 64, wantCode: "usage.ref"},
		{name: "source flag and ref disagree", ref: "fixtures/noop", mutate: func(r *InstallRequest) { r.Source = "other" },
			wantExit: 64, wantCode: "usage.source"},
		{name: "source flag", ref: "noop", mutate: func(r *InstallRequest) { r.Source = "fixtures" }},
		{name: "unknown source", ref: "noop", mutate: func(r *InstallRequest) { r.Source = "other" }, wantExit: 66, wantCode: "not_found.source"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			te := installEngine(t)
			for _, b := range tc.before {
				installMust(t, te, b)
			}
			r := installReq([]string{tc.ref})
			if tc.mutate != nil {
				tc.mutate(&r)
			}
			_, err := te.Install(context.Background(), r)
			if tc.wantExit == 0 {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			wantCode(t, err, tc.wantExit, tc.wantCode)
		})
	}
}

func TestInstallNotApplicable(t *testing.T) {
	t.Parallel()
	te := installEngine(t)
	_, err := installRun(t, te, "not-applicable")
	wantCode(t, err, cerr.ExitNotApplicable, "hook.not_applicable")
	if rec := installRecord(t, te, "not-applicable"); rec.Status != state.StatusSkipped {
		t.Errorf("без --allow-skip: %+v", rec)
	}
	r := installReq([]string{"not-applicable"})
	r.AllowSkip = true
	res, err := te.Install(context.Background(), r)
	if err != nil || res.Status != state.StatusSkipped {
		t.Fatalf("--allow-skip: %+v %v", res, err)
	}
	if n := hookCount(t, te, "not-applicable", "install"); n != 2 {
		t.Errorf("skipped повторяет хук: install.count = %d", n)
	}
}

func TestInstallRebootRequired(t *testing.T) {
	t.Parallel()
	te := installEngine(t)
	res := installMust(t, te, "reboot-required")
	if !res.RebootRequired || !installRecord(t, te, "reboot-required").RebootRequired {
		t.Errorf("reboot_required: %+v", res)
	}
	if !installExists(te.L.RebootRequired()) {
		t.Error("нет /run/reboot-required")
	}
}

func installSum(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:])
}

func TestInstallFilesMode(t *testing.T) {
	t.Parallel()
	te := installEngine(t)
	installMust(t, te, "noop")
	res := installMust(t, te, "files-mode")
	want := map[string]os.FileMode{
		"/etc/files-mode/plain.conf":          0o644,
		"/etc/files-mode/private.conf":        0o600,
		"/usr/local/bin/files-mode-tool":      0o755,
		"/etc/files-mode/by-bit.conf":         0o644,
		"/etc/cream/noop/labels.d/files-mode": 0o644,
	}
	if len(res.Files) != len(want) {
		t.Fatalf("files: %+v", res.Files)
	}
	for _, f := range res.Files {
		p := filepath.Join(te.Root, f.Dst)
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != want[f.Dst] {
			t.Errorf("%s: режим %v, ждали %v", f.Dst, st.Mode().Perm(), want[f.Dst])
		}
		if f.SHA256 != installSum(t, p) {
			t.Errorf("%s: sha256 %s", f.Dst, f.SHA256)
		}
	}
	// Р11: файл в каталоге noop — в реестре files-mode; повтор не конфликтует сам с собой.
	r := installReq([]string{"files-mode"})
	r.Reinstall = true
	if _, err := te.Install(context.Background(), r); err != nil {
		t.Fatalf("повтор files-mode: %v", err)
	}
	if noop := installRecord(t, te, "noop"); len(noop.Files) != 0 {
		t.Errorf("файл записан в реестр noop: %+v", noop.Files)
	}
}

// installPatched — каталог из одного расширения-фикстуры name (src — его
// каталог), в манифесте которого edit правит текст extension.yaml.
func installPatched(t *testing.T, src, name string, edit func(string) string) string {
	t.Helper()
	dir := installCatalog(t, "1.0.0", map[string]string{name: src})
	mf := filepath.Join(dir, "extensions", name, "extension.yaml")
	data, err := os.ReadFile(mf)
	if err != nil {
		t.Fatal(err)
	}
	out := edit(string(data))
	if out == string(data) {
		t.Fatalf("%s: правка манифеста ничего не изменила", name)
	}
	if err := os.WriteFile(mf, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// installAppend — правка манифеста: дописать tail в конец.
func installAppend(tail string) func(string) string {
	return func(s string) string { return s + tail }
}

func TestInstallFileUnowned(t *testing.T) {
	t.Parallel()
	// Расширение с files и параметром: при 67 домен не пишется (§6.5: параметры — после files).
	dir := installPatched(t, fixtureDir(t, "files-mode"), "files-mode",
		installAppend("config:\n  keys:\n    LEVEL: {type: \"enum:low,high\", default: low, description: \"уровень\"}\n"))
	te := installEngine(t, installSrc{"fixtures", dir})
	writeFile(t, te, "/etc/files-mode/plain.conf", "чужой\n", 0o600)
	_, err := installRun(t, te, "files-mode", "LEVEL=high")
	wantCode(t, err, cerr.ExitGraph, "conflict.file_unowned")
	if installExists(te.L.ConfigFile("files-mode")) {
		t.Error("домен записан при конфликте файла")
	}
	if _, ok, _ := te.Store.Get("files-mode"); ok {
		t.Error("запись реестра при конфликте файла")
	}
	if installExists(te.L.ExtTree("files-mode")) || installExists(te.L.ExtNew("files-mode")) {
		t.Error("дерево осталось при конфликте файла")
	}
}

func TestInstallSecretNotLeaked(t *testing.T) {
	t.Parallel()
	te := installEngine(t)
	const secret = "s3cr3t-value-42"
	r := installReq([]string{"with-config"}, "TOKEN=-", "COUNT=7")
	r.Params.Stdin = strings.NewReader(secret + "\n")
	res, err := te.Install(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := te.Get("with-config", "TOKEN", true); err != nil || v != secret {
		t.Fatalf("секрет не записан: %q %v", v, err)
	}
	reg, err := os.ReadFile(te.L.InstalledFile("with-config"))
	if err != nil {
		t.Fatal(err)
	}
	for what, s := range map[string]string{"реестр": string(reg), "history": installHistory(t, te),
		"stdout": te.Out.String(), "stderr": te.Err.String(), "результат": res.ParamsDigest} {
		if strings.Contains(s, secret) {
			t.Errorf("секрет в %s", what)
		}
	}
	if v, _ := te.Get("with-config", "COUNT", false); v != "7" {
		t.Errorf("COUNT = %q", v)
	}
}

func TestInstallConfigureLive(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		state     string // "" — systemd не запущен
		wantCalls []string
		wantWarn  string
		wantCount int
	}{
		{name: "running", state: "running",
			wantCalls: []string{"daemon-reload", "restart cream-configure@configure-system.service"}},
		{name: "starting", state: "starting",
			wantCalls: []string{"daemon-reload", "restart --no-block cream-configure@configure-system.service"},
			wantWarn:  "configure.no_block"},
		{name: "no systemd", wantCount: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			te := installEngine(t)
			if tc.state != "" {
				te.systemdRunning(tc.state)
			}
			res := installMust(t, te, "configure-system")
			if res.Configure == nil || res.Configure.Stage != "system" || res.Configure.Target != "multi-user.target" {
				t.Errorf("configure: %+v", res.Configure)
			}
			link := filepath.Join(te.L.WantsDir("multi-user.target"), "cream-configure@configure-system.service")
			if !installExists(link) || !installExists(te.L.DropIn("configure-system")) {
				t.Error("configure не включён")
			}
			var calls []string
			for _, c := range te.Sys.Calls() {
				if c != "is-system-running" {
					calls = append(calls, c)
				}
			}
			if !slices.Equal(calls, tc.wantCalls) {
				t.Errorf("systemctl: %q, ждали %q", calls, tc.wantCalls)
			}
			if tc.wantWarn != "" && !slices.Contains(warningCodes(te), tc.wantWarn) {
				t.Errorf("предупреждения %v", warningCodes(te))
			}
			if n := hookCount(t, te, "configure-system", "configure"); n != tc.wantCount {
				t.Errorf("configure.count = %d", n)
			}
			if tc.wantCount > 0 && !strings.Contains(readState(t, te, "configure-system", "configure.log"), "reason=install ") {
				t.Error("configure без CREAM_REASON=install")
			}
		})
	}
}

func TestInstallUsage(t *testing.T) {
	t.Parallel()
	te := installEngine(t)
	_, err := te.InstallAll(context.Background(), installReq([]string{"noop", "with-config"}, "COUNT=1"))
	wantCode(t, err, cerr.ExitUsage, "usage.args")
	_, err = te.InstallAll(context.Background(), installReq(nil))
	wantCode(t, err, cerr.ExitUsage, "usage.args")
	_, err = te.Install(context.Background(), installReq([]string{"noop", "lib-fixture"}))
	wantCode(t, err, cerr.ExitUsage, "usage.args")
	_, err = installRun(t, te, "with-config", "NOPE=1")
	wantCode(t, err, cerr.ExitData, "config.unknown_key")
	_, err = installRun(t, te, "with-config", "TOKEN=x")
	wantCode(t, err, cerr.ExitUsage, "secret.in_argv")
	if _, ok, _ := te.Store.Get("with-config"); ok {
		t.Error("запись при неверных параметрах")
	}
}

func TestInstallVersionChecks(t *testing.T) {
	t.Parallel()
	t.Run("downgrade", func(t *testing.T) {
		t.Parallel()
		te := installEngine(t)
		seedInstalled(t, te, testdataDir(t, "catalog-next", "extensions", "upgrade-fixture"))
		_, err := installRun(t, te, "upgrade-fixture")
		wantCode(t, err, cerr.ExitData, "version.downgrade")
	})
	t.Run("same version other digest", func(t *testing.T) {
		t.Parallel()
		te := installEngine(t)
		seedInstalled(t, te, fixtureDir(t, "noop"), func(r *state.Record) { r.ExtDigest = "sha256:" + strings.Repeat("0", 64) })
		_, err := installRun(t, te, "noop")
		wantCode(t, err, cerr.ExitData, "version.same_digest")
	})
	t.Run("higher version upgrades", func(t *testing.T) {
		t.Parallel()
		te := installEngine(t, installSrc{"next", testdataDir(t, "catalog-next")})
		seedInstalled(t, te, fixtureDir(t, "upgrade-fixture"))
		res := installMust(t, te, "upgrade-fixture")
		if res.Version != "2.0.0" || hookCount(t, te, "upgrade-fixture", "upgrade") != 1 {
			t.Errorf("install новой версии: %+v", res)
		}
	})
}
