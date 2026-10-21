package engine

// Отметка выполнения configure (спека bisquite-bundled-cream §10.7, Task 11).

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/config"
	"github.com/iamletenkov/cream/internal/fsx"
	"github.com/iamletenkov/cream/internal/lock"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/semver"
	"github.com/iamletenkov/cream/internal/state"
)

const cfgExt = "configure-system"

// configureFixture — копия configure-system во временном каталоге; edit правит
// extension.yaml (nil — без правок).
func configureFixture(t *testing.T, edit func(string) string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), cfgExt)
	if err := fsx.CopyTree(fixtureDir(t, cfgExt), dir); err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		p := filepath.Join(dir, "extension.yaml")
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(edit(string(data))), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// withSecret добавляет секретный ключ TOKEN.
func withSecret(y string) string {
	return y + "    TOKEN: {type: str, default: \"\", empty: true, secret: file, description: \"секрет\"}\n"
}

// withEveryBoot объявляет configure.every_boot (cream >=1.2.0).
func withEveryBoot(y string) string {
	y = strings.Replace(y, `cream: ">=1.0.0"`, `cream: ">=1.2.0"`, 1)
	return strings.Replace(y, "wants: [network-online.target]}", "wants: [network-online.target], every_boot: true}", 1)
}

// newConfiguredEngine — движок без systemd, с /etc/machine-id и установленным
// расширением src.
func newConfiguredEngine(t *testing.T, src string) *testEngine {
	t.Helper()
	te := newTestEngine(t)
	te.Cream = manifest.CreamBuild{Version: semver.Version{Major: 1, Minor: 2}}
	writeFile(t, te, "/etc/machine-id", "machine-of-the-test-host\n", 0o644)
	seedInstalled(t, te, src)
	return te
}

// bootConfigure — вызов юнита на загрузке (маркера причины нет).
func bootConfigure(t *testing.T, te *testEngine) ConfigureResult {
	t.Helper()
	res, err := te.ConfigureReport(context.Background(), cfgExt, "", ConfigureOptions{FromUnit: true})
	if err != nil {
		t.Fatalf("configure --from-unit: %v", err)
	}
	return res
}

func getMark(t *testing.T, te *testEngine) *state.Configured {
	t.Helper()
	c, ok, err := te.Store.GetConfigured(cfgExt)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		return nil
	}
	return c
}

func TestConfigureBootSkipsWhenMarkMatches(t *testing.T) {
	t.Parallel()
	te := newConfiguredEngine(t, fixtureDir(t, cfgExt))
	if res := bootConfigure(t, te); res.Mode != ApplyHook {
		t.Fatalf("первая загрузка: %+v", res)
	}
	if n := hookCount(t, te, cfgExt, "configure"); n != 1 {
		t.Fatalf("хук вызван %d раз", n)
	}
	mark := getMark(t, te)
	if mark == nil {
		t.Fatal("отметка не записана после успеха")
	}
	rec, _, _ := te.Store.Get(cfgExt)
	if mark.Version != rec.Version || mark.ExtDigest != rec.ExtDigest || mark.SecretsRev != rec.SecretsRev ||
		!strings.HasPrefix(mark.MachineID, "sha256:") || !strings.HasPrefix(mark.ParamsDigest, "sha256:") {
		t.Errorf("отметка %+v, запись %+v", mark, rec)
	}

	res := bootConfigure(t, te)
	if res.Mode != ConfigureUpToDate || res.Reason != ReasonBoot {
		t.Errorf("вторая загрузка: %+v", res)
	}
	if n := hookCount(t, te, cfgExt, "configure"); n != 1 {
		t.Errorf("хук запущен при совпавшей отметке: %d", n)
	}
	if !strings.Contains(te.Err.String(), "configure "+cfgExt+": пропуск, отметка совпала") {
		t.Errorf("нет строки журнала о пропуске: %q", te.Err.String())
	}
}

