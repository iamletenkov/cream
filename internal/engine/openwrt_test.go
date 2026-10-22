package engine

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/config"
	"github.com/iamletenkov/cream/internal/lock"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/paths"
	"github.com/iamletenkov/cream/internal/semver"
	"github.com/iamletenkov/cream/internal/state"
)

// openwrtEngine — тестовый движок на «OpenWrt 25.12» (ADR-0005): раскладка
// openwrt (/opt/cream/var), cream 1.3.0, источник testdata/openwrt.
func openwrtEngine(t *testing.T) *testEngine {
	t.Helper()
	te := newTestEngine(t)
	te.L = paths.Layout{Root: te.Root, Family: paths.FamilyOpenWrt}
	te.Store = state.NewStore(te.L, te.Now)
	te.Locks = lock.NewLocker(te.L, testLockTimeout, te.Env)
	te.Cream = manifest.CreamBuild{Version: semver.Version{Major: 1, Minor: 3}}
	writeFile(t, te, "/etc/os-release", "ID=\"openwrt\"\nID_LIKE=\"lede openwrt\"\nVERSION_ID=\"25.12.6\"\n", 0o644)
	installSync(t, te, installSrc{"openwrt", testdataDir(t, "openwrt")})
	return te
}

func TestDetectPhaseOpenWrt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		comm string
		run  bool
		want PhaseInfo
	}{
		{"procd", false, PhaseInfo{Phase: PhaseLive, Reason: "pid1=procd"}},
		{"init", false, PhaseInfo{Phase: PhaseBuild, Reason: "pid1=init"}},
		// chroot образа OpenWrt на хосте с systemd: /proc хоста — сборка.
		{"systemd", true, PhaseInfo{Phase: PhaseBuild, Reason: "pid1=systemd"}},
	} {
		root := t.TempDir()
		fakePID1(t, root, tc.comm, tc.run)
		got, err := DetectPhase(paths.Layout{Root: root, Family: paths.FamilyOpenWrt})
		if err != nil || got != tc.want {
			t.Errorf("%s: DetectPhase = %+v, %v; want %+v", tc.comm, got, err, tc.want)
		}
	}
}

func TestOpenWrtSystemInit(t *testing.T) {
	t.Parallel()
	te := openwrtEngine(t)
	res, err := te.SystemInit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Unit != "cream-configure" || res.DaemonReload {
		t.Errorf("результат: %+v", res)
	}
	for _, p := range []string{te.L.InitScript("cream-configure"), te.L.InitScript("cream-configure-early"),
		te.L.RcLink("S95cream-configure"), te.L.RcLink("S19cream-configure-early"), te.L.HookTmp(), te.L.InstalledDir()} {
		if !installExists(p) {
			t.Errorf("нет %s", p)
		}
	}
	// Ничего от systemd и ничего в /var/lib (tmpfs на OpenWrt).
	for _, p := range []string{te.L.UnitTemplate(), filepath.Join(te.Root, "var/lib/cream")} {
		if installExists(p) {
			t.Errorf("лишнее %s", p)
		}
	}
}

