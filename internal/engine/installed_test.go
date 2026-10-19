package engine

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/config"
	"github.com/iamletenkov/cream/internal/lock"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/state"
	"github.com/iamletenkov/cream/internal/systemd"
)

func TestInstalledAndList(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "with-config"))
	seedInstalled(t, te, fixtureDir(t, "apply-hook"), func(r *state.Record) {
		r.Status, r.RebootRequired = state.StatusFailed, true
	})
	seedInstalled(t, te, fixtureDir(t, "configure-system"))
	seedInstalled(t, te, fixtureDir(t, "action-json"))
	writeFile(t, te, "/var/lib/cream/installed/broken.json", "{", 0o644)

	all, err := te.Installed()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, in := range all {
		names = append(names, in.Record.Name)
		if in.Manifest == nil {
			t.Errorf("%s без манифеста", in.Record.Name)
		}
	}
	if !slices.Equal(names, []string{"action-json", "apply-hook", "configure-system", "with-config"}) {
		t.Errorf("names = %v", names)
	}
	if !slices.Contains(warningCodes(te), "state.corrupt") {
		t.Errorf("нет state.corrupt: %v", warningCodes(te))
	}

	list, err := te.List()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]ListEntry{}
	for _, le := range list {
		byName[le.Name] = le
	}
	if le := byName["with-config"]; !le.Domain || !le.Secret || le.Apply != ApplyNone {
		t.Errorf("with-config: %+v", le)
	}
	if le := byName["apply-hook"]; le.Apply != ApplyHook || le.Secret || le.Status != state.StatusFailed || !le.RebootRequired {
		t.Errorf("apply-hook: %+v", le)
	}
	if le := byName["configure-system"]; le.Apply != ApplyRestart {
		t.Errorf("configure-system: %+v", le)
	}
	if le := byName["action-json"]; len(le.Actions) == 0 || !slices.IsSorted(le.Actions) {
		t.Errorf("action-json: %+v", le)
	}
}

func TestInstalledTreeMissing(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "noop"))
	if err := os.RemoveAll(te.L.ExtTree("noop")); err != nil {
		t.Fatal(err)
	}
	all, err := te.Installed()
	if err != nil || len(all) != 1 || all[0].Manifest != nil {
		t.Fatalf("%+v, %v", all, err)
	}
	if !slices.Contains(warningCodes(te), "state.tree") {
		t.Errorf("предупреждения %v", warningCodes(te))
	}
	if _, err := te.Find("noop"); err == nil {
		t.Error("Find без дерева без ошибки")
	}
}

func TestShowGetExport(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	in := seedInstalled(t, te, fixtureDir(t, "with-config"))
	if _, err := config.Open(te.L, "with-config", in.Manifest.Config).Set([]config.Change{
		{Key: "COUNT", Value: "7"}, {Key: "TOKEN", Value: "tok"}, {Key: "LBL_ROLE", Value: "edge"},
	}, config.SetOpts{}); err != nil {
		t.Fatal(err)
	}
	rows, err := te.Show("with-config")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]ShowEntry{}
	for _, r := range rows {
		got[r.Key] = r
	}
	if r := got["COUNT"]; r.Value != "7" || r.Origin != config.OriginFile || r.Type != "int" {
		t.Errorf("COUNT = %+v", r)
	}
	if r := got["MODE"]; r.Value != "fast" || r.Origin != config.OriginDefault {
		t.Errorf("MODE = %+v", r)
	}
	if r := got["TOKEN"]; !r.Secret || r.Value != config.MaskSet {
		t.Errorf("TOKEN = %+v", r)
	}
	if _, ok := got["LBL_ROLE"]; !ok {
		t.Error("нет LBL_ROLE")
	}

	if v, err := te.Get("with-config", "COUNT", false); err != nil || v != "7" {
		t.Errorf("get COUNT = %q, %v", v, err)
	}
	_, err = te.Get("with-config", "TOKEN", false)
	wantCode(t, err, cerr.ExitUsage, "usage.secret")
	if v, err := te.Get("with-config", "TOKEN", true); err != nil || v != "tok" {
		t.Errorf("get --secret TOKEN = %q, %v", v, err)
	}
	_, err = te.Get("with-config", "NOPE", false)
	wantCode(t, err, cerr.ExitNotFound, "not_found.key")

	sh, err := te.ExportShell("with-config")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sh, "COUNT='7'\n") || strings.Contains(sh, "tok") {
		t.Errorf("export:\n%s", sh)
	}
	_, err = te.Show("noop")
	wantCode(t, err, cerr.ExitNotFound, "not_found.extension")
}

func TestSystemInit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		state      string
		wantReload bool
	}{
		{name: "offline"},
		{name: "running", state: "running", wantReload: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			te := newTestEngine(t)
			if tc.state != "" {
				te.systemdRunning(tc.state)
			}
			res, err := te.SystemInit(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if res.DaemonReload != tc.wantReload {
				t.Errorf("res = %+v", res)
			}
			if _, err := os.Stat(te.L.UnitTemplate()); err != nil {
				t.Errorf("шаблон юнита: %v", err)
			}
			st, err := os.Stat(te.L.LockDir())
			if err != nil || st.Mode().Perm() != 0o700 {
				t.Errorf("lock dir: %v %v", st, err)
			}
			if got := slices.Contains(te.Sys.Calls(), "daemon-reload"); got != tc.wantReload {
				t.Errorf("calls %v", te.Sys.Calls())
			}
		})
	}
}

