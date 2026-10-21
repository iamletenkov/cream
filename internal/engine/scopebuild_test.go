package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/config"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/resolve"
	"github.com/iamletenkov/cream/internal/state"
)

// Ключи scope: build (ADR-0004, спека 2026-10-21 §10.9): значение живёт только
// в окружении хука install/upgrade текущей операции и нигде на машине не
// хранится — ни в config/secrets домена, ни в params_digest, ни в журнале, ни в
// cream.json; configure его не видит; set/get — 64 config.scope_build.

const (
	sbTokenMarker = "build-secret-marker-5f1c"
	sbURLMarker   = "http://mirror-marker-9a7e.invalid"
)

// sbHook — общий хук фикстуры: считает вызовы и пишет в состояние не значения
// ключей сборки, а их sha256 (иначе сам хук положил бы маркер на диск), argv
// своего процесса и печатает значения в вывод — для проверки маски.
const sbHook = `#!/bin/sh
set -eu
d="$CREAM_STATE_DIR"; mkdir -p "$d"
h=$(basename "$0")
n=0; [ -f "$d/$h.count" ] && n=$(cat "$d/$h.count"); echo $((n + 1)) > "$d/$h.count"
sum() { if [ -n "$1" ]; then printf %s "$2" | sha256sum | cut -d' ' -f1; else echo unset; fi; }
sum "${MIRROR_TOKEN+x}" "${MIRROR_TOKEN-}" > "$d/$h.token"
sum "${MIRROR_URL+x}" "${MIRROR_URL-}" > "$d/$h.url"
sum "${REQ_TOKEN+x}" "${REQ_TOKEN-}" > "$d/$h.req"
tr '\0' ' ' < /proc/$$/cmdline > "$d/$h.argv"
echo "token=${MIRROR_TOKEN-} url=${MIRROR_URL-} req=${REQ_TOKEN-}"
`

