package engine

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/state"
)

// planExt — синтетическое расширение каталога для тестов плана обновления.
type planExt struct {
	name, version string
	// extra — дополнительные строки манифеста (depends, targets…).
	extra string
	// fail — хук, который завершается 1 ("" — все хуки успешны).
	fail string
}

// planCatalog пишет каталог из exts: у каждого хуки install, upgrade, remove;
// install и upgrade дописывают имя расширения в $CREAM_ROOT/order.log.
func planCatalog(t *testing.T, exts ...planExt) string {
	t.Helper()
	dir := t.TempDir()
	body := "schema: cream/v1\nname: cream-fixtures\nversion: 1.0.0\nroots: [extensions]\n"
	if err := os.WriteFile(filepath.Join(dir, "cream-catalog.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, x := range exts {
		mf := "schema: cream/v1\nname: " + x.name + "\nversion: " + x.version + "\nsummary: фикстура " + x.name +
			"\ncream: \">=1.0.0\"\nplatform: {family: deb, arch: [amd64, arm64]}\n" + x.extra +
			"hooks:\n  install: {}\n  upgrade: {}\n  remove: {}\n"
		catalogWrite(t, dir, x.name, "extension.yaml", mf)
		for _, h := range []string{"install", "upgrade", "remove"} {
			script := "#!/bin/sh\nset -eu\n"
			if h != "remove" {
				script += "echo " + x.name + "@" + x.version + " >> \"$CREAM_ROOT/order.log\"\n"
			}
			if h == x.fail {
				script += "exit 1\n"
			}
			catalogWrite(t, dir, x.name, "hooks/"+h, script)
		}
	}
	return dir
}

// planSetup ставит first (в порядке аргументов) и переключает источник
// fixtures на next.
func planSetup(t *testing.T, first, next []planExt) *testEngine {
	t.Helper()
	te := installEngine(t, installSrc{"fixtures", planCatalog(t, first...)})
	for _, x := range first {
		if _, err := te.Install(context.Background(), InstallRequest{Refs: []string{x.name}, AcceptRisk: true}); err != nil {
			t.Fatalf("install %s: %v (stderr %s)", x.name, err, te.Err.String())
		}
	}
	installSync(t, te, installSrc{"fixtures", planCatalog(t, next...)})
	if err := os.Remove(filepath.Join(te.Root, "order.log")); err != nil {
		t.Fatal(err)
	}
	return te
}

func planOrder(t *testing.T, te *testEngine) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(te.Root, "order.log"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(data))
}

// planItem — пункт плана по имени.
func planItem(t *testing.T, rep *UpgradeReport, name string) UpgradeItem {
	t.Helper()
	for _, it := range rep.Plan {
		if it.Name == name {
			return it
		}
	}
	t.Fatalf("в плане нет %s: %+v", name, rep.Plan)
	return UpgradeItem{}
}

func wantItem(t *testing.T, rep *UpgradeReport, name, action, reason, status string) UpgradeItem {
	t.Helper()
	it := planItem(t, rep, name)
	if it.Action != action || it.Reason != reason || it.Status != status {
		t.Errorf("%s: action=%q reason=%q status=%q, ждали %q %q %q (error %+v)",
			name, it.Action, it.Reason, it.Status, action, reason, status, it.Error)
	}
	return it
}

func upgradeAll(t *testing.T, te *testEngine, r UpgradeRequest) *UpgradeReport {
	t.Helper()
	r.All = true
	rep, err := te.Upgrade(context.Background(), r)
	if err != nil {
		t.Fatalf("upgrade --all: %v (stderr %s)", err, te.Err.String())
	}
	return rep
}

var confirmExt = "targets: {build: exec, live: confirm, live_note: \"межсетевой экран: можно потерять доступ\"}\n"

