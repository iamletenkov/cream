package engine

import (
	"context"
	"path/filepath"
	"reflect"
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
	t.Run("hook failure keeps prev, retry, then success", func(t *testing.T) {
		t.Parallel()
		dir := installCatalog(t, "1.0.0", map[string]string{"upgrade-fixture": fixtureDir(t, "upgrade-fixture")})
		catalogWrite(t, dir, "upgrade-fixture", "hooks/upgrade", "#!/bin/sh\nexit 1\n")
		te := installEngine(t, installSrc{"fixtures", dir})
		seedInstalled(t, te, testdataDir(t, "catalog-next", "extensions", "upgrade-fixture"))
		for i := range 2 {
			_, err := installDowngrade(t, te)
			wantCode(t, err, cerr.ExitHook, "hook.failed")
			rec := installRecord(t, te, "upgrade-fixture")
			if rec.Version != "2.0.0" || rec.Status != state.StatusFailedUpgrade {
				t.Errorf("попытка %d: реестр после сбоя: %+v", i+1, rec)
			}
			if v := upgradeTreeVersion(t, te, te.L.ExtPrev("upgrade-fixture")); v != "2.0.0" {
				t.Errorf("попытка %d: .prev = %s, ждали 2.0.0", i+1, v)
			}
			if v := upgradeTreeVersion(t, te, te.L.ExtTree("upgrade-fixture")); v != "1.0.0" {
				t.Errorf("попытка %d: живое дерево = %s, ждали 1.0.0", i+1, v)
			}
		}
		if h := installHistory(t, te); strings.Count(h, `"op":"downgrade"`) != 2 ||
			!strings.Contains(h, `"status":"failed-upgrade"`) {
			t.Errorf("history.jsonl: %s", h)
		}
		// Хук исправлен в каталоге — повтор с флагом проходит.
		catalogWrite(t, dir, "upgrade-fixture", "hooks/upgrade", "#!/bin/sh\nexit 0\n")
		installSync(t, te, installSrc{"fixtures", dir})
		res, err := installDowngrade(t, te)
		if err != nil {
			t.Fatalf("повтор после исправления: %v (stderr: %s)", err, te.Err.String())
		}
		rec := installRecord(t, te, "upgrade-fixture")
		if res.Version != "1.0.0" || rec.Version != "1.0.0" || rec.Status != state.StatusInstalled {
			t.Errorf("реестр после успеха: %+v", rec)
		}
		if installExists(te.L.ExtPrev("upgrade-fixture")) {
			t.Error(".prev остался после успешного понижения")
		}
	})
	t.Run("same version hook failure keeps the old digest", func(t *testing.T) {
		t.Parallel()
		dir := installCatalog(t, "1.0.0", map[string]string{"upgrade-fixture": fixtureDir(t, "upgrade-fixture")})
		catalogWrite(t, dir, "upgrade-fixture", "hooks/upgrade", "#!/bin/sh\nexit 1\n")
		te := installEngine(t, installSrc{"fixtures", dir})
		old := "sha256:" + strings.Repeat("0", 64)
		seedInstalled(t, te, fixtureDir(t, "upgrade-fixture"), func(r *state.Record) { r.ExtDigest = old })
		_, err := installDowngrade(t, te)
		wantCode(t, err, cerr.ExitHook, "hook.failed")
		rec := installRecord(t, te, "upgrade-fixture")
		if rec.ExtDigest != old || rec.Status != state.StatusFailedUpgrade || rec.Version != "1.0.0" {
			t.Errorf("реестр после сбоя: %+v", rec)
		}
	})
	t.Run("rollback after failed-upgrade is a plain install", func(t *testing.T) {
		// Так откатывается stvor: понижение сорвалось, каталог вернулся к
		// версии и отпечатку из реестра — install без CREAM_PREV_VERSION.
		t.Parallel()
		dir := installCatalog(t, "1.0.0", map[string]string{"upgrade-fixture": fixtureDir(t, "upgrade-fixture")})
		catalogWrite(t, dir, "upgrade-fixture", "hooks/upgrade", "#!/bin/sh\nexit 1\n")
		te := installEngine(t, installSrc{"fixtures", dir})
		next := installCatalog(t, "1.1.0", map[string]string{
			"upgrade-fixture": testdataDir(t, "catalog-next", "extensions", "upgrade-fixture")})
		seedInstalled(t, te, filepath.Join(next, "extensions", "upgrade-fixture"))
		_, err := installDowngrade(t, te)
		wantCode(t, err, cerr.ExitHook, "hook.failed")
		installSync(t, te, installSrc{"fixtures", next})
		if _, err := installDowngrade(t, te); err != nil {
			t.Fatalf("откат: %v (stderr: %s)", err, te.Err.String())
		}
		rec := installRecord(t, te, "upgrade-fixture")
		if rec.Version != "2.0.0" || rec.Status != state.StatusInstalled {
			t.Errorf("реестр после отката: %+v", rec)
		}
		if n := hookCount(t, te, "upgrade-fixture", "install"); n != 1 {
			t.Errorf("хук install вызван %d раз", n)
		}
		if log := readState(t, te, "upgrade-fixture", "install.log"); !strings.Contains(log, "prev= ") ||
			!strings.Contains(log, "version=2.0.0") {
			t.Errorf("окружение хука install: %q", log)
		}
		if v := upgradeTreeVersion(t, te, te.L.ExtTree("upgrade-fixture")); v != "2.0.0" {
			t.Errorf("живое дерево = %s", v)
		}
		if installExists(te.L.ExtPrev("upgrade-fixture")) {
			t.Error(".prev остался после отката")
		}
	})
	t.Run("build phase refuses the flag", func(t *testing.T) {
		t.Parallel()
		te := installEngine(t)
		te.EnvVars["CREAM_PHASE"] = PhaseBuild
		_, err := installDowngrade(t, te)
		wantCode(t, err, cerr.ExitUsage, "usage.flag")
	})
}

// TestInstallDowngradeRefusedChangesNothing — отказ без флага (65) не трогает
// реестр, дерево и history.jsonl.
func TestInstallDowngradeRefusedChangesNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, code string
		src        func(t *testing.T) string
		mutate     func(*state.Record)
	}{
		{"downgrade", "version.downgrade",
			func(t *testing.T) string { return testdataDir(t, "catalog-next", "extensions", "upgrade-fixture") }, nil},
		{"same version other digest", "version.same_digest",
			func(t *testing.T) string { return fixtureDir(t, "upgrade-fixture") },
			func(r *state.Record) { r.ExtDigest = "sha256:" + strings.Repeat("0", 64) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			te := installEngine(t)
			var mut []func(*state.Record)
			if tc.mutate != nil {
				mut = append(mut, tc.mutate)
			}
			before := seedInstalled(t, te, tc.src(t), mut...).Record
			_, err := installRun(t, te, "upgrade-fixture")
			wantCode(t, err, cerr.ExitData, tc.code)
			if rec := installRecord(t, te, "upgrade-fixture"); !reflect.DeepEqual(rec, before) {
				t.Errorf("реестр изменён:\n было %+v\n стало %+v", before, rec)
			}
			for _, p := range []string{te.L.ExtNew("upgrade-fixture"), te.L.ExtPrev("upgrade-fixture")} {
				if installExists(p) {
					t.Errorf("%s появился", p)
				}
			}
			if h := installHistory(t, te); h != "" {
				t.Errorf("history.jsonl: %s", h)
			}
		})
	}
}