// sbExt пишет во временный каталог расширение build-token версии version с
// хуками install, upgrade, configure и ключами сборки (extra — ещё строки
// config.keys).
func sbExt(t *testing.T, version, extra string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "build-token")
	y := `schema: cream/v1
name: build-token
version: ` + version + `
summary: фикстура scope build
cream: ">=1.2.0"
platform: {family: deb, arch: [amd64, arm64]}
hooks:
  install: {}
  upgrade: {}
  configure: {stage: system}
config:
  keys:
    MIRROR_TOKEN: {type: str, default: "", empty: true, secret: file, scope: build, description: токен зеркала}
    MIRROR_URL: {type: str, default: "http://default.invalid", scope: build, description: адрес зеркала}
    LEVEL: {type: str, default: "a", description: обычная настройка}
` + extra
	if err := os.MkdirAll(filepath.Join(dir, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extension.yaml"), []byte(y), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"install", "upgrade", "configure"} {
		if err := os.WriteFile(filepath.Join(dir, "hooks", h), []byte(sbHook), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// sbEngine — движок cream 1.2.0 на «Debian 12» с источником fx, в котором
// build-token 1.0.0.
func sbEngine(t *testing.T, extra string) *testEngine {
	t.Helper()
	te := newTestEngine(t)
	te.Cream = manifest.ParseCreamBuild("1.2.0")
	writeFile(t, te, "/etc/os-release", "ID=debian\nVERSION_ID=12\n", 0o644)
	sbSync(t, te, "1.0.0", extra)
	return te
}

func sbSync(t *testing.T, te *testEngine, version, extra string) {
	t.Helper()
	installSync(t, te, installSrc{"fx", installCatalog(t, version, map[string]string{"build-token": sbExt(t, version, extra)})})
}

func sbSum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// sbState — строка состояния хука (<hook>.<what>) без перевода строки.
func sbState(t *testing.T, te *testEngine, hook, what string) string {
	t.Helper()
	return strings.TrimSpace(readState(t, te, "build-token", hook+"."+what))
}

// sbNoMarkers требует, чтобы ни один файл под корнем машины (/var/lib/cream,
// /etc/cream, /etc/image-labels.d, /run, кеш источников — всё) не содержал
// маркеров.
func sbNoMarkers(t *testing.T, te *testEngine, markers ...string) {
	t.Helper()
	n := 0
	err := filepath.WalkDir(te.Root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		n++
		for _, m := range markers {
			if strings.Contains(string(data), m) {
				t.Errorf("маркер %q в %s", m, strings.TrimPrefix(p, te.Root))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{te.L.InstalledDir(), te.L.StateDir("build-token")} {
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("проверка пустая: нет %s (%v)", dir, err)
		}
	}
	if n == 0 {
		t.Fatal("под корнем нет файлов")
	}
}

// TestInstallScopeBuildLive: live install и upgrade — значения ключей сборки в
// окружении хука, на диске их нет, вывод вымаран, configure их не видит.
func TestInstallScopeBuildLive(t *testing.T) {
	t.Parallel()
	te := sbEngine(t, "")
	r := installReq([]string{"build-token"}, "MIRROR_TOKEN=-", "MIRROR_URL="+sbURLMarker, "LEVEL=b")
	r.Params.Stdin = strings.NewReader(sbTokenMarker + "\n")
	res, err := te.Install(context.Background(), r)
	if err != nil {
		t.Fatalf("install: %v (stderr: %s)", err, te.Err.String())
	}
	if res.Status != state.StatusInstalled || !res.Changed {
		t.Fatalf("результат: %+v", res)
	}
	if got := sbState(t, te, "install", "token"); got != sbSum(sbTokenMarker) {
		t.Errorf("install: MIRROR_TOKEN в окружении — %s", got)
	}
	if got := sbState(t, te, "install", "url"); got != sbSum(sbURLMarker) {
		t.Errorf("install: MIRROR_URL в окружении — %s", got)
	}
	if n := hookCount(t, te, "build-token", "configure"); n != 1 {
		t.Fatalf("configure вызван %d раз", n)
	}
	for _, what := range []string{"token", "url"} {
		if got := sbState(t, te, "configure", what); got != "unset" {
			t.Errorf("configure видит ключ сборки (%s): %s", what, got)
		}
	}
	if argv := sbState(t, te, "install", "argv"); strings.Contains(argv, sbTokenMarker) || strings.Contains(argv, sbURLMarker) {
		t.Errorf("значение в argv хука: %q", argv)
	}
	if errOut := te.Err.String(); strings.Contains(errOut, sbTokenMarker) || !strings.Contains(errOut, "token=***") {
		t.Errorf("вывод хука не вымаран: %q", errOut)
	}
	rec := installRecord(t, te, "build-token")
	if want := state.ParamsDigest(map[string]string{"LEVEL": "b"}, nil); rec.ParamsDigest != want {
		t.Errorf("params_digest %s, ждали %s (только LEVEL)", rec.ParamsDigest, want)
	}
	if v, err := te.Get("build-token", "LEVEL", false); err != nil || v != "b" {
		t.Errorf("LEVEL = %q, %v", v, err)
	}
	sbNoMarkers(t, te, sbTokenMarker, sbURLMarker)

	// Домен не отдаёт и не принимает ключи сборки.
	for _, k := range []string{"MIRROR_TOKEN", "MIRROR_URL"} {
		_, err := te.Get("build-token", k, true)
		wantCode(t, err, cerr.ExitUsage, "config.scope_build")
		_, err = te.Set(context.Background(), "build-token", []config.Change{{Key: k, Value: "x"}}, SetOptions{})
		wantCode(t, err, cerr.ExitUsage, "config.scope_build")
	}
	rows, err := te.Show("build-token")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if strings.HasPrefix(row.Key, "MIRROR_") {
			t.Errorf("show: %+v", row)
		}
	}

	// Повтор той же версии без смены параметров установки: хук не запускается,
	// значение сборки не применено — предупреждение, не ошибка.
	if _, err := installRun(t, te, "build-token", "MIRROR_URL="+sbURLMarker); err != nil {
		t.Fatalf("повтор: %v", err)
	}
	if n := hookCount(t, te, "build-token", "install"); n != 1 {
		t.Errorf("install.count = %d после повтора", n)
	}
	if !strings.Contains(strings.Join(warningCodes(te), " "), "install.build_unused") {
		t.Errorf("предупреждения: %v", warningCodes(te))
	}

	// upgrade: новое значение — в окружении хука upgrade, на диске — ничего.
	const token2 = "build-secret-marker-2-0d3b"
	sbSync(t, te, "1.1.0", "")
	r = installReq([]string{"build-token"}, "MIRROR_TOKEN=-")
	r.Params.Stdin = strings.NewReader(token2 + "\n")
	if _, err := te.Install(context.Background(), r); err != nil {
		t.Fatalf("upgrade: %v (stderr: %s)", err, te.Err.String())
	}
	if got := sbState(t, te, "upgrade", "token"); got != sbSum(token2) {
		t.Errorf("upgrade: MIRROR_TOKEN в окружении — %s", got)
	}
	if got := sbState(t, te, "upgrade", "url"); got != sbSum("http://default.invalid") {
		t.Errorf("upgrade: MIRROR_URL — не умолчание: %s", got)
	}
	if got := sbState(t, te, "configure", "token"); got != "unset" {
		t.Errorf("configure после upgrade видит ключ сборки: %s", got)
	}
	if rec := installRecord(t, te, "build-token"); rec.Version != "1.1.0" {
		t.Errorf("версия %s", rec.Version)
	}
	if out := te.Err.String(); strings.Contains(out, token2) || strings.Count(out, "token=***") < 2 {
		t.Errorf("вывод upgrade не вымаран: %q", out)
	}
	sbNoMarkers(t, te, sbTokenMarker, sbURLMarker, token2)
}

// TestInstallScopeBuildPhaseBuild: фаза build, значения из --params-file.
func TestInstallScopeBuildPhaseBuild(t *testing.T) {
	t.Parallel()
	te := sbEngine(t, "")
	te.EnvVars["CREAM_PHASE"] = PhaseBuild
	te.systemdRunning("running")
	pf := filepath.Join(t.TempDir(), "params.env")
	if err := os.WriteFile(pf, []byte("MIRROR_TOKEN="+sbTokenMarker+"\nLEVEL=c\nMIRROR_URL="+sbURLMarker+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := InstallRequest{Refs: []string{"build-token"}, Params: config.AssignInput{ParamsFile: pf, Stdin: strings.NewReader("")}}
	res, err := te.Install(context.Background(), r)
	if err != nil {
		t.Fatalf("install: %v (stderr: %s)", err, te.Err.String())
	}
	if res.Phase != PhaseBuild || res.Status != state.StatusInstalled {
		t.Fatalf("результат: %+v", res)
	}
	if got := sbState(t, te, "install", "token"); got != sbSum(sbTokenMarker) {
		t.Errorf("MIRROR_TOKEN в окружении — %s", got)
	}
	if n := hookCount(t, te, "build-token", "configure"); n != 0 {
		t.Errorf("configure в фазе build вызван %d раз", n)
	}
	if strings.Contains(te.Err.String(), sbTokenMarker) {
		t.Error("вывод не вымаран")
	}
	if _, err := os.Stat(te.L.ImageLabels()); err != nil {
		t.Errorf("cream.json: %v", err)
	}
	if cfg := readConfig(t, te, "build-token"); !strings.Contains(cfg, "LEVEL=c") {
		t.Errorf("config: %q", cfg)
	}
	sbNoMarkers(t, te, sbTokenMarker, sbURLMarker)
}

// TestInstallScopeBuildRequired: обязательный ключ сборки без значения на
// install — 64 config.build_required до дерева; на upgrade не требуется.
func TestInstallScopeBuildRequired(t *testing.T) {
	t.Parallel()
	te := sbEngine(t, sbReq)
	_, err := installRun(t, te, "build-token")
	wantCode(t, err, cerr.ExitUsage, "config.build_required")
	for _, p := range []string{te.L.InstalledFile("build-token"), te.L.EtcDir("build-token"), te.L.ExtTree("build-token")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s существует после отказа (%v)", p, err)
		}
	}
	r := installReq([]string{"build-token"}, "REQ_TOKEN=-")
	r.Params.Stdin = strings.NewReader(sbTokenMarker + "\n")
	if _, err := te.Install(context.Background(), r); err != nil {
		t.Fatalf("install: %v", err)
	}
	if got := sbState(t, te, "install", "req"); got != sbSum(sbTokenMarker) {
		t.Errorf("REQ_TOKEN — %s", got)
	}
	sbSync(t, te, "1.1.0", sbReq)
	if _, err := installRun(t, te, "build-token"); err != nil {
		t.Fatalf("upgrade без обязательного ключа сборки: %v", err)
	}
	if got := sbState(t, te, "upgrade", "req"); got != "unset" {
		t.Errorf("upgrade: REQ_TOKEN — %s", got)
	}
	sbNoMarkers(t, te, sbTokenMarker)
}

// TestInstallScopeBuildRefusals: секрет сборки в argv — 64 secret.in_argv;
// значение не того типа — 65 config.invalid; в обоих случаях ничего не
// записано.
func TestInstallScopeBuildRefusals(t *testing.T) {
	t.Parallel()
	te := sbEngine(t, "    BUILD_JOBS: {type: int, default: \"1\", scope: build, description: потоки}\n")
	_, err := installRun(t, te, "build-token", "MIRROR_TOKEN="+sbTokenMarker)
	wantCode(t, err, cerr.ExitUsage, "secret.in_argv")
	_, err = installRun(t, te, "build-token", "BUILD_JOBS=много")
	wantCode(t, err, cerr.ExitData, "config.invalid")
	if _, err := os.Stat(te.L.InstalledFile("build-token")); !os.IsNotExist(err) {
		t.Errorf("запись реестра после отказа: %v", err)
	}
}

// TestInstallFromDirScopeBuild: устаревший путь --from-dir/--record — то же:
// значение только в окружении хука, params_digest слоя без ключей сборки.
func TestInstallFromDirScopeBuild(t *testing.T) {
	t.Parallel()
	te := buildEngine(t)
	te.Cream = manifest.ParseCreamBuild("1.2.0")
	req := buildIncoming(t, te, "sb", sbExt(t, "1.0.0", ""), "MIRROR_TOKEN="+sbTokenMarker+"\nLEVEL=c\n",
		func(l *resolve.Layer) { l.ParamsDigest = state.ParamsDigest(map[string]string{"LEVEL": "c"}, nil) })
	res, err := te.InstallFromDir(context.Background(), req)
	if err != nil {
		t.Fatalf("build: %v (stderr: %s)", err, te.Err.String())
	}
	if res.Status != state.StatusInstalled {
		t.Fatalf("результат: %+v", res)
	}
	if got := sbState(t, te, "install", "token"); got != sbSum(sbTokenMarker) {
		t.Errorf("MIRROR_TOKEN в окружении — %s", got)
	}
	if strings.Contains(te.Err.String(), sbTokenMarker) {
		t.Error("вывод не вымаран")
	}
	sbNoMarkers(t, te, sbTokenMarker)
}

const sbReq = "    REQ_TOKEN: {type: str, secret: file, scope: build, description: обязательный токен}\n"

// sbInstallReq ставит build-token с REQ_TOKEN со stdin (token == "" — без него).
func sbInstallReq(t *testing.T, te *testEngine, token string, reinstall bool) error {
	t.Helper()
	var args []string
	if token != "" {
		args = []string{"REQ_TOKEN=-"}
	}
	r := installReq([]string{"build-token"}, args...)
	r.Params.Stdin = strings.NewReader(token + "\n")
	r.Reinstall = reinstall
	_, err := te.Install(context.Background(), r)
	return err
}

// TestInstallScopeBuildReinstall: --reinstall запускает хук install заново —
// обязательный ключ сборки снова обязателен, новое значение в окружении.
func TestInstallScopeBuildReinstall(t *testing.T) {
	t.Parallel()
	te := sbEngine(t, sbReq)
	if err := sbInstallReq(t, te, sbTokenMarker, false); err != nil {
		t.Fatal(err)
	}
	wantCode(t, sbInstallReq(t, te, "", true), cerr.ExitUsage, "config.build_required")
	const token2 = "build-secret-marker-re-77aa"
	if err := sbInstallReq(t, te, token2, true); err != nil {
		t.Fatalf("--reinstall: %v", err)
	}
	if n := hookCount(t, te, "build-token", "install"); n != 2 {
		t.Errorf("install.count = %d", n)
	}
	if got := sbState(t, te, "install", "req"); got != sbSum(token2) {
		t.Errorf("REQ_TOKEN после --reinstall — %s", got)
	}
	if !strings.Contains(te.Err.String(), "req=***") || strings.Contains(te.Err.String(), token2) {
		t.Errorf("вывод не вымаран: %q", te.Err.String())
	}
	sbNoMarkers(t, te, sbTokenMarker, token2)
}

// sbFailed — build-token 1.0.0 в реестре со статусом failed (дерево — из
// того же расширения, что в источнике).
func sbFailed(t *testing.T) *testEngine {
	t.Helper()
	te := newTestEngine(t)
	te.Cream = manifest.ParseCreamBuild("1.2.0")
	writeFile(t, te, "/etc/os-release", "ID=debian\nVERSION_ID=12\n", 0o644)
	src := sbExt(t, "1.0.0", sbReq)
	installSync(t, te, installSrc{"fx", installCatalog(t, "1.0.0", map[string]string{"build-token": src})})
	seedInstalled(t, te, src, func(r *state.Record) {
		r.Status, r.Source = state.StatusFailed, state.SourceRef{Name: "fx", Type: "path"}
	})
	return te
}

// TestInstallScopeBuildFailedRetry: повтор install после неудачного хука —
// хук запускается снова, обязательный ключ сборки нужен.
func TestInstallScopeBuildFailedRetry(t *testing.T) {
	t.Parallel()
	te := sbFailed(t)
	wantCode(t, sbInstallReq(t, te, "", false), cerr.ExitUsage, "config.build_required")
	if err := sbInstallReq(t, te, sbTokenMarker, false); err != nil {
		t.Fatalf("повтор: %v", err)
	}
	if rec := installRecord(t, te, "build-token"); rec.Status != state.StatusInstalled {
		t.Errorf("статус %s", rec.Status)
	}
	if got := sbState(t, te, "install", "req"); got != sbSum(sbTokenMarker) {
		t.Errorf("REQ_TOKEN — %s", got)
	}
	sbNoMarkers(t, te, sbTokenMarker)
}

// TestUpgradeScopeBuild: cream upgrade не принимает параметров — хук upgrade
// видит только умолчания ключей сборки, на диске ничего; повтор failed
// install через cream upgrade с обязательным ключом сборки — отказ элемента
// 64 config.build_required (значение через upgrade не передать; повтор —
// cream install с ключом).
func TestUpgradeScopeBuild(t *testing.T) {
	t.Parallel()
	te := sbEngine(t, sbReq)
	if err := sbInstallReq(t, te, sbTokenMarker, false); err != nil {
		t.Fatal(err)
	}
	sbSync(t, te, "1.1.0", sbReq)
	rep, err := te.Upgrade(context.Background(), UpgradeRequest{Names: []string{"build-token"}})
	if err != nil {
		t.Fatalf("upgrade: %v (stderr: %s)", err, te.Err.String())
	}
	if rec := installRecord(t, te, "build-token"); rec.Version != "1.1.0" || rec.Status != state.StatusInstalled {
		t.Fatalf("запись: %+v (%+v)", rec, rep)
	}
	if got := sbState(t, te, "upgrade", "token"); got != sbSum("") {
		t.Errorf("MIRROR_TOKEN — не пустое умолчание: %s", got)
	}
	if got := sbState(t, te, "upgrade", "url"); got != sbSum("http://default.invalid") {
		t.Errorf("MIRROR_URL — не умолчание: %s", got)
	}
	if got := sbState(t, te, "upgrade", "req"); got != "unset" {
		t.Errorf("REQ_TOKEN — %s", got)
	}
	sbNoMarkers(t, te, sbTokenMarker)

	tf := sbFailed(t)
	rep, err = tf.Upgrade(context.Background(), UpgradeRequest{Names: []string{"build-token"}})
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if len(rep.Plan) != 1 || rep.Plan[0].Status != StatusFailed || rep.Plan[0].Error == nil ||
		rep.Plan[0].Error.Code != "config.build_required" || rep.Summary.ExitCode != cerr.ExitUsage {
		t.Errorf("план: %+v", rep)
	}
}
