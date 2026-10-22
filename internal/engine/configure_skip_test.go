package engine

// Код 20 хука configure — «пропущено» (спека bisquite-bundled-cream §10.8, Task 13).

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/iamletenkov/cream/internal/state"
)

// skipHook — хук configure, который считает вызовы и выходит с кодом 20.
const skipHook = `n=$(cat "$CREAM_STATE_DIR/configure.count" 2>/dev/null || echo 0)
echo $((n+1)) > "$CREAM_STATE_DIR/configure.count"
exit 20`

// configureHistory — записи configure из history.jsonl.
func configureHistory(t *testing.T, te *testEngine) []state.HistoryEntry {
	t.Helper()
	data, err := os.ReadFile(te.L.History())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []state.HistoryEntry
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var h state.HistoryEntry
		if err := json.Unmarshal([]byte(line), &h); err != nil {
			t.Fatalf("history: %q: %v", line, err)
		}
		if h.Op == "configure" {
			out = append(out, h)
		}
	}
	return out
}

func TestConfigureSkippedOnBoot(t *testing.T) {
	t.Parallel()
	te := newConfiguredEngine(t, fixtureDir(t, cfgExt))
	writeHook(t, te, cfgExt, "configure", skipHook)

	res := bootConfigure(t, te)
	if res.Mode != ApplyHook || res.Status != ConfigureSkipped || res.Reason != ReasonBoot {
		t.Errorf("первая загрузка: %+v", res)
	}
	if getMark(t, te) != nil {
		t.Error("код 20 записал отметку")
	}
	hist := configureHistory(t, te)
	if len(hist) != 1 || hist[0].Status != state.StatusSkipped || hist[0].Ext != cfgExt || hist[0].Phase != PhaseLive {
		t.Errorf("history: %+v", hist)
	}
	if !strings.Contains(te.Err.String(), "configure "+cfgExt+": пропущено") {
		t.Errorf("нет строки журнала о пропуске: %q", te.Err.String())
	}
	if rec, _, _ := te.Store.Get(cfgExt); rec.Status != state.StatusInstalled || rec.RebootRequired {
		t.Errorf("реестр изменён: %+v", rec)
	}

	// Отметки нет — следующая загрузка снова запускает хук.
	if res := bootConfigure(t, te); res.Mode != ApplyHook || res.Status != ConfigureSkipped {
		t.Errorf("вторая загрузка: %+v", res)
	}
	if n := hookCount(t, te, cfgExt, "configure"); n != 2 {
		t.Errorf("хук вызван %d раз, ждали 2", n)
	}
	if n := len(configureHistory(t, te)); n != 2 {
		t.Errorf("записей configure в history: %d", n)
	}
}

// Прежняя отметка при коде 20 снимается (решение владельца: «при коде 20 —
// повтор на следующей загрузке»): хук гарантированно пойдёт снова, даже если
// отметка от прошлой успешной загрузки совпала бы с реестром.
func TestConfigureSkippedDropsOldMark(t *testing.T) {
	t.Parallel()
	te := newConfiguredEngine(t, fixtureDir(t, cfgExt))
	bootConfigure(t, te)
	if getMark(t, te) == nil {
		t.Fatal("отметка не записана после успеха")
	}
	writeHook(t, te, cfgExt, "configure", skipHook)
	// Ручной прогон (причина apply) идёт мимо отметки и выходит с кодом 20.
	res, err := te.ConfigureReport(context.Background(), cfgExt, ReasonApply, ConfigureOptions{})
	if err != nil || res.Mode != ApplyHook || res.Status != ConfigureSkipped {
		t.Fatalf("ручной прогон: %+v %v", res, err)
	}
	if getMark(t, te) != nil {
		t.Fatal("код 20 оставил прежнюю отметку: на загрузке хук был бы пропущен")
	}
	if res := bootConfigure(t, te); res.Mode != ApplyHook {
		t.Errorf("повтор на следующей загрузке не пошёл: %+v", res)
	}
	// Счётчик общий с хуком фикстуры: успех + ручной прогон + загрузка.
	if n := hookCount(t, te, cfgExt, "configure"); n != 3 {
		t.Errorf("хук вызван %d раз, ждали 3", n)
	}
}

// Ручной cream configure: код 20 — не ошибка, status skipped.
func TestConfigureSkippedManual(t *testing.T) {
	t.Parallel()
	te := newConfiguredEngine(t, fixtureDir(t, cfgExt))
	writeHook(t, te, cfgExt, "configure", skipHook)
	res, err := te.ConfigureReport(context.Background(), cfgExt, ReasonApply, ConfigureOptions{Wait: true})
	if err != nil {
		t.Fatalf("код 20 — ошибка: %v", err)
	}
	if res.Status != ConfigureSkipped || res.Mode != ApplyHook || res.Reason != ReasonApply {
		t.Errorf("res = %+v", res)
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"status":"skipped"`) {
		t.Errorf("JSON без status skipped: %s", data)
	}
	if getMark(t, te) != nil {
		t.Error("код 20 записал отметку")
	}
}

// Успешный прогон хука несёт status ok (как и пропуск, и сбой — исход хука).
func TestConfigureSuccessStatusOK(t *testing.T) {
	t.Parallel()
	te := newConfiguredEngine(t, fixtureDir(t, cfgExt))
	res := bootConfigure(t, te)
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != ConfigureOK || !strings.Contains(string(data), `"status":"ok"`) {
		t.Errorf("успех без status ok: %s", data)
	}
}

// Фаза build: код 20 — тоже «пропущено», отметок нет вовсе.
func TestConfigureSkippedBuildPhase(t *testing.T) {
	t.Parallel()
	te := newConfiguredEngine(t, fixtureDir(t, cfgExt))
	te.EnvVars["CREAM_PHASE"] = PhaseBuild
	writeHook(t, te, cfgExt, "configure", skipHook)
	res, err := te.ConfigureReport(context.Background(), cfgExt, ReasonApply, ConfigureOptions{})
	if err != nil || res.Status != ConfigureSkipped {
		t.Fatalf("build: %+v %v", res, err)
	}
	if getMark(t, te) != nil {
		t.Error("фаза build записала отметку")
	}
}
