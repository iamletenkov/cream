package engine

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/state"
)

// upgradeCatalog — копия testdata/catalog-next, где хук upgrade у
// upgrade-fixture заменён телом body ("" — хука нет вовсе: убран из
// манифеста и дерева).
func upgradeCatalog(t *testing.T, body string) string {
	t.Helper()
	dir := installCatalog(t, "1.1.0", map[string]string{
		"upgrade-fixture": testdataDir(t, "catalog-next", "extensions", "upgrade-fixture")})
	ext := filepath.Join(dir, "extensions", "upgrade-fixture")
	hook := filepath.Join(ext, "hooks", "upgrade")
	if body != "" {
		if err := os.WriteFile(hook, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	mf := filepath.Join(ext, "extension.yaml")
	data, err := os.ReadFile(mf)
	if err != nil {
		t.Fatal(err)
	}
	out := strings.Replace(string(data), "  upgrade: {}\n", "", 1)
	if out == string(data) {
		t.Fatal("в манифесте нет upgrade: {}")
	}
	if err := os.WriteFile(mf, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// upgradeInstalled ставит upgrade-fixture 1.0.0 из источника fixtures и
// переключает тот же источник на каталог next (Р8: upgrade ищет в источнике
// из реестра).
func upgradeInstalled(t *testing.T, next string) *testEngine {
	t.Helper()
	te := installEngine(t)
	installMust(t, te, "upgrade-fixture")
	installSync(t, te, installSrc{"fixtures", next})
	return te
}

// upgradeTreeVersion — версия манифеста в каталоге dir.
func upgradeTreeVersion(t *testing.T, te *testEngine, dir string) string {
	t.Helper()
	m, err := manifest.Load(dir, te.Cream, nil)
	if err != nil {
		t.Fatal(err)
	}
	return m.Version.String()
}

func TestUpgrade(t *testing.T) {
	t.Parallel()
	te := upgradeInstalled(t, testdataDir(t, "catalog-next"))
	res, err := upgradeRun(te, []string{"upgrade-fixture"}, false)
	if err != nil {
		t.Fatalf("%v (stderr %s)", err, te.Err.String())
	}
	if len(res) != 1 || !res[0].Changed || res[0].Version != "2.0.0" || res[0].Status != state.StatusInstalled ||
		res[0].PrevVersion != "" || res[0].Source.CatalogVersion != "1.1.0" {
		t.Fatalf("результат: %+v", res)
	}
	rec := installRecord(t, te, "upgrade-fixture")
	if rec.Version != "2.0.0" || !rec.InstalledAt.Equal(testNow) {
		t.Errorf("запись: %+v", rec)
	}
	if n := hookCount(t, te, "upgrade-fixture", "upgrade"); n != 1 {
		t.Errorf("upgrade.count = %d", n)
	}
	if n := hookCount(t, te, "upgrade-fixture", "install"); n != 1 {
		t.Errorf("install.count = %d", n)
	}
	if log := readState(t, te, "upgrade-fixture", "upgrade.log"); !strings.Contains(log, "prev=1.0.0 ") || !strings.Contains(log, "version=2.0.0") {
		t.Errorf("upgrade.log: %q", log)
	}
	if installExists(te.L.ExtPrev("upgrade-fixture")) || installExists(te.L.ExtNew("upgrade-fixture")) {
		t.Error(".prev или .new остались после успеха")
	}
	if v := upgradeTreeVersion(t, te, te.L.ExtTree("upgrade-fixture")); v != "2.0.0" {
		t.Errorf("дерево %s", v)
	}
	if h := installHistory(t, te); !strings.Contains(h, `"op":"upgrade","ext":"upgrade-fixture","from":"1.0.0","to":"2.0.0","status":"installed"`) {
		t.Errorf("history: %s", h)
	}
	// Повтор — та же версия, ничего не делается.
	res, err = upgradeRun(te, nil, true)
	if err != nil || len(res) != 1 || res[0].Changed {
		t.Errorf("повтор upgrade --all: %+v %v", res, err)
	}
}

func TestUpgradeFailedKeepsPrev(t *testing.T) {
	t.Parallel()
	te := upgradeInstalled(t, upgradeCatalog(t, "exit 1"))
	_, err := upgradeRun(te, []string{"upgrade-fixture"}, false)
	wantCode(t, err, cerr.ExitHook, "hook.failed")
	rec := installRecord(t, te, "upgrade-fixture")
	if rec.Status != state.StatusFailedUpgrade || rec.Version != "1.0.0" || rec.PrevVersion != "1.0.0" {
		t.Fatalf("запись после сбоя: %+v", rec)
	}
	if v := upgradeTreeVersion(t, te, te.L.ExtPrev("upgrade-fixture")); v != "1.0.0" {
		t.Errorf(".prev: %s", v)
	}
	if v := upgradeTreeVersion(t, te, te.L.ExtTree("upgrade-fixture")); v != "2.0.0" {
		t.Errorf("дерево: %s", v)
	}
	if h := installHistory(t, te); !strings.Contains(h, `"op":"upgrade","ext":"upgrade-fixture","from":"1.0.0","to":"2.0.0","status":"failed-upgrade"`) {
		t.Errorf("history: %s", h)
	}
	// Повтор сорвавшейся операции: исправленный источник, cream install без флагов.
	installSync(t, te, installSrc{"fixtures", testdataDir(t, "catalog-next")})
	res := installMust(t, te, "upgrade-fixture")
	if res.Status != state.StatusInstalled || res.Version != "2.0.0" || res.PrevVersion != "" {
		t.Fatalf("повтор: %+v", res)
	}
	if log := readState(t, te, "upgrade-fixture", "upgrade.log"); !strings.Contains(log, "prev=1.0.0 ") {
		t.Errorf("upgrade.log: %q", log)
	}
	if installExists(te.L.ExtPrev("upgrade-fixture")) {
		t.Error(".prev остался после успешного повтора")
	}
}

func TestUpgradeWithoutHookRunsInstall(t *testing.T) {
	t.Parallel()
	te := upgradeInstalled(t, upgradeCatalog(t, ""))
	if _, err := upgradeRun(te, []string{"upgrade-fixture"}, false); err != nil {
		t.Fatal(err)
	}
	log := readState(t, te, "upgrade-fixture", "install.log")
	lines := strings.Split(strings.TrimSpace(log), "\n")
	if len(lines) != 2 || !strings.Contains(lines[1], "prev=1.0.0 ") || !strings.Contains(lines[1], "version=2.0.0") {
		t.Errorf("install.log: %q", log)
	}
}

func TestUpgradeErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		names    []string
		all      bool
		prep     func(t *testing.T, te *testEngine)
		wantExit int
		wantCode string
	}{
		{name: "nothing requested", wantExit: 64, wantCode: "usage.args"},
		{name: "names and --all", names: []string{"noop"}, all: true, wantExit: 64, wantCode: "usage.args"},
		{name: "not installed", names: []string{"noop"}, wantExit: 66, wantCode: "not_found.extension"},
		{name: "source gone", names: []string{"noop"}, wantExit: 69, wantCode: "source.missing",
			prep: func(t *testing.T, te *testEngine) {
				installMust(t, te, "noop")
				installSync(t, te, installSrc{"other", testdataDir(t, "catalog-next")})
			}},
		{name: "downgrade", names: []string{"upgrade-fixture"}, wantExit: 65, wantCode: "version.downgrade",
			prep: func(t *testing.T, te *testEngine) {
				seedInstalled(t, te, testdataDir(t, "catalog-next", "extensions", "upgrade-fixture"),
					func(r *state.Record) { r.Source = state.SourceRef{Name: "fixtures", Type: "path"} })
			}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			te := installEngine(t)
			if tc.prep != nil {
				tc.prep(t, te)
			}
			_, err := upgradeRun(te, tc.names, tc.all)
			wantCode(t, err, tc.wantExit, tc.wantCode)
		})
	}
}

// upgradeFrom ставит name из каталога first (источник fixtures) и переключает
// тот же источник на каталог next.
func upgradeFrom(t *testing.T, first, next, name string) *testEngine {
	t.Helper()
	te := installEngine(t, installSrc{"fixtures", first})
	installMust(t, te, name)
	installSync(t, te, installSrc{"fixtures", next})
	return te
}

// catalogWrite пишет файл rel в дерево расширения name каталога dir: hooks/ —
// с режимом 0755, остальное — 0644.
func catalogWrite(t *testing.T, dir, name, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, "extensions", name, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	mode := os.FileMode(0o644)
	if strings.HasPrefix(rel, "hooks/") {
		mode = 0o755
	}
	if err := os.WriteFile(p, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

// upgradeFilesCatalog — каталог с upgrade-fixture из src, раскладывающим
// files/<base> в каждый dst; failHook — хук upgrade завершается 1.
func upgradeFilesCatalog(t *testing.T, src string, failHook bool, dsts ...string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("files:\n")
	for _, d := range dsts {
		b.WriteString("  - {src: files/" + filepath.Base(d) + ", dst: " + d + ", mode: \"0644\"}\n")
	}
	dir := installPatched(t, src, "upgrade-fixture", installAppend(b.String()))
	for _, d := range dsts {
		catalogWrite(t, dir, "upgrade-fixture", "files/"+filepath.Base(d), d+"\n")
	}
	if failHook {
		catalogWrite(t, dir, "upgrade-fixture", "hooks/upgrade", "#!/bin/sh\nexit 1\n")
	}
	return dir
}

func upgradeDsts(rec state.Record) []string {
	out := make([]string, 0, len(rec.Files))
	for _, f := range rec.Files {
		out = append(out, f.Dst)
	}
	return out
}

func TestUpgradeFailedTwiceKeepsPrev(t *testing.T) {
	t.Parallel()
	te := upgradeInstalled(t, upgradeCatalog(t, "exit 1"))
	for i := range 2 {
		_, err := upgradeRun(te, []string{"upgrade-fixture"}, false)
		wantCode(t, err, cerr.ExitHook, "hook.failed")
		if v := upgradeTreeVersion(t, te, te.L.ExtPrev("upgrade-fixture")); v != "1.0.0" {
			t.Fatalf("попытка %d: .prev = %s, ждали 1.0.0", i+1, v)
		}
		rec := installRecord(t, te, "upgrade-fixture")
		if rec.Status != state.StatusFailedUpgrade || rec.Version != "1.0.0" {
			t.Fatalf("попытка %d: запись %+v", i+1, rec)
		}
	}
	if n := hookCount(t, te, "upgrade-fixture", "install"); n != 1 {
		t.Errorf("install.count = %d", n)
	}
}

func TestUpgradeRemovesStaleFiles(t *testing.T) {
	t.Parallel()
	first := upgradeFilesCatalog(t, fixtureDir(t, "upgrade-fixture"), false, "/etc/upg/keep.conf", "/etc/upg/stale.conf")
	next := upgradeFilesCatalog(t, testdataDir(t, "catalog-next", "extensions", "upgrade-fixture"), false, "/etc/upg/keep.conf")
	te := upgradeFrom(t, first, next, "upgrade-fixture")
	if !installExists(filepath.Join(te.Root, "/etc/upg/stale.conf")) {
		t.Fatal("1.0.0 не разложила stale.conf")
	}
	if _, err := upgradeRun(te, []string{"upgrade-fixture"}, false); err != nil {
		t.Fatal(err)
	}
	if installExists(filepath.Join(te.Root, "/etc/upg/stale.conf")) {
		t.Error("устаревший файл остался после upgrade")
	}
	if !installExists(filepath.Join(te.Root, "/etc/upg/keep.conf")) {
		t.Error("файл новой версии удалён")
	}
	if got := upgradeDsts(installRecord(t, te, "upgrade-fixture")); !slices.Equal(got, []string{"/etc/upg/keep.conf"}) {
		t.Errorf("files в реестре: %v", got)
	}
}

func TestUpgradeFailedRecordsNewFiles(t *testing.T) {
	t.Parallel()
	next := upgradeFilesCatalog(t, testdataDir(t, "catalog-next", "extensions", "upgrade-fixture"), true, "/etc/upg/new.conf")
	te := upgradeFrom(t, testdataDir(t, "catalog"), next, "upgrade-fixture")
	_, err := upgradeRun(te, []string{"upgrade-fixture"}, false)
	wantCode(t, err, cerr.ExitHook, "hook.failed")
	rec := installRecord(t, te, "upgrade-fixture")
	if rec.Status != state.StatusFailedUpgrade {
		t.Fatalf("запись: %+v", rec)
	}
	// Разложенные новой версией файлы принадлежат расширению: remove их уберёт.
	if got := upgradeDsts(rec); !slices.Equal(got, []string{"/etc/upg/new.conf"}) {
		t.Errorf("files в записи failed-upgrade: %v", got)
	}
}

func TestUpgradeConfigureReason(t *testing.T) {
	t.Parallel()
	next := installPatched(t, fixtureDir(t, "configure-system"), "configure-system",
		func(s string) string { return strings.Replace(s, "version: 1.0.0", "version: 2.0.0", 1) })
	te := upgradeFrom(t, testdataDir(t, "catalog"), next, "configure-system")
	if _, err := upgradeRun(te, []string{"configure-system"}, false); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(readState(t, te, "configure-system", "configure.log")), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "reason=install ") || !strings.Contains(lines[1], "reason=upgrade ") {
		t.Errorf("configure.log: %q", lines)
	}
}

func TestUpgradeDropsConfigure(t *testing.T) {
	t.Parallel()
	next := installPatched(t, fixtureDir(t, "configure-system"), "configure-system", func(s string) string {
		s = strings.Replace(s, "version: 1.0.0", "version: 2.0.0", 1)
		return strings.Replace(s, "  configure: {stage: system, after: [network-online.target], wants: [network-online.target]}\n", "", 1)
	})
	if err := os.Remove(filepath.Join(next, "extensions", "configure-system", "hooks", "configure")); err != nil {
		t.Fatal(err)
	}
	te := upgradeFrom(t, testdataDir(t, "catalog"), next, "configure-system")
	link := filepath.Join(te.L.WantsDir("multi-user.target"), "cream-configure@configure-system.service")
	if !installExists(link) {
		t.Fatal("1.0.0 не включила configure")
	}
	res, err := upgradeRun(te, []string{"configure-system"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Configure != nil || installRecord(t, te, "configure-system").Configure != nil {
		t.Errorf("configure остался в записи: %+v", res[0].Configure)
	}
	if installExists(link) || installExists(te.L.DropIn("configure-system")) {
		t.Error("юнит configure не снят новой версией без configure")
	}
}

// upgradeClock — часы теста, которые можно двигать.
type upgradeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *upgradeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *upgradeClock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestUpgradeKeepsInstalledAtAndPhase(t *testing.T) {
	t.Parallel()
	te := installEngine(t, installSrc{"fixtures", testdataDir(t, "catalog-next")})
	clock := &upgradeClock{now: testNow}
	te.Now, te.Store = clock.Now, state.NewStore(te.L, clock.Now)
	seedInstalled(t, te, fixtureDir(t, "upgrade-fixture"), func(r *state.Record) {
		r.Source = state.SourceRef{Name: "fixtures", Type: "path"}
		r.Phase = PhaseBuild
	})
	clock.Add(time.Hour)
	if _, err := upgradeRun(te, []string{"upgrade-fixture"}, false); err != nil {
		t.Fatal(err)
	}
	rec := installRecord(t, te, "upgrade-fixture")
	if rec.Version != "2.0.0" || !rec.InstalledAt.Equal(testNow) || !rec.UpdatedAt.Equal(testNow.Add(time.Hour)) {
		t.Errorf("installed_at %v updated_at %v (версия %s)", rec.InstalledAt, rec.UpdatedAt, rec.Version)
	}
	if rec.Phase != PhaseBuild {
		t.Errorf("phase = %q, ждали унаследованную build", rec.Phase)
	}
}

func TestUpgradeAllSkipsWithoutSource(t *testing.T) {
	t.Parallel()
	te := installEngine(t)
	installMust(t, te, "noop")
	seedInstalled(t, te, fixtureDir(t, "lib-fixture"), func(r *state.Record) { r.Source = state.SourceRef{} })
	res, err := upgradeRun(te, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Name != "noop" {
		t.Errorf("результат --all: %+v", res)
	}
	if !slices.Contains(warningCodes(te), "upgrade.no_source") {
		t.Errorf("предупреждения %v", warningCodes(te))
	}
	// Поимённо — не пропуск, а 69.
	_, err = upgradeRun(te, []string{"lib-fixture"}, false)
	wantCode(t, err, cerr.ExitSource, "source.missing")
}

// upgradeRun — upgrade в прежней форме: результаты расширений и ошибка
// (фатальная или худшего исхода плана, Err).
func upgradeRun(te *testEngine, names []string, all bool) ([]InstallResult, error) {
	rep, err := te.Upgrade(context.Background(), UpgradeRequest{Names: names, All: all})
	if err != nil {
		return nil, err
	}
	return rep.Extensions, rep.Err()
}