// system init переносит экземпляр cloud-user из цели 1.1.0 к вспомогательному
// юниту (cream 1.1.1) и обновляет configure.target в реестре под lock/global;
// прочие записи не трогает.
func TestSystemInitUpdatesConfigureTarget(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	te.Cream = manifest.ParseCreamBuild("1.1.0")
	seedInstalled(t, te, fixtureDir(t, "configure-cloud-user"), func(r *state.Record) { r.Configure.Target = "cloud-init.target" })
	seedInstalled(t, te, fixtureDir(t, "configure-system"))
	for ext, stage := range map[string]string{"configure-cloud-user": "cloud-user", "configure-system": "system"} {
		if _, err := systemd.Enable(te.L, ext, systemd.StageSpec{Stage: stage}); err != nil {
			t.Fatal(err)
		}
	}
	before, err := os.ReadFile(te.L.InstalledFile("configure-system"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := te.SystemInit(context.Background()); err != nil {
		t.Fatal(err)
	}
	rec, ok, err := te.Store.Get("configure-cloud-user")
	if err != nil || !ok || rec.Configure == nil || rec.Configure.Target != systemd.CloudUserUnit {
		t.Fatalf("реестр cloud-user: %+v %v %v", rec, ok, err)
	}
	link := filepath.Join(te.L.WantsDir(systemd.CloudUserUnit), systemd.UnitName("configure-cloud-user"))
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("ссылка: %v", err)
	}
	if after, _ := os.ReadFile(te.L.InstalledFile("configure-system")); !bytes.Equal(after, before) {
		t.Errorf("запись system переписана:\n%s", after)
	}
	if len(te.Warn.List()) != 0 {
		t.Errorf("предупреждения %v", warningCodes(te))
	}
}

// cream 1.1.1: system init (postinst пакета под set -e) не ждёт lock/global,
// когда реестр менять не нужно, а при занятой блокировке и сменившейся цели
// пропускает запись реестра с предупреждением — ссылки уже пересчитаны.
func TestSystemInitGlobalLockBusy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		target     string // configure.target записи cloud-user до system init
		wantTarget string
		wantWarn   bool
	}{
		{"цель не меняется", systemd.CloudUserUnit, systemd.CloudUserUnit, false},
		{"цель меняется", "cloud-init.target", "cloud-init.target", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			te := newTestEngine(t)
			te.Cream = manifest.ParseCreamBuild("1.1.0")
			seedInstalled(t, te, fixtureDir(t, "configure-cloud-user"), func(r *state.Record) { r.Configure.Target = tc.target })
			if _, err := systemd.Enable(te.L, "configure-cloud-user", systemd.StageSpec{Stage: "cloud-user"}); err != nil {
				t.Fatal(err)
			}
			g, err := lock.NewLocker(te.L, 0, nil).Global(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer g.Release()
			start := time.Now()
			if _, err := te.SystemInit(context.Background()); err != nil {
				t.Fatalf("system init: %v", err)
			}
			if d := time.Since(start); d > testLockTimeout/2 {
				t.Errorf("system init ждал блокировку %s", d)
			}
			rec, ok, err := te.Store.Get("configure-cloud-user")
			if err != nil || !ok || rec.Configure.Target != tc.wantTarget {
				t.Fatalf("реестр: %+v %v %v", rec, ok, err)
			}
			if got := slices.Contains(warningCodes(te), "system.registry_busy"); got != tc.wantWarn {
				t.Errorf("предупреждения %v", warningCodes(te))
			}
			link := filepath.Join(te.L.WantsDir(systemd.CloudUserUnit), systemd.UnitName("configure-cloud-user"))
			if _, err := os.Lstat(link); err != nil {
				t.Errorf("ссылка: %v", err)
			}
		})
	}
}

// Запись, снятая remove, пока system init ждал lock/global, не воскресает.
func TestSystemInitSkipsRemovedRecord(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	te.Cream = manifest.ParseCreamBuild("1.1.0")
	seedInstalled(t, te, fixtureDir(t, "configure-cloud-user"), func(r *state.Record) { r.Configure.Target = "cloud-init.target" })
	recs, err := te.Store.List(te.Warn)
	if err != nil {
		t.Fatal(err)
	}
	if err := te.Store.Delete("configure-cloud-user"); err != nil {
		t.Fatal(err)
	}
	if err := te.applyTargets(recs); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := te.Store.Get("configure-cloud-user"); ok {
		t.Error("снятая запись вернулась в реестр")
	}
}

func TestSystemInitReloadFailureWarns(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	te.systemdRunning("running")
	te.Sys.ReloadErr = os.ErrClosed
	res, err := te.SystemInit(context.Background())
	if err != nil || res.DaemonReload {
		t.Fatalf("%+v, %v", res, err)
	}
	if !slices.Contains(warningCodes(te), "systemd.reload_failed") {
		t.Errorf("предупреждения %v", warningCodes(te))
	}
	if _, err := os.Stat(filepath.Join(te.Root, "var/lib/cream/installed")); err != nil {
		t.Error(err)
	}
}