func TestOpenWrtInstallLiveConfigureRemove(t *testing.T) {
	t.Parallel()
	te := openwrtEngine(t)
	if _, err := te.ResolvePhase(PhaseLive); err != nil {
		t.Fatal(err)
	}
	installMust(t, te, "fixture-openwrt")
	rec := installRecord(t, te, "fixture-openwrt")
	if rec.Configure == nil || rec.Configure.Stage != "system" || rec.Configure.Target != "S95cream-configure" {
		t.Fatalf("configure в реестре: %+v", rec.Configure)
	}
	if !strings.HasPrefix(te.L.InstalledFile("fixture-openwrt"), filepath.Join(te.Root, "opt/cream/var")) ||
		!installExists(te.L.InstalledFile("fixture-openwrt")) {
		t.Errorf("реестр не в /opt/cream/var: %s", te.L.InstalledFile("fixture-openwrt"))
	}
	if !installExists(te.L.StageMark("system", "fixture-openwrt")) || !installExists(te.L.InitScript("cream-configure")) {
		t.Error("нет отметки стадии или init-скрипта")
	}
	// systemd нет: configure с причиной install прошёл напрямую сразу после install.
	if n := hookCount(t, te, "fixture-openwrt", "configure"); n != 1 {
		t.Fatalf("configure.count = %d (stderr: %s)", n, te.Err.String())
	}
	tmp := te.L.HookTmp()
	for _, h := range []string{"install", "configure"} {
		log := readState(t, te, "fixture-openwrt", h+".log")
		if !strings.Contains(log, "phase=live ") || !strings.Contains(log, "tmpdir="+tmp+" ") ||
			!strings.Contains(log, "instroot=unset ") {
			t.Errorf("%s.log: %q", h, log)
		}
	}
	if installExists(te.L.PolicyRcD()) {
		t.Error("policy-rc.d на OpenWrt")
	}
	// Загрузка: every_boot — хук идёт и при совпавшей отметке.
	if _, err := te.ConfigureReport(context.Background(), "fixture-openwrt", "", ConfigureOptions{FromUnit: true}); err != nil {
		t.Fatal(err)
	}
	if n := hookCount(t, te, "fixture-openwrt", "configure"); n != 2 {
		t.Errorf("configure.count после загрузки = %d", n)
	}
	if !strings.Contains(readState(t, te, "fixture-openwrt", "configure.log"), "reason=boot ") {
		t.Errorf("configure.log: %s", readState(t, te, "fixture-openwrt", "configure.log"))
	}
	if list, err := te.List(); err != nil || len(list) != 1 || list[0].Apply != ApplyHook {
		t.Errorf("cream list: %+v, %v", list, err)
	}
	// cream set на OpenWrt: юнита нет — configure (apply) идёт сразу.
	sres, err := te.Set(context.Background(), "fixture-openwrt", []config.Change{{Key: "MESSAGE", Value: "мир"}}, SetOptions{})
	if err != nil || sres.Apply != ApplyHook {
		t.Fatalf("set: %+v, %v", sres, err)
	}
	if got := strings.TrimSpace(readState(t, te, "fixture-openwrt", "message")); got != "мир" ||
		!strings.Contains(readState(t, te, "fixture-openwrt", "configure.log"), "reason=apply ") {
		t.Errorf("после set: message=%q", got)
	}
	if _, err := te.Remove(context.Background(), "fixture-openwrt", RemoveOptions{Purge: true}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{te.L.StageMark("system", "fixture-openwrt"), te.L.InstalledFile("fixture-openwrt"),
		te.L.ExtTree("fixture-openwrt"), te.L.StateDir("fixture-openwrt")} {
		if _, err := os.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("после remove остался %s", p)
		}
	}
}

func TestOpenWrtEarlyBootSkipsByMark(t *testing.T) {
	t.Parallel()
	te := openwrtEngine(t)
	if _, err := te.ResolvePhase(PhaseLive); err != nil {
		t.Fatal(err)
	}
	installMust(t, te, "fixture-openwrt-early")
	if rec := installRecord(t, te, "fixture-openwrt-early"); rec.Configure == nil || rec.Configure.Target != "S19cream-configure-early" {
		t.Fatalf("configure: %+v", rec.Configure)
	}
	res, err := te.ConfigureReport(context.Background(), "fixture-openwrt-early", "", ConfigureOptions{FromUnit: true})
	if err != nil || res.Mode != ConfigureUpToDate {
		t.Fatalf("загрузка: %+v, %v", res, err)
	}
	if n := hookCount(t, te, "fixture-openwrt-early", "configure"); n != 1 {
		t.Errorf("configure.count = %d", n)
	}
}