func TestConfigureBootRerunsWhenMarkDiffers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		src    func(string) string
		change func(t *testing.T, te *testEngine)
	}{
		{name: "нет отметки", change: func(t *testing.T, te *testEngine) {
			if err := te.Store.DeleteConfigured(cfgExt); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "version", change: func(t *testing.T, te *testEngine) {
			mutateRecord(t, te, func(r *state.Record) { r.Version = "1.0.1" })
		}},
		{name: "ext_digest", change: func(t *testing.T, te *testEngine) {
			mutateRecord(t, te, func(r *state.Record) { r.ExtDigest = "sha256:other" })
		}},
		{name: "params_digest", change: func(t *testing.T, te *testEngine) {
			if _, err := te.Set(context.Background(), cfgExt, []config.Change{{Key: "MESSAGE", Value: "bye"}},
				SetOptions{}); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "secrets_rev", src: withSecret, change: func(t *testing.T, te *testEngine) {
			if _, err := te.Set(context.Background(), cfgExt, []config.Change{{Key: "TOKEN", Value: "s3cr3t-rotated"}},
				SetOptions{}); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "machine_id", change: func(t *testing.T, te *testEngine) {
			writeFile(t, te, "/etc/machine-id", "machine-of-a-cloned-host\n", 0o644)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			te := newConfiguredEngine(t, configureFixture(t, tc.src))
			bootConfigure(t, te)
			tc.change(t, te)
			if res := bootConfigure(t, te); res.Mode != ApplyHook {
				t.Errorf("после изменения: %+v", res)
			}
			if n := hookCount(t, te, cfgExt, "configure"); n != 2 {
				t.Errorf("хук вызван %d раз, ждали 2", n)
			}
			// Отметка переписана — третья загрузка снова пропускает.
			if res := bootConfigure(t, te); res.Mode != ConfigureUpToDate {
				t.Errorf("третья загрузка: %+v", res)
			}
		})
	}
}

func mutateRecord(t *testing.T, te *testEngine, f func(*state.Record)) {
	t.Helper()
	rec, ok, err := te.Store.Get(cfgExt)
	if err != nil || !ok {
		t.Fatalf("реестр: %v %v", ok, err)
	}
	f(rec)
	if err := te.Store.Put(*rec); err != nil {
		t.Fatal(err)
	}
}

// Причины, кроме boot (install, upgrade, apply, followup), идут мимо отметки.
func TestConfigureNonBootReasonIgnoresMark(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{ReasonInstall, ReasonUpgrade, ReasonApply, ReasonFollowup} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()
			te := newConfiguredEngine(t, fixtureDir(t, cfgExt))
			bootConfigure(t, te)
			writeFile(t, te, "/run/cream/"+cfgExt+".reason", reason+"\n", 0o644)
			res := bootConfigure(t, te)
			if res.Mode != ApplyHook || res.Reason != reason {
				t.Errorf("res = %+v", res)
			}
			if n := hookCount(t, te, cfgExt, "configure"); n != 2 {
				t.Errorf("хук вызван %d раз, ждали 2", n)
			}
			if log := readState(t, te, cfgExt, "configure.log"); !strings.Contains(log, "reason="+reason+" ") {
				t.Errorf("CREAM_REASON: %q", log)
			}
		})
	}
}

