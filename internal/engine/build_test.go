package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iamletenkov/cream/internal/catalog"
	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/fsx"
	"github.com/iamletenkov/cream/internal/resolve"
	"github.com/iamletenkov/cream/internal/state"
	"github.com/iamletenkov/cream/internal/systemd"
)

// buildSecretValue — значение секрета в тестах фазы build.
const buildSecretValue = "s3cr3t-build-value"

// buildEngine — тестовый движок гостя «Debian 12» без источников: фаза build
// берёт расширение из --from-dir.
func buildEngine(t *testing.T) *testEngine {
	t.Helper()
	te := newTestEngine(t)
	writeFile(t, te, "/etc/os-release", "ID=debian\nVERSION_ID=12\n", 0o644)
	return te
}

// buildIncoming раскладывает приёмник incoming/<id>, как bisquite через
// --tar-in: дерево src в <id>/<имя>, params.env (0600) с телом params и
// record.json — слой resolve с отпечатком дерева и params_digest от
// paramsForDigest (nil — от params). Возвращает запрос фазы build.
func buildIncoming(t *testing.T, te *testEngine, id, src, params string, mutate ...func(*resolve.Layer)) BuildRequest {
	t.Helper()
	en, err := catalog.LoadExtension(src, te.Cream)
	if err != nil {
		t.Fatalf("расширение %s: %v", src, err)
	}
	name := en.Manifest.Name
	inc := te.L.Incoming(id)
	dir := filepath.Join(inc, name)
	if err := os.MkdirAll(inc, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := fsx.CopyTree(src, dir); err != nil {
		t.Fatal(err)
	}
	vals := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(params), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			vals[k] = v
		}
	}
	secret := func(k string) bool {
		sp, ok := en.Manifest.Config.Lookup(k)
		return ok && sp.Secret != ""
	}
	layer := resolve.Layer{Name: name, Version: en.Manifest.Version.String(), Source: "core",
		SourceResolved: "sha256:" + strings.Repeat("ab", 32), CatalogVersion: "4.0.0", ExtDigest: en.Digest,
		ParamsDigest: state.ParamsDigest(vals, secret), Deps: []string{}, Mode: resolve.ModeExec,
		Token: "core/" + name + "@" + en.Manifest.Version.String() + "#abababababab", Pinned: true, SecretParams: []string{}}
	for _, f := range mutate {
		f(&layer)
	}
	data, err := json.Marshal(layer)
	if err != nil {
		t.Fatal(err)
	}
	rec := filepath.Join(inc, "record.json")
	if err := os.WriteFile(rec, data, 0o600); err != nil {
		t.Fatal(err)
	}
	pf := filepath.Join(inc, "params.env")
	if err := os.WriteFile(pf, []byte(params), 0o600); err != nil {
		t.Fatal(err)
	}
	return BuildRequest{FromDir: dir, Record: rec, ParamsFile: pf, RemoveParamsFile: true}
}

