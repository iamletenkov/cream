package engine

// Исход юнита для `cream configure` при работающем systemd: юнит
// cream-configure@<ext> пишет /run/cream/configure/<ext>.result, перезапуск
// с --wait читает его и отдаёт status ok/skipped/failed.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
)

// runUnitOnRestart: синхронный перезапуск фейкового systemd исполняет то же,
// что ExecStart юнита, — cream configure --from-unit; сбой юнита — ошибка
// перезапуска, как у systemctl restart.
func runUnitOnRestart(te *testEngine) {
	te.Sys.OnRestart = func(_ string, noBlock bool) error {
		if noBlock {
			return nil
		}
		if _, err := te.ConfigureReport(context.Background(), cfgExt, "", ConfigureOptions{FromUnit: true}); err != nil {
			return errors.New("unit failed")
		}
		return nil
	}
}

func readResult(t *testing.T, te *testEngine) string {
	t.Helper()
	data, err := os.ReadFile(te.L.RunResult(cfgExt))
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestConfigureRestartStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, hook, status string
		reboot             bool
	}{
		{"ok", "exit 0", ConfigureOK, false},
		{"reboot", "exit 10", ConfigureOK, true},
		{"skipped", "exit 20", ConfigureSkipped, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			te := newConfiguredEngine(t, fixtureDir(t, cfgExt))
			writeHook(t, te, cfgExt, "configure", tc.hook)
			te.systemdRunning("running")
			runUnitOnRestart(te)
			res, err := te.ConfigureReport(context.Background(), cfgExt, ReasonApply, ConfigureOptions{Wait: true})
			if err != nil {
				t.Fatal(err)
			}
			if res.Mode != ApplyRestart || res.Status != tc.status || res.RebootRequired != tc.reboot {
				t.Errorf("res = %+v, ждали status %s reboot %v", res, tc.status, tc.reboot)
			}
		})
	}
}

func TestConfigureRestartFailedStatus(t *testing.T) {
	t.Parallel()
	te := newConfiguredEngine(t, fixtureDir(t, cfgExt))
	writeHook(t, te, cfgExt, "configure", "exit 3")
	te.systemdRunning("running")
	runUnitOnRestart(te)
	_, err := te.ConfigureReport(context.Background(), cfgExt, ReasonApply, ConfigureOptions{Wait: true})
	wantCode(t, err, cerr.ExitHook, "hook.failed")
	var ce *cerr.Error
	if !errors.As(err, &ce) || ce.Details["status"] != ConfigureFailed {
		t.Errorf("details без status failed: %+v", ce)
	}
}

// Устаревший исход прошлого прогона не выдаётся за исход этого: перед
// перезапуском он снимается; юнит, не записавший исход, — status пуст.
func TestConfigureRestartStaleResult(t *testing.T) {
	t.Parallel()
	te := newConfiguredEngine(t, fixtureDir(t, cfgExt))
	te.systemdRunning("running")
	p := te.L.RunResult(cfgExt)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"status":"skipped"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := te.ConfigureReport(context.Background(), cfgExt, ReasonApply, ConfigureOptions{Wait: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != ApplyRestart || res.Status != "" {
		t.Errorf("res = %+v: устаревший исход выдан за новый", res)
	}
}

// Без --wait перезапуск в очереди: исход неизвестен, status пуст.
func TestConfigureQueuedNoStatus(t *testing.T) {
	t.Parallel()
	te := newConfiguredEngine(t, fixtureDir(t, cfgExt))
	te.systemdRunning("running")
	runUnitOnRestart(te)
	res, err := te.ConfigureReport(context.Background(), cfgExt, ReasonApply, ConfigureOptions{})
	if err != nil || res.Mode != ApplyQueued || res.Status != "" {
		t.Errorf("res = %+v %v", res, err)
	}
}

// Юнит пишет исход каждого прогона хука; пропуск по отметке исход снимает.
func TestConfigureFromUnitWritesResult(t *testing.T) {
	t.Parallel()
	te := newConfiguredEngine(t, fixtureDir(t, cfgExt))
	bootConfigure(t, te)
	if got := readResult(t, te); got == "" {
		t.Fatal("юнит не записал исход")
	}
	if res := bootConfigure(t, te); res.Mode != ConfigureUpToDate {
		t.Fatalf("вторая загрузка: %+v", res)
	}
	if got := readResult(t, te); got != "" {
		t.Errorf("исход прошлого прогона остался после пропуска по отметке: %s", got)
	}
}