func TestConfigureManualRunsAndRewritesMark(t *testing.T) {
	t.Parallel()
	te := newConfiguredEngine(t, fixtureDir(t, cfgExt))
	bootConfigure(t, te)
	mutateRecord(t, te, func(r *state.Record) { r.SecretsRev = 7 })
	res, err := te.ConfigureReport(context.Background(), cfgExt, ReasonApply, ConfigureOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != ApplyHook || hookCount(t, te, cfgExt, "configure") != 2 {
		t.Errorf("--force без systemd: %+v, вызовов %d", res, hookCount(t, te, cfgExt, "configure"))
	}
	if m := getMark(t, te); m == nil || m.SecretsRev != 7 {
		t.Errorf("отметка не переписана: %+v", m)
	}
}

func TestConfigureForceWithSystemdDropsMarkAndRestarts(t *testing.T) {
	t.Parallel()
	te := newConfiguredEngine(t, fixtureDir(t, cfgExt))
	bootConfigure(t, te)
	te.systemdRunning("running")
	res, err := te.ConfigureReport(context.Background(), cfgExt, ReasonApply, ConfigureOptions{Force: true, Wait: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != ApplyRestart {
		t.Errorf("res = %+v", res)
	}
	if getMark(t, te) != nil {
		t.Error("--force оставил отметку: прерванный перезапуск не повторится на загрузке")
	}
	if !slices.Contains(te.Sys.Calls(), "restart cream-configure@configure-system.service") {
		t.Errorf("юнит не перезапущен: %v", te.Sys.Calls())
	}
}

func TestConfigureForceAll(t *testing.T) {
	t.Parallel()
	te := newConfiguredEngine(t, fixtureDir(t, cfgExt))
	seedInstalled(t, te, fixtureDir(t, "configure-early"))
	seedInstalled(t, te, fixtureDir(t, "noop"))
	bootConfigure(t, te)
	res, err := te.ConfigureForceAll(context.Background(), ConfigureOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range res {
		names = append(names, r.Ext)
		if r.Mode != ApplyHook || r.Reason != ReasonApply {
			t.Errorf("%s: %+v", r.Ext, r)
		}
	}
	if want := []string{"configure-early", cfgExt}; !slices.Equal(names, want) {
		t.Errorf("расширения %v, ждали %v (без хука configure — не трогаются)", names, want)
	}
	if hookCount(t, te, cfgExt, "configure") != 2 {
		t.Error("configure-system не перезапущен")
	}
}

func TestConfigureFailureDropsMark(t *testing.T) {
	t.Parallel()
	te := newConfiguredEngine(t, fixtureDir(t, cfgExt))
	bootConfigure(t, te)
	writeHook(t, te, cfgExt, "configure", "exit 4")
	_, err := te.ConfigureReport(context.Background(), cfgExt, ReasonApply, ConfigureOptions{})
	wantCode(t, err, cerr.ExitHook, "hook.failed")
	if getMark(t, te) != nil {
		t.Error("отметка осталась после сбоя")
	}
}

// Код 20 отметку не трогает (Task 13 решает остальное про «пропущено»).
func TestConfigureNotApplicableKeepsMark(t *testing.T) {
	t.Parallel()
	te := newConfiguredEngine(t, fixtureDir(t, cfgExt))
	bootConfigure(t, te)
	writeHook(t, te, cfgExt, "configure", "exit 20")
	_, _ = te.ConfigureReport(context.Background(), cfgExt, ReasonApply, ConfigureOptions{})
	if getMark(t, te) == nil {
		t.Error("код 20 удалил отметку")
	}
}

func TestConfigureEveryBootIgnoresMark(t *testing.T) {
	t.Parallel()
	te := newConfiguredEngine(t, configureFixture(t, withEveryBoot))
	bootConfigure(t, te)
	if getMark(t, te) == nil {
		t.Fatal("every_boot: отметка не записана")
	}
	if res := bootConfigure(t, te); res.Mode != ApplyHook {
		t.Errorf("every_boot: %+v", res)
	}
	if n := hookCount(t, te, cfgExt, "configure"); n != 2 {
		t.Errorf("every_boot: хук вызван %d раз", n)
	}
}

func TestConfigureBuildPhaseWritesNoMark(t *testing.T) {
	t.Parallel()
	te := newConfiguredEngine(t, fixtureDir(t, cfgExt))
	te.EnvVars["CREAM_PHASE"] = PhaseBuild
	if _, err := te.ConfigureReport(context.Background(), cfgExt, ReasonApply, ConfigureOptions{}); err != nil {
		t.Fatal(err)
	}
	if getMark(t, te) != nil {
		t.Error("фаза build записала отметку")
	}
}

func TestConfigureMarkHasNoValues(t *testing.T) {
	t.Parallel()
	te := newConfiguredEngine(t, configureFixture(t, withSecret))
	if _, err := te.Set(context.Background(), cfgExt, []config.Change{{Key: "MESSAGE", Value: "plain-value-42"},
		{Key: "TOKEN", Value: "top-secret-99"}}, SetOptions{}); err != nil {
		t.Fatal(err)
	}
	bootConfigure(t, te)
	data, err := os.ReadFile(te.L.ConfiguredFile(cfgExt))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"plain-value-42", "top-secret-99", "machine-of-the-test-host"} {
		if strings.Contains(string(data), v) {
			t.Errorf("в отметке значение %q:\n%s", v, data)
		}
	}
}

func TestSetSecretBumpsSecretsRev(t *testing.T) {
	t.Parallel()
	te := newConfiguredEngine(t, configureFixture(t, withSecret))
	rev := func() int {
		rec, _, _ := te.Store.Get(cfgExt)
		return rec.SecretsRev
	}
	set := func(k, v string) {
		t.Helper()
		if _, err := te.Set(context.Background(), cfgExt, []config.Change{{Key: k, Value: v}}, SetOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	set("MESSAGE", "x")
	if rev() != 0 {
		t.Errorf("несекретный ключ поднял secrets_rev: %d", rev())
	}
	set("TOKEN", "a")
	set("TOKEN", "a") // без изменения — без роста
	set("TOKEN", "b")
	if rev() != 2 {
		t.Errorf("secrets_rev = %d, ждали 2", rev())
	}
}

func TestConfigureAgain(t *testing.T) {
	t.Parallel()
	te := newConfiguredEngine(t, fixtureDir(t, cfgExt))
	bootConfigure(t, te)
	te.systemdRunning("running")
	res, err := te.ConfigureReport(context.Background(), cfgExt, ReasonFollowup, ConfigureOptions{Again: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != ApplyQueued || res.Reason != ReasonFollowup {
		t.Errorf("res = %+v", res)
	}
	if !slices.Contains(te.Sys.Calls(), "restart --no-block cream-configure@configure-system.service") {
		t.Errorf("--again не перезапустил юнит без блокировки: %v", te.Sys.Calls())
	}
	data, err := os.ReadFile(te.L.RunReason(cfgExt))
	if err != nil || strings.TrimSpace(string(data)) != ReasonFollowup {
		t.Fatalf("маркер причины %q, %v", data, err)
	}
	// Юнит, перезапущенный с followup, идёт мимо совпавшей отметки.
	if res := bootConfigure(t, te); res.Mode != ApplyHook || res.Reason != ReasonFollowup {
		t.Errorf("юнит после --again: %+v", res)
	}
	if hookCount(t, te, cfgExt, "configure") != 2 {
		t.Error("хук не запущен после --again")
	}
}

func TestConfigureAgainWithoutSystemd(t *testing.T) {
	t.Parallel()
	te := newConfiguredEngine(t, fixtureDir(t, cfgExt))
	res, err := te.ConfigureReport(context.Background(), cfgExt, ReasonFollowup, ConfigureOptions{Again: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != ApplyNone || hookCount(t, te, cfgExt, "configure") != 0 {
		t.Errorf("--again без systemd запустил хук: %+v", res)
	}
	if !slices.Contains(warningCodes(te), "configure.no_systemd") {
		t.Errorf("нет предупреждения: %v", warningCodes(te))
	}
}

func TestConfigureFlagConflicts(t *testing.T) {
	t.Parallel()
	te := newConfiguredEngine(t, fixtureDir(t, cfgExt))
	for _, o := range []ConfigureOptions{
		{FromUnit: true, Force: true},
		{FromUnit: true, Again: true},
		{Again: true, Force: true},
		{Again: true, Wait: true},
	} {
		_, err := te.ConfigureReport(context.Background(), cfgExt, ReasonApply, o)
		wantCode(t, err, cerr.ExitUsage, "usage.flag")
	}
}

func TestRemoveDropsMark(t *testing.T) {
	t.Parallel()
	te := newConfiguredEngine(t, fixtureDir(t, cfgExt))
	bootConfigure(t, te)
	if _, err := te.Remove(context.Background(), cfgExt, RemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(te.L.ConfiguredFile(cfgExt)); !os.IsNotExist(err) {
		t.Errorf("отметка осталась после remove: %v", err)
	}
}

func TestConfiguredStatus(t *testing.T) {
	t.Parallel()
	te := newConfiguredEngine(t, configureFixture(t, withEveryBoot))
	seedInstalled(t, te, fixtureDir(t, "noop"))
	st, err := te.ConfiguredStatus(cfgExt)
	if err != nil {
		t.Fatal(err)
	}
	if st == nil || st.At != nil || st.Matches || !st.EveryBoot {
		t.Errorf("до configure: %+v", st)
	}
	bootConfigure(t, te)
	st, _ = te.ConfiguredStatus(cfgExt)
	if st == nil || st.At == nil || !st.At.Equal(testNow) || !st.Matches {
		t.Errorf("после configure: %+v", st)
	}
	mutateRecord(t, te, func(r *state.Record) { r.SecretsRev++ })
	if st, _ = te.ConfiguredStatus(cfgExt); st.Matches {
		t.Errorf("после ротации секрета matches: %+v", st)
	}
	if st, err := te.ConfiguredStatus("noop"); err != nil || st != nil {
		t.Errorf("без хука configure: %+v, %v", st, err)
	}
	list, err := te.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, le := range list {
		if (le.Configured != nil) != (le.Name == cfgExt) {
			t.Errorf("list %s: configured %+v", le.Name, le.Configured)
		}
	}
}

// Переустановка сохраняет secrets_rev, а секрет, записанный install, его
// поднимает (§10.7): иначе отметка «совпала» бы после ротации.
func TestInstallKeepsAndBumpsSecretsRev(t *testing.T) {
	t.Parallel()
	te := installEngine(t)
	install := func(secret string, reinstall bool) {
		t.Helper()
		r := installReq([]string{"with-config"}, "TOKEN=-")
		r.Params.Stdin = strings.NewReader(secret + "\n")
		r.Reinstall = reinstall
		if _, err := te.Install(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
	install("one", false)
	rev := installRecord(t, te, "with-config").SecretsRev
	if rev != 1 {
		t.Fatalf("секрет при install: secrets_rev = %d, ждали 1", rev)
	}
	if _, err := te.Set(context.Background(), "with-config", []config.Change{{Key: "TOKEN", Value: "two"}},
		SetOptions{NoApply: true}); err != nil {
		t.Fatal(err)
	}
	r := installReq([]string{"with-config"})
	r.Reinstall = true
	if _, err := te.Install(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if got := installRecord(t, te, "with-config").SecretsRev; got != 2 {
		t.Errorf("--reinstall сбросил secrets_rev: %d, ждали 2", got)
	}
	install("three", true)
	if got := installRecord(t, te, "with-config").SecretsRev; got != 3 {
		t.Errorf("новый секрет при --reinstall: secrets_rev = %d, ждали 3", got)
	}
}

// Настройки, которые хук configure сам поменял (вложенный cream set), входят
// в отметку: следующая загрузка хук не повторяет.
func TestConfigureMarkIncludesHookOwnChanges(t *testing.T) {
	t.Parallel()
	te := newConfiguredEngine(t, fixtureDir(t, cfgExt))
	writeHook(t, te, cfgExt, "configure", `mkdir -p "$CREAM_ETC_DIR" && printf 'MESSAGE=from-hook\n' > "$CREAM_CONFIG_FILE"
n=$(cat "$CREAM_STATE_DIR/configure.count" 2>/dev/null || echo 0)
echo $((n + 1)) > "$CREAM_STATE_DIR/configure.count"`)
	bootConfigure(t, te)
	if v, err := te.Get(cfgExt, "MESSAGE", false); err != nil || v != "from-hook" {
		t.Fatalf("хук не записал настройку: %q %v", v, err)
	}
	if res := bootConfigure(t, te); res.Mode != ConfigureUpToDate {
		t.Errorf("загрузка после хука, сменившего настройку: %+v", res)
	}
	if n := hookCount(t, te, cfgExt, "configure"); n != 1 {
		t.Errorf("хук вызван %d раз, ждали 1", n)
	}
}

// Запись сверяется под lock/<ext>: cream set, которого юнит дождался, виден.
func TestConfigureBootRereadsRecordUnderLock(t *testing.T) {
	t.Parallel()
	te := newConfiguredEngine(t, fixtureDir(t, cfgExt))
	bootConfigure(t, te)
	release := holdExtLock(t, te, cfgExt, lock.Exclusive)
	done := make(chan ConfigureResult)
	go func() {
		res, _ := te.ConfigureReport(context.Background(), cfgExt, "", ConfigureOptions{FromUnit: true})
		done <- res
	}()
	time.Sleep(200 * time.Millisecond) // юнит прочитал запись и ждёт блокировку
	mutateRecord(t, te, func(r *state.Record) { r.SecretsRev++ })
	release()
	if res := <-done; res.Mode != ApplyHook {
		t.Errorf("ротация во время ожидания блокировки потеряна: %+v", res)
	}
}