// buildExt пишет во временный каталог расширение name с манифестом
// (заголовок + extra) и хуками install (телом install) и remove.
func buildExt(t *testing.T, name, extra, install string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Join(dir, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := "schema: cream/v1\nname: " + name + "\nversion: 1.0.0\nsummary: фикстура " + name +
		"\ncream: \">=1.0.0\"\nplatform: {family: deb, arch: [amd64, arm64]}\nhooks:\n  install: {}\n  remove: {}\n" + extra
	if err := os.WriteFile(filepath.Join(dir, "extension.yaml"), []byte(m), 0o644); err != nil {
		t.Fatal(err)
	}
	for hook, body := range map[string]string{"install": install, "remove": "exit 0"} {
		p := filepath.Join(dir, "hooks", hook)
		if err := os.WriteFile(p, []byte("#!/bin/sh\nset -eu\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// buildPolicyProbe — расширение, хук install которого записывает код
// policy-rc.d (или «нет») в состояние: так видно, стоял ли он во время хука.
func buildPolicyProbe(t *testing.T) string {
	t.Helper()
	return buildExt(t, "policy-probe", "", `mkdir -p "$CREAM_STATE_DIR"
p="$CREAM_ROOT/usr/sbin/policy-rc.d"
if [ -x "$p" ]; then rc=0; "$p" x start || rc=$?; echo "$rc" > "$CREAM_STATE_DIR/policy"; else echo нет > "$CREAM_STATE_DIR/policy"; fi`)
}

func TestBuildInstall(t *testing.T) {
	t.Parallel()
	te := buildEngine(t)
	te.systemdRunning("running") // в госте systemd нет, но и при «запущенном» build не трогает его
	req := buildIncoming(t, te, "l1", fixtureDir(t, "configure-system"), "MESSAGE=из-образа\n")
	res, err := te.InstallFromDir(context.Background(), req)
	if err != nil {
		t.Fatalf("build: %v (stderr: %s)", err, te.Err.String())
	}
	if !res.Changed || res.Status != state.StatusInstalled || res.Phase != PhaseBuild {
		t.Fatalf("результат: %+v", res)
	}
	rec := installRecord(t, te, "configure-system")
	if rec.Phase != PhaseBuild || rec.Source.Name != "core" || rec.Source.Type != "archive" ||
		rec.Source.Resolved != "sha256:"+strings.Repeat("ab", 32) || rec.Source.CatalogVersion != "4.0.0" ||
		rec.Configure == nil || rec.Configure.Target != "multi-user.target" {
		t.Errorf("запись: %+v", rec)
	}
	link := filepath.Join(te.L.WantsDir("multi-user.target"), systemd.UnitName("configure-system"))
	if !installExists(link) {
		t.Errorf("нет ссылки %s", link)
	}
	if n := hookCount(t, te, "configure-system", "install"); n != 1 {
		t.Errorf("install.count = %d", n)
	}
	if log := readState(t, te, "configure-system", "install.log"); !strings.Contains(log, "phase=build") {
		t.Errorf("install.log: %q", log)
	}
	// configure только включается: не вызывается и daemon-reload нет.
	if n := hookCount(t, te, "configure-system", "configure"); n != 0 {
		t.Errorf("configure вызван %d раз", n)
	}
	for _, c := range te.Sys.Calls() {
		if c != "is-system-running" {
			t.Errorf("вызовы systemctl в фазе build: %v", te.Sys.Calls())
		}
	}
	if v := readConfig(t, te, "configure-system"); !strings.Contains(v, "из-образа") {
		t.Errorf("config: %q", v)
	}
	for _, p := range []string{te.L.PolicyRcD(), req.ParamsFile, te.L.Incoming("l1")} {
		if installExists(p) {
			t.Errorf("%s остался", p)
		}
	}
	if h := installHistory(t, te); !strings.Contains(h, `"phase":"build"`) {
		t.Errorf("history: %s", h)
	}
}

// readConfig — файл config расширения.
func readConfig(t *testing.T, te *testEngine, ext string) string {
	t.Helper()
	data, err := os.ReadFile(te.L.ConfigFile(ext))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestBuildPolicyRcD(t *testing.T) {
	t.Parallel()
	t.Run("ставится на время хука и снимается", func(t *testing.T) {
		t.Parallel()
		te := buildEngine(t)
		if _, err := te.InstallFromDir(context.Background(), buildIncoming(t, te, "p1", buildPolicyProbe(t), "")); err != nil {
			t.Fatalf("build: %v (stderr: %s)", err, te.Err.String())
		}
		if got := strings.TrimSpace(readState(t, te, "policy-probe", "policy")); got != "101" {
			t.Errorf("policy-rc.d во время хука: %q, ждали 101", got)
		}
		if installExists(te.L.PolicyRcD()) {
			t.Error("policy-rc.d не снят")
		}
	})
	t.Run("чужой сохраняется нетронутым", func(t *testing.T) {
		t.Parallel()
		te := buildEngine(t)
		own := "#!/bin/sh\n# свой\nexit 0\n"
		writeFile(t, te, "/usr/sbin/policy-rc.d", own, 0o755)
		if _, err := te.InstallFromDir(context.Background(), buildIncoming(t, te, "p2", buildPolicyProbe(t), "")); err != nil {
			t.Fatalf("build: %v", err)
		}
		if got := strings.TrimSpace(readState(t, te, "policy-probe", "policy")); got != "0" {
			t.Errorf("во время хука работал не чужой policy-rc.d: %q", got)
		}
		data, err := os.ReadFile(te.L.PolicyRcD())
		st, serr := os.Stat(te.L.PolicyRcD())
		if err != nil || serr != nil || string(data) != own || st.Mode().Perm() != 0o755 {
			t.Errorf("чужой policy-rc.d изменён: %q %v %v", data, err, serr)
		}
	})
	t.Run("хук упал — снят, status failed", func(t *testing.T) {
		t.Parallel()
		te := buildEngine(t)
		req := buildIncoming(t, te, "p3", fixtureDir(t, "failing-install"), "FAIL=1\n")
		_, err := te.InstallFromDir(context.Background(), req)
		wantCode(t, err, cerr.ExitHook, "hook.failed")
		if installExists(te.L.PolicyRcD()) {
			t.Error("policy-rc.d не снят после сбоя")
		}
		if rec := installRecord(t, te, "failing-install"); rec.Status != state.StatusFailed || rec.Phase != PhaseBuild {
			t.Errorf("запись: %+v", rec)
		}
		if installExists(req.ParamsFile) {
			t.Error("params.env не удалён после сбоя")
		}
		if !installExists(te.L.Incoming("p3")) {
			t.Error("incoming удалён после сбоя — нечего разбирать")
		}
	})
}

func TestBuildMismatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*resolve.Layer)
		tamper bool // подменить файл дерева после record.json
		field  string
	}{
		{name: "подмена файла дерева", tamper: true, field: "ext_digest"},
		{name: "params_digest", mutate: func(l *resolve.Layer) {
			l.ParamsDigest = state.ParamsDigest(map[string]string{"MESSAGE": "другое"}, nil)
		}, field: "params_digest"},
		{name: "имя", mutate: func(l *resolve.Layer) { l.Name = "other" }, field: "name"},
		{name: "версия", mutate: func(l *resolve.Layer) { l.Version = "1.0.1" }, field: "version"},
		{name: "отпечаток", mutate: func(l *resolve.Layer) { l.ExtDigest = "sha256:" + strings.Repeat("0", 64) }, field: "ext_digest"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			te := buildEngine(t)
			var mut []func(*resolve.Layer)
			if tc.mutate != nil {
				mut = append(mut, tc.mutate)
			}
			req := buildIncoming(t, te, "m", fixtureDir(t, "configure-system"), "MESSAGE=x\n", mut...)
			if tc.tamper {
				if err := os.WriteFile(filepath.Join(req.FromDir, "hooks", "install"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			_, err := te.InstallFromDir(context.Background(), req)
			wantCode(t, err, cerr.ExitData, "record.mismatch")
			var ce *cerr.Error
			if !errors.As(err, &ce) || ce.Details["field"] != tc.field {
				t.Errorf("details: %+v", ce)
			}
			if _, ok, _ := te.Store.Get("configure-system"); ok {
				t.Error("запись реестра появилась")
			}
			if installExists(te.L.ExtTree("configure-system")) || installExists(te.L.PolicyRcD()) {
				t.Error("дерево или policy-rc.d после отказа")
			}
			if installExists(req.ParamsFile) {
				t.Error("params.env не удалён после отказа")
			}
		})
	}
}

func TestBuildRefusals(t *testing.T) {
	t.Parallel()
	t.Run("record.json не JSON", func(t *testing.T) {
		t.Parallel()
		te := buildEngine(t)
		req := buildIncoming(t, te, "r1", fixtureDir(t, "noop"), "")
		if err := os.WriteFile(req.Record, []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := te.InstallFromDir(context.Background(), req)
		wantCode(t, err, cerr.ExitData, "record.invalid")
	})
	t.Run("params.env не 0600", func(t *testing.T) {
		t.Parallel()
		te := buildEngine(t)
		req := buildIncoming(t, te, "r2", fixtureDir(t, "noop"), "")
		if err := os.Chmod(req.ParamsFile, 0o644); err != nil {
			t.Fatal(err)
		}
		req.CheckPerm = true
		_, err := te.InstallFromDir(context.Background(), req)
		wantCode(t, err, cerr.ExitPerm, "")
		if _, ok, _ := te.Store.Get("noop"); ok {
			t.Error("запись реестра появилась")
		}
	})
	t.Run("params.env остаётся без флага", func(t *testing.T) {
		t.Parallel()
		te := buildEngine(t)
		req := buildIncoming(t, te, "r3", fixtureDir(t, "noop"), "")
		req.RemoveParamsFile = false
		req.ParamsFile = filepath.Join(t.TempDir(), "params.env")
		if err := os.WriteFile(req.ParamsFile, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := te.InstallFromDir(context.Background(), req); err != nil {
			t.Fatal(err)
		}
		if !installExists(req.ParamsFile) {
			t.Error("params.env удалён без --remove-params-file")
		}
	})
	t.Run("targets.build none", func(t *testing.T) {
		t.Parallel()
		te := buildEngine(t)
		src := buildExt(t, "build-none", "targets: {build: none}\n", "exit 0")
		_, err := te.InstallFromDir(context.Background(), buildIncoming(t, te, "r4", src, ""))
		wantCode(t, err, cerr.ExitNotApplicable, "target.build")
	})
	t.Run("зависимость — из реестра гостя", func(t *testing.T) {
		t.Parallel()
		te := buildEngine(t)
		_, err := te.InstallFromDir(context.Background(), buildIncoming(t, te, "d1", fixtureDir(t, "depends-lib"), ""))
		wantCode(t, err, cerr.ExitGraph, "deps.missing")
		seedInstalled(t, te, fixtureDir(t, "lib-fixture"))
		if _, err := te.InstallFromDir(context.Background(), buildIncoming(t, te, "d2", fixtureDir(t, "depends-lib"), "")); err != nil {
			t.Fatalf("с унаследованной lib-fixture: %v", err)
		}
	})
	t.Run("каталог не по имени", func(t *testing.T) {
		t.Parallel()
		te := buildEngine(t)
		req := buildIncoming(t, te, "n1", fixtureDir(t, "noop"), "")
		other := filepath.Join(filepath.Dir(req.FromDir), "renamed")
		if err := os.Rename(req.FromDir, other); err != nil {
			t.Fatal(err)
		}
		req.FromDir = other
		_, err := te.InstallFromDir(context.Background(), req)
		wantCode(t, err, cerr.ExitData, "")
	})
}

// TestBuildSecret: секрет из params.env — в secrets расширения и больше нигде:
// ни в выводе хука, ни в результате, ни в реестре, ни в журнале.
func TestBuildSecret(t *testing.T) {
	t.Parallel()
	te := buildEngine(t)
	src := buildExt(t, "secret-probe",
		"config:\n  keys:\n    TOKEN: {type: str, default: \"\", empty: true, secret: file, description: \"секрет\"}\n"+
			"    LEVEL: {type: str, default: low, description: \"обычный\"}\n",
		`cat "$CREAM_SECRETS_FILE"; cat "$CREAM_SECRETS_FILE" >&2; env >&2`)
	req := buildIncoming(t, te, "s1", src, "TOKEN="+buildSecretValue+"\nLEVEL=high\n")
	res, err := te.InstallFromDir(context.Background(), req)
	if err != nil {
		t.Fatalf("build: %v (stderr: %s)", err, te.Err.String())
	}
	sec, err := os.ReadFile(te.L.SecretsFile("secret-probe"))
	if err != nil || !strings.Contains(string(sec), buildSecretValue) {
		t.Fatalf("secrets: %q %v", sec, err)
	}
	out, _ := json.Marshal(res)
	reg, _ := os.ReadFile(te.L.InstalledFile("secret-probe"))
	for what, s := range map[string]string{"stdout": te.Out.String(), "stderr": te.Err.String(),
		"result": string(out), "реестр": string(reg), "history": installHistory(t, te)} {
		if strings.Contains(s, buildSecretValue) {
			t.Errorf("секрет в %s: %s", what, s)
		}
	}
	if !strings.Contains(te.Err.String(), "***") {
		t.Errorf("вывод хука не замаскирован: %q", te.Err.String())
	}
	want := state.ParamsDigest(map[string]string{"TOKEN": "x", "LEVEL": "high"}, func(k string) bool { return k == "TOKEN" })
	if res.ParamsDigest != want {
		t.Errorf("params_digest %s, ждали %s (секрет — только <secret>)", res.ParamsDigest, want)
	}
}