// Ошибка из отчёта владельца: расширение с live: confirm без новой версии не
// мешает upgrade --all — оно up-to-date, проверки риска нет, код 0.
func TestUpgradeAllConfirmUpToDate(t *testing.T) {
	t.Parallel()
	fw := planExt{name: "firewall", version: "1.0.0", extra: confirmExt}
	te := planSetup(t, []planExt{fw, {name: "agent", version: "1.0.0"}},
		[]planExt{fw, {name: "agent", version: "1.1.0"}})
	rep := upgradeAll(t, te, UpgradeRequest{})
	wantItem(t, rep, "firewall", ActionUpToDate, "", StatusUnchanged)
	it := wantItem(t, rep, "agent", ActionUpgrade, "", StatusUpgraded)
	if it.From != "1.0.0" || it.To != "1.1.0" {
		t.Errorf("agent: %s → %s", it.From, it.To)
	}
	if rep.Summary.ExitCode != 0 || rep.Err() != nil {
		t.Errorf("код %d, err %v", rep.Summary.ExitCode, rep.Err())
	}
	if s := rep.Summary; s.Upgraded != 1 || s.Unchanged != 1 || s.Skipped != 0 || s.Failed != 0 {
		t.Errorf("сводка %+v", s)
	}
	if got := planOrder(t, te); !slices.Equal(got, []string{"agent@1.1.0"}) {
		t.Errorf("хуки: %v", got)
	}
}

func TestUpgradeRiskBlocked(t *testing.T) {
	t.Parallel()
	first := []planExt{{name: "firewall", version: "1.0.0", extra: confirmExt}, {name: "agent", version: "1.0.0"}}
	next := []planExt{{name: "firewall", version: "1.1.0", extra: confirmExt}, {name: "agent", version: "1.1.0"}}

	t.Run("без --accept-risk — пропуск, остальные обновлены, код 68", func(t *testing.T) {
		t.Parallel()
		te := planSetup(t, first, next)
		rep := upgradeAll(t, te, UpgradeRequest{})
		it := wantItem(t, rep, "firewall", ActionBlocked, ReasonRisk, StatusBlocked)
		if it.Error == nil || it.Error.Code != "target.confirm" || it.To != "1.1.0" {
			t.Errorf("firewall: %+v", it)
		}
		wantItem(t, rep, "agent", ActionUpgrade, "", StatusUpgraded)
		if rep.Summary.ExitCode != cerr.ExitNotApplicable || rep.Summary.Skipped != 1 || rep.Summary.Upgraded != 1 {
			t.Errorf("сводка %+v", rep.Summary)
		}
		if rec := installRecord(t, te, "firewall"); rec.Version != "1.0.0" {
			t.Errorf("firewall обновлён: %s", rec.Version)
		}
	})
	for _, accept := range [][]string{{"firewall"}, {"all"}} {
		t.Run("--accept-risk "+accept[0], func(t *testing.T) {
			t.Parallel()
			te := planSetup(t, first, next)
			rep := upgradeAll(t, te, UpgradeRequest{AcceptRisk: accept})
			wantItem(t, rep, "firewall", ActionUpgrade, "", StatusUpgraded)
			if rep.Summary.ExitCode != 0 || rep.Summary.Upgraded != 2 {
				t.Errorf("сводка %+v", rep.Summary)
			}
		})
	}
	t.Run("--accept-risk другого имени не снимает риск", func(t *testing.T) {
		t.Parallel()
		te := planSetup(t, first, next)
		rep := upgradeAll(t, te, UpgradeRequest{AcceptRisk: []string{"agent"}})
		wantItem(t, rep, "firewall", ActionBlocked, ReasonRisk, StatusBlocked)
	})
	t.Run("вопрос в TTY", func(t *testing.T) {
		t.Parallel()
		for _, yes := range []bool{true, false} {
			te := planSetup(t, first, next)
			var asked []string
			rep := upgradeAll(t, te, UpgradeRequest{ConfirmRisk: func(name, note string) (bool, error) {
				asked = append(asked, name+": "+note)
				return yes, nil
			}})
			if !slices.Equal(asked, []string{"firewall: межсетевой экран: можно потерять доступ"}) {
				t.Errorf("вопросы: %v", asked)
			}
			if yes {
				wantItem(t, rep, "firewall", ActionUpgrade, "", StatusUpgraded)
			} else {
				wantItem(t, rep, "firewall", ActionBlocked, ReasonRisk, StatusBlocked)
			}
		}
	})
	t.Run("поимённо — ошибка 68 target.confirm", func(t *testing.T) {
		t.Parallel()
		te := planSetup(t, first, next)
		rep, err := te.Upgrade(context.Background(), UpgradeRequest{Names: []string{"firewall"}})
		if err != nil {
			t.Fatal(err)
		}
		wantCode(t, rep.Err(), cerr.ExitNotApplicable, "target.confirm")
	})
}