func TestOpenWrtBuildPhase(t *testing.T) {
	t.Parallel()
	te := openwrtEngine(t)
	fakePID1(t, te.Root, "init", false)
	writeFile(t, te, "/etc/apk/config", "own-option\n", 0o644)
	p, err := te.ResolvePhase("")
	if err != nil || p.Phase != PhaseBuild {
		t.Fatalf("фаза: %+v, %v", p, err)
	}
	installMust(t, te, "fixture-openwrt")
	log := readState(t, te, "fixture-openwrt", "install.log")
	if !strings.Contains(log, "phase=build ") || !strings.Contains(log, "instroot=/ ") ||
		!strings.Contains(log, "tmpdir="+te.L.HookTmp()+" ") {
		t.Errorf("install.log: %q", log)
	}
	// apk 3 передаёт IPKG_INSTROOT скриптам пакетов только с preserve-env:
	// APK_CONFIG хука — копия /etc/apk/config образа плюс preserve-env.
	conf, err := os.ReadFile(te.L.ApkBuildConfig())
	if err != nil || !strings.Contains(string(conf), "own-option\n") || !strings.Contains(string(conf), "\npreserve-env\n") {
		t.Errorf("apk-build.conf: %q, %v", conf, err)
	}
	// configure в build только включается; policy-rc.d не ставится.
	if n := hookCount(t, te, "fixture-openwrt", "configure"); n != 0 {
		t.Errorf("configure.count = %d в build", n)
	}
	if !installExists(te.L.StageMark("system", "fixture-openwrt")) || installExists(te.L.PolicyRcD()) {
		t.Error("отметка стадии или policy-rc.d")
	}
}

func TestOpenWrtRefusesDebOnly(t *testing.T) {
	t.Parallel()
	te := openwrtEngine(t)
	if _, err := te.ResolvePhase(PhaseLive); err != nil {
		t.Fatal(err)
	}
	installSync(t, te, installSrc{"fixtures", testdataDir(t, "catalog")})
	_, err := installRun(t, te, "noop")
	wantCode(t, err, cerr.ExitNotApplicable, "platform.family")
}

// apkBuildConfig читает /etc/apk/config образа внутри корня: ссылка наружу
// (на файл хоста) не читается и в образ не попадает.
func TestApkBuildConfigStaysInRoot(t *testing.T) {
	t.Parallel()
	te := openwrtEngine(t)
	host := filepath.Join(t.TempDir(), "host-secret")
	if err := os.WriteFile(host, []byte("HOST-SECRET\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(te.Root, "etc/apk"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(host, filepath.Join(te.Root, "etc/apk/config")); err != nil {
		t.Fatal(err)
	}
	_, err := te.apkBuildConfig()
	if err == nil {
		if conf, _ := os.ReadFile(te.L.ApkBuildConfig()); strings.Contains(string(conf), "HOST-SECRET") {
			t.Fatalf("файл хоста в образе: %q", conf)
		}
		return
	}
	wantCode(t, err, cerr.ExitState, cerr.CodeIORead)
	if conf, _ := os.ReadFile(te.L.ApkBuildConfig()); strings.Contains(string(conf), "HOST-SECRET") {
		t.Fatalf("файл хоста в образе: %q", conf)
	}
}

// prepareTmp: без прав на каталог (cream run не от root) TMPDIR не ставится, а
// не роняет запуск; готовый каталог с верным режимом не трогается.
func TestPrepareTmp(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	ok := filepath.Join(base, "ok")
	if !prepareTmp(ok) {
		t.Fatal("prepareTmp(ok) = false")
	}
	if st, _ := os.Stat(ok); st.Mode() != os.ModeDir|os.ModeSticky|0o777 {
		t.Errorf("mode %v", st.Mode())
	}
	if !prepareTmp(ok) {
		t.Error("повтор: false")
	}
	ro := filepath.Join(base, "ro")
	if err := os.Mkdir(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() != 0 && prepareTmp(filepath.Join(ro, "tmp")) {
		t.Error("prepareTmp в каталоге без записи = true")
	}
}
