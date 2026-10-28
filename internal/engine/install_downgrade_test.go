package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/state"
)

// installDowngrade ставит upgrade-fixture с --allow-downgrade.
func installDowngrade(t *testing.T, te *testEngine) (*InstallResult, error) {
	t.Helper()
	r := installReq([]string{"upgrade-fixture"})
	r.AllowDowngrade = true
	return te.Install(context.Background(), r)
}

// TestInstallAllowDowngrade — install --allow-downgrade (§6.5): понижение версии
// и та же версия с другим отпечатком идут путём upgrade (хук upgrade с
// CREAM_PREV_VERSION), реестр — фактически поставленная версия и отпечаток,
// history.jsonl — op downgrade.
func TestInstallAllowDowngrade(t *testing.T) {
	t.Parallel()
	t.Run("lower version", func(t *testing.T) {
		t.Parallel()
		te := installEngine(t)
		seedInstalled(t, te, testdataDir(t, "catalog-next", "extensions", "upgrade-fixture"))
		res, err := installDowngrade(t, te)
		if err != nil {
			t.Fatalf("install --allow-downgrade: %v (stderr: %s)", err, te.Err.String())
		}
		if res.Version != "1.0.0" || res.Status != state.StatusInstalled || !res.Changed {
			t.Errorf("результат: %+v", res)
		}
		if n := hookCount(t, te, "upgrade-fixture", "upgrade"); n != 1 {
			t.Errorf("хук upgrade вызван %d раз", n)
		}
		if n := hookCount(t, te, "upgrade-fixture", "install"); n != 0 {
			t.Errorf("хук install вызван %d раз", n)
		}
		if log := readState(t, te, "upgrade-fixture", "upgrade.log"); !strings.Contains(log, "prev=2.0.0") ||
			!strings.Contains(log, "version=1.0.0") {
			t.Errorf("окружение хука: %q", log)
		}
		rec := installRecord(t, te, "upgrade-fixture")
		if rec.Version != "1.0.0" || rec.ExtDigest != res.ExtDigest || rec.Status != state.StatusInstalled {
			t.Errorf("реестр: %+v", rec)
		}
		if h := installHistory(t, te); !strings.Contains(h, `"op":"downgrade"`) ||
			!strings.Contains(h, `"from":"2.0.0","to":"1.0.0"`) {
			t.Errorf("history.jsonl: %s", h)
		}
		if installExists(te.L.ExtPrev("upgrade-fixture")) {
			t.Error(".prev остался после успешного понижения")
		}
	})
	t.Run("same version other digest", func(t *testing.T) {
		t.Parallel()
		te := installEngine(t)
		other := "sha256:" + strings.Repeat("0", 64)
		seedInstalled(t, te, fixtureDir(t, "upgrade-fixture"), func(r *state.Record) { r.ExtDigest = other })
		res, err := installDowngrade(t, te)
		if err != nil {
			t.Fatalf("install --allow-downgrade: %v (stderr: %s)", err, te.Err.String())
		}
		if res.Version != "1.0.0" || res.ExtDigest == other || !res.Changed {
			t.Errorf("результат: %+v", res)
		}
		if n := hookCount(t, te, "upgrade-fixture", "upgrade"); n != 1 {
			t.Errorf("хук upgrade вызван %d раз", n)
		}
		if log := readState(t, te, "upgrade-fixture", "upgrade.log"); !strings.Contains(log, "prev=1.0.0") {
			t.Errorf("окружение хука: %q", log)
		}
		if rec := installRecord(t, te, "upgrade-fixture"); rec.ExtDigest != res.ExtDigest {
			t.Errorf("реестр: отпечаток %s, поставлен %s", rec.ExtDigest, res.ExtDigest)
		}
		if h := installHistory(t, te); !strings.Contains(h, `"op":"downgrade"`) ||
			!strings.Contains(h, `"from":"1.0.0","to":"1.0.0"`) {
			t.Errorf("history.jsonl: %s", h)
		}
	})
	t.Run("same version same digest is a repeat", func(t *testing.T) {
		t.Parallel()
		te := installEngine(t)
		seedInstalled(t, te, fixtureDir(t, "upgrade-fixture"))
		res, err := installDowngrade(t, te)
		if err != nil {
			t.Fatal(err)
		}
		if res.Changed || hookCount(t, te, "upgrade-fixture", "upgrade") != 0 {
			t.Errorf("повтор с флагом что-то сделал: %+v", res)
		}
	})
	t.Run("held refuses", func(t *testing.T) {
		t.Parallel()
		te := installEngine(t)
		seedInstalled(t, te, testdataDir(t, "catalog-next", "extensions", "upgrade-fixture"),
			func(r *state.Record) { r.Held = true })
		_, err := installDowngrade(t, te)
		wantCode(t, err, cerr.ExitNotApplicable, "hold.held")
		if rec := installRecord(t, te, "upgrade-fixture"); rec.Version != "2.0.0" {
			t.Errorf("реестр: %+v", rec)
		}
	})
	t.Run("hook failure keeps the installed version", func(t *testing.T) {
		t.Parallel()
		dir := installCatalog(t, "1.0.0", map[string]string{"upgrade-fixture": fixtureDir(t, "upgrade-fixture")})
		catalogWrite(t, dir, "upgrade-fixture", "hooks/upgrade", "#!/bin/sh\nexit 1\n")
		te := installEngine(t, installSrc{"fixtures", dir})
		seedInstalled(t, te, testdataDir(t, "catalog-next", "extensions", "upgrade-fixture"))
		_, err := installDowngrade(t, te)
		wantCode(t, err, cerr.ExitHook, "hook.failed")
		rec := installRecord(t, te, "upgrade-fixture")
		if rec.Version != "2.0.0" || rec.Status != state.StatusFailedUpgrade {
			t.Errorf("реестр после сбоя: %+v", rec)
		}
		if h := installHistory(t, te); !strings.Contains(h, `"op":"downgrade"`) ||
			!strings.Contains(h, `"status":"failed-upgrade"`) {
			t.Errorf("history.jsonl: %s", h)
		}
	})
}