func TestUpgradeHold(t *testing.T) {
	t.Parallel()
	first := []planExt{{name: "agent", version: "1.0.0"}, {name: "pinned", version: "1.0.0"}}
	next := []planExt{{name: "agent", version: "1.1.0"}, {name: "pinned", version: "2.0.0"}}
	te := planSetup(t, first, next)
	ctx := context.Background()

	if _, err := te.Hold(ctx, []string{"missing"}, true); err == nil {
		t.Fatal("hold неустановленного — без ошибки")
	} else {
		wantCode(t, err, cerr.ExitNotFound, "not_found.extension")
	}
	res, err := te.Hold(ctx, []string{"pinned"}, true)
	if err != nil || len(res) != 1 || !res[0].Changed || !res[0].Held {
		t.Fatalf("hold: %+v %v", res, err)
	}
	if res, err := te.Hold(ctx, []string{"pinned"}, true); err != nil || res[0].Changed {
		t.Errorf("повтор hold: %+v %v", res, err)
	}
	if !installRecord(t, te, "pinned").Held {
		t.Fatal("held не записан в реестр")
	}
	if h := installHistory(t, te); !strings.Contains(h, `"op":"hold","ext":"pinned"`) {
		t.Errorf("history: %s", h)
	}

	rep := upgradeAll(t, te, UpgradeRequest{})
	wantItem(t, rep, "pinned", ActionHeld, ReasonHeld, StatusHeld)
	wantItem(t, rep, "agent", ActionUpgrade, "", StatusUpgraded)
	if rep.Summary.ExitCode != 0 || rep.Summary.Skipped != 1 {
		t.Errorf("сводка %+v", rep.Summary)
	}
	if v := installRecord(t, te, "pinned").Version; v != "1.0.0" {
		t.Errorf("закреплённое обновлено: %s", v)
	}

	// Поимённо — отказ с подсказкой.
	rep, err = te.Upgrade(ctx, UpgradeRequest{Names: []string{"pinned"}})
	if err != nil {
		t.Fatal(err)
	}
	wantItem(t, rep, "pinned", ActionBlocked, ReasonHeld, StatusBlocked)
	wantCode(t, rep.Err(), cerr.ExitNotApplicable, "hold.held")
	if !strings.Contains(rep.Err().Error(), "cream unhold pinned") {
		t.Errorf("нет подсказки: %v", rep.Err())
	}
	// install новой версии закреплённого — тот же отказ.
	_, err = te.Install(ctx, InstallRequest{Refs: []string{"pinned"}})
	wantCode(t, err, cerr.ExitNotApplicable, "hold.held")

	if res, err := te.Hold(ctx, []string{"pinned"}, false); err != nil || !res[0].Changed || res[0].Held {
		t.Fatalf("unhold: %+v %v", res, err)
	}
	rep = upgradeAll(t, te, UpgradeRequest{})
	wantItem(t, rep, "pinned", ActionUpgrade, "", StatusUpgraded)
	if rec := installRecord(t, te, "pinned"); rec.Version != "2.0.0" || rec.Held {
		t.Errorf("после unhold: %+v", rec)
	}
}

// Закрепление переживает повторную установку той же версии.
func TestHoldSurvivesReinstall(t *testing.T) {
	t.Parallel()
	te := installEngine(t)
	installMust(t, te, "noop")
	if _, err := te.Hold(context.Background(), []string{"noop"}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := te.Install(context.Background(), InstallRequest{Refs: []string{"noop"}, Reinstall: true}); err != nil {
		t.Fatal(err)
	}
	if !installRecord(t, te, "noop").Held {
		t.Error("held потерян после --reinstall")
	}
}

// Сбой одного не останавливает остальные; зависимые от него — blocked:
// dependency; порядок — поставщики раньше потребителей.
func TestUpgradeContinuesAfterFailure(t *testing.T) {
	t.Parallel()
	dep := "depends: [{name: prov, version: \">=1.0.0\"}]\n"
	first := []planExt{{name: "prov", version: "1.0.0"}, {name: "cons", version: "1.0.0", extra: dep},
		{name: "zeta", version: "1.0.0"}}
	next := []planExt{{name: "prov", version: "2.0.0", fail: "upgrade"}, {name: "cons", version: "2.0.0", extra: dep},
		{name: "zeta", version: "2.0.0"}}

	t.Run("по умолчанию", func(t *testing.T) {
		t.Parallel()
		te := planSetup(t, first, next)
		rep := upgradeAll(t, te, UpgradeRequest{})
		it := wantItem(t, rep, "prov", ActionUpgrade, "", StatusFailed)
		if it.Error == nil || it.Error.Code != "hook.failed" {
			t.Errorf("prov: %+v", it.Error)
		}
		it = wantItem(t, rep, "cons", ActionUpgrade, ReasonDependency, StatusBlocked)
		if it.Error == nil || !strings.Contains(it.Error.Message, "prov") {
			t.Errorf("cons: %+v", it.Error)
		}
		wantItem(t, rep, "zeta", ActionUpgrade, "", StatusUpgraded)
		if s := rep.Summary; s.Upgraded != 1 || s.Failed != 1 || s.Skipped != 1 || s.ExitCode != cerr.ExitHook {
			t.Errorf("сводка %+v", s)
		}
		wantCode(t, rep.Err(), cerr.ExitHook, "upgrade.partial")
		if got := planOrder(t, te); !slices.Equal(got, []string{"prov@2.0.0", "zeta@2.0.0"}) {
			t.Errorf("хуки: %v", got)
		}
	})
	t.Run("--fail-fast", func(t *testing.T) {
		t.Parallel()
		te := planSetup(t, first, next)
		rep := upgradeAll(t, te, UpgradeRequest{FailFast: true})
		wantItem(t, rep, "prov", ActionUpgrade, "", StatusFailed)
		wantItem(t, rep, "zeta", ActionUpgrade, "", StatusNotRun)
		if got := planOrder(t, te); !slices.Equal(got, []string{"prov@2.0.0"}) {
			t.Errorf("хуки: %v", got)
		}
		if rep.Summary.ExitCode != cerr.ExitHook {
			t.Errorf("сводка %+v", rep.Summary)
		}
	})
}

func TestUpgradeTopologicalOrder(t *testing.T) {
	t.Parallel()
	dep := "depends: [{name: zprov, version: \">=1.0.0\"}]\n"
	first := []planExt{{name: "zprov", version: "1.0.0"}, {name: "acons", version: "1.0.0", extra: dep}}
	next := []planExt{{name: "zprov", version: "2.0.0"}, {name: "acons", version: "2.0.0",
		extra: "depends: [{name: zprov, version: \">=2.0.0\"}]\n"}}
	te := planSetup(t, first, next)
	rep := upgradeAll(t, te, UpgradeRequest{})
	if got := planOrder(t, te); !slices.Equal(got, []string{"zprov@2.0.0", "acons@2.0.0"}) {
		t.Errorf("порядок хуков: %v", got)
	}
	if rep.Plan[0].Name != "zprov" || rep.Plan[1].Name != "acons" {
		t.Errorf("порядок плана: %+v", rep.Plan)
	}
}

// Новая версия потребителя требует новой версии закреплённого поставщика —
// потребитель blocked:dependency (67) ещё в плане, остальное обновляется.
func TestUpgradeGraphBlocked(t *testing.T) {
	t.Parallel()
	first := []planExt{{name: "prov", version: "1.0.0"}, {name: "cons", version: "1.0.0"}, {name: "zeta", version: "1.0.0"}}
	next := []planExt{{name: "prov", version: "2.0.0"},
		{name: "cons", version: "2.0.0", extra: "depends: [{name: prov, version: \">=2.0.0\"}]\n"},
		{name: "zeta", version: "2.0.0"}}
	te := planSetup(t, first, next)
	if _, err := te.Hold(context.Background(), []string{"prov"}, true); err != nil {
		t.Fatal(err)
	}
	rep := upgradeAll(t, te, UpgradeRequest{DryRun: true})
	it := wantItem(t, rep, "cons", ActionBlocked, ReasonDependency, StatusBlocked)
	if it.Error == nil || it.Error.Code != "deps.version" {
		t.Errorf("cons: %+v", it.Error)
	}
	wantItem(t, rep, "zeta", ActionUpgrade, "", StatusPlanned)
	if rep.Summary.ExitCode != cerr.ExitGraph {
		t.Errorf("сводка %+v", rep.Summary)
	}
}

func TestUpgradeDryRun(t *testing.T) {
	t.Parallel()
	te := planSetup(t, []planExt{{name: "agent", version: "1.0.0"}, {name: "same", version: "1.0.0"}},
		[]planExt{{name: "agent", version: "1.1.0"}, {name: "same", version: "1.0.0"}})
	rep := upgradeAll(t, te, UpgradeRequest{DryRun: true, Confirm: func([]UpgradeItem) (bool, error) {
		t.Error("dry-run спрашивает подтверждение")
		return false, nil
	}})
	if !rep.DryRun {
		t.Error("dry_run = false")
	}
	wantItem(t, rep, "agent", ActionUpgrade, "", StatusPlanned)
	wantItem(t, rep, "same", ActionUpToDate, "", StatusUnchanged)
	if got := planOrder(t, te); len(got) != 0 {
		t.Errorf("dry-run вызвал хуки: %v", got)
	}
	if v := installRecord(t, te, "agent").Version; v != "1.0.0" {
		t.Errorf("dry-run изменил реестр: %s", v)
	}
	if len(rep.Extensions) != 0 {
		t.Errorf("extensions в dry-run: %+v", rep.Extensions)
	}
}

func TestUpgradeConfirm(t *testing.T) {
	t.Parallel()
	first := []planExt{{name: "agent", version: "1.0.0"}}
	t.Run("нет — ничего не сделано, 1 upgrade.declined", func(t *testing.T) {
		t.Parallel()
		te := planSetup(t, first, []planExt{{name: "agent", version: "1.1.0"}})
		var shown []UpgradeItem
		_, err := te.Upgrade(context.Background(), UpgradeRequest{All: true, Confirm: func(p []UpgradeItem) (bool, error) {
			shown = p
			return false, nil
		}})
		wantCode(t, err, cerr.ExitCheck, "upgrade.declined")
		if len(shown) != 1 || shown[0].Name != "agent" || shown[0].Status != StatusPlanned {
			t.Errorf("показан план %+v", shown)
		}
		if v := installRecord(t, te, "agent").Version; v != "1.0.0" {
			t.Errorf("обновлено после отказа: %s", v)
		}
	})
	t.Run("да — обновлено", func(t *testing.T) {
		t.Parallel()
		te := planSetup(t, first, []planExt{{name: "agent", version: "1.1.0"}})
		rep := upgradeAll(t, te, UpgradeRequest{Confirm: func([]UpgradeItem) (bool, error) { return true, nil }})
		wantItem(t, rep, "agent", ActionUpgrade, "", StatusUpgraded)
	})
	t.Run("нечего обновлять — не спрашивает", func(t *testing.T) {
		t.Parallel()
		te := planSetup(t, first, first)
		upgradeAll(t, te, UpgradeRequest{Confirm: func([]UpgradeItem) (bool, error) {
			t.Error("спросил без обновлений")
			return false, nil
		}})
	})
}

// --fail-fast: заблокированный в плане останавливает всё до первого действия.
func TestUpgradeFailFastPlan(t *testing.T) {
	t.Parallel()
	te := planSetup(t,
		[]planExt{{name: "firewall", version: "1.0.0", extra: confirmExt}, {name: "agent", version: "1.0.0"}},
		[]planExt{{name: "firewall", version: "1.1.0", extra: confirmExt}, {name: "agent", version: "1.1.0"}})
	rep := upgradeAll(t, te, UpgradeRequest{FailFast: true})
	wantItem(t, rep, "firewall", ActionBlocked, ReasonRisk, StatusBlocked)
	wantItem(t, rep, "agent", ActionUpgrade, "", StatusNotRun)
	if got := planOrder(t, te); len(got) != 0 {
		t.Errorf("хуки: %v", got)
	}
	if rep.Summary.ExitCode != cerr.ExitNotApplicable {
		t.Errorf("сводка %+v", rep.Summary)
	}
}

// Без новой версии — up-to-date и для записи skipped: хук не повторяется.
func TestUpgradeSkippedIsUpToDate(t *testing.T) {
	t.Parallel()
	te := installEngine(t)
	seedInstalled(t, te, fixtureDir(t, "noop"), func(r *state.Record) {
		r.Source = state.SourceRef{Name: "fixtures", Type: "path"}
		r.Status = state.StatusSkipped
	})
	rep := upgradeAll(t, te, UpgradeRequest{})
	wantItem(t, rep, "noop", ActionUpToDate, "", StatusUnchanged)
	if n := hookCount(t, te, "noop", "install"); n != 0 {
		t.Errorf("install.count = %d", n)
	}
}

func TestUpgradeAcceptRiskUsage(t *testing.T) {
	t.Parallel()
	te := installEngine(t)
	_, err := te.Upgrade(context.Background(), UpgradeRequest{All: true, AcceptRisk: []string{"../x"}})
	wantCode(t, err, cerr.ExitUsage, "usage.ext_name")
}
