package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/fsx"
	"github.com/iamletenkov/cream/internal/lock"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/place"
	"github.com/iamletenkov/cream/internal/state"
	"github.com/iamletenkov/cream/internal/systemd"
)

// removeExists — есть ли путь под корнем машины (rel — системный путь или
// уже полный путь движка).
func removeExists(t *testing.T, p string) bool {
	t.Helper()
	_, err := os.Lstat(p)
	if err == nil {
		return true
	}
	if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return false
}

// removeHasRecord — есть ли запись реестра ext.
func removeHasRecord(t *testing.T, te *testEngine, ext string) bool {
	t.Helper()
	_, ok, err := te.Store.Get(ext)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

// removeHistory — строки history.jsonl.
func removeHistory(t *testing.T, te *testEngine) []state.HistoryEntry {
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
			t.Fatalf("history %q: %v", line, err)
		}
		out = append(out, h)
	}
	return out
}

// removeSeedFilesMode ставит files-mode и раскладывает его files, как
// установка: записи FileRec — в реестре.
func removeSeedFilesMode(t *testing.T, te *testEngine) {
	t.Helper()
	const name = "files-mode"
	seedInstalled(t, te, fixtureDir(t, name), func(r *state.Record) {
		m, err := manifest.Load(te.L.ExtTree(name), te.Cream, nil)
		if err != nil {
			t.Fatal(err)
		}
		recs, err := place.Place(te.L, name, te.L.ExtTree(name), m.Files, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Files = recs
	})
}

// removeNoHookDir — расширение с хуком install, но без remove.
func removeNoHookDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "no-remove")
	if err := fsx.CopyTree(fixtureDir(t, "noop"), dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "hooks", "remove")); err != nil {
		t.Fatal(err)
	}
	y := "schema: cream/v1\nname: no-remove\nversion: 1.0.0\nsummary: без remove\ncream: \">=1.0.0\"\n" +
		"platform: {family: deb, arch: [amd64, arm64]}\nhooks:\n  install: {}\n"
	if err := os.WriteFile(filepath.Join(dir, "extension.yaml"), []byte(y), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRemoveRunsHookAndCleansUp(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "noop"))
	writeFile(t, te, "/etc/cream/noop/config", "X=1\n", 0o644)
	// Остатки прерванного upgrade тоже уходят.
	writeFile(t, te, "/opt/cream/ext/.noop.prev/extension.yaml", "old\n", 0o644)

	res, err := te.Remove(context.Background(), "noop", RemoveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Ext != "noop" || res.Version != "1.0.0" || !res.Hook || res.Purged || res.Forgotten ||
		len(res.Dependents) != 0 || res.Dependents == nil {
		t.Errorf("res = %+v", res)
	}
	if n := hookCount(t, te, "noop", "remove"); n != 1 {
		t.Errorf("remove вызван %d раз", n)
	}
	if log := readState(t, te, "noop", "remove.log"); !strings.Contains(log, "phase=live ") ||
		!strings.Contains(log, "version=1.0.0") {
		t.Errorf("окружение хука: %q", log)
	}
	if removeHasRecord(t, te, "noop") {
		t.Error("запись реестра осталась")
	}
	for _, p := range []string{te.L.ExtTree("noop"), te.L.ExtPrev("noop")} {
		if removeExists(t, p) {
			t.Errorf("%s остался", p)
		}
	}
	// Без --purge домен и состояние сохраняются.
	for _, p := range []string{te.L.ConfigFile("noop"), te.L.StateDir("noop")} {
		if !removeExists(t, p) {
			t.Errorf("%s удалён без --purge", p)
		}
	}
	h := removeHistory(t, te)
	if len(h) != 1 || h[0].Op != "remove" || h[0].Ext != "noop" || h[0].From != "1.0.0" || h[0].To != "" ||
		h[0].Status != RemoveStatusRemoved || h[0].Phase != PhaseLive {
		t.Errorf("history = %+v", h)
	}
}

func TestRemovePurge(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "with-config"))
	writeFile(t, te, "/etc/cream/with-config/config", "COUNT=7\n", 0o644)
	writeFile(t, te, "/etc/cream/with-config/secrets", "TOKEN=s\n", 0o600)
	writeFile(t, te, "/var/lib/cream/ext/with-config/data", "x", 0o644)

	res, err := te.Remove(context.Background(), "with-config", RemoveOptions{Purge: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Purged || !res.Hook {
		t.Errorf("res = %+v", res)
	}
	for _, p := range []string{te.L.EtcDir("with-config"), te.L.StateDir("with-config"), te.L.ExtTree("with-config")} {
		if removeExists(t, p) {
			t.Errorf("%s остался после --purge", p)
		}
	}
}

// Р11: files A в каталоге настроек B; purge B забирает файл, запись A
// становится висячей, и remove A её пропускает.
func TestRemovePurgeForeignFileThenOwner(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "noop"))
	removeSeedFilesMode(t, te)
	label := filepath.Join(te.Root, "etc/cream/noop/labels.d/files-mode")
	if !removeExists(t, label) {
		t.Fatal("метка files-mode не разложена")
	}

	if _, err := te.Remove(context.Background(), "noop", RemoveOptions{Purge: true}); err != nil {
		t.Fatal(err)
	}
	if removeExists(t, te.L.EtcDir("noop")) {
		t.Error("каталог noop остался после --purge")
	}
	if _, err := te.Remove(context.Background(), "files-mode", RemoveOptions{}); err != nil {
		t.Fatalf("remove files-mode после purge noop: %v", err)
	}
	if removeExists(t, filepath.Join(te.Root, "etc/files-mode/plain.conf")) {
		t.Error("файл files-mode остался")
	}
	if slices.Contains(warningCodes(te), place.CodeFileModified) {
		t.Errorf("лишнее предупреждение: %v", te.Warn.List())
	}
}

func TestRemoveModifiedFileKept(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	removeSeedFilesMode(t, te)
	plain := filepath.Join(te.Root, "etc/files-mode/plain.conf")
	if err := os.WriteFile(plain, []byte("правка оператора\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := te.Remove(context.Background(), "files-mode", RemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	if removeExists(t, plain) {
		t.Error("изменённый файл остался на месте")
	}
	if data, err := os.ReadFile(plain + place.ModifiedSuffix); err != nil || string(data) != "правка оператора\n" {
		t.Errorf("%s.cream-modified: %q, %v", plain, data, err)
	}
	for _, rel := range []string{"etc/files-mode/private.conf", "usr/local/bin/files-mode-tool",
		"etc/files-mode/by-bit.conf", "etc/cream/noop/labels.d/files-mode"} {
		if removeExists(t, filepath.Join(te.Root, rel)) {
			t.Errorf("%s остался", rel)
		}
	}
	if !slices.Contains(warningCodes(te), place.CodeFileModified) {
		t.Errorf("нет предупреждения %s: %v", place.CodeFileModified, te.Warn.List())
	}
}

func TestRemoveDependents(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		installed []string
		target    string
		force     bool
		wantDeps  []string
		wantErr   bool
	}{
		{name: "библиотека при потребителе", installed: []string{"lib-fixture", "depends-lib"},
			target: "lib-fixture", wantDeps: []string{"depends-lib"}, wantErr: true},
		{name: "библиотека с --force", installed: []string{"lib-fixture", "depends-lib"},
			target: "lib-fixture", force: true, wantDeps: []string{"depends-lib"}},
		{name: "библиотека без потребителей", installed: []string{"lib-fixture"}, target: "lib-fixture"},
		{name: "потребитель уходит первым", installed: []string{"lib-fixture", "depends-lib"}, target: "depends-lib"},
		{name: "единственный поставщик requires", installed: []string{"apply-hook", "requires-cap"},
			target: "apply-hook", wantDeps: []string{"requires-cap"}, wantErr: true},
		{name: "второй поставщик есть", installed: []string{"apply-hook", "conflict-cap", "requires-cap"},
			target: "apply-hook"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			te := newTestEngine(t)
			for _, n := range tc.installed {
				seedInstalled(t, te, fixtureDir(t, n))
			}
			res, err := te.Remove(context.Background(), tc.target, RemoveOptions{Force: tc.force})
			if tc.wantErr {
				wantCode(t, err, cerr.ExitGraph, "deps.dependents")
				d := cerr.DetailsOf(err)
				if got, _ := d["dependents"].([]string); !slices.Equal(got, tc.wantDeps) || d["ext"] != tc.target {
					t.Errorf("details = %v", d)
				}
				if !removeHasRecord(t, te, tc.target) || !removeExists(t, te.L.ExtTree(tc.target)) {
					t.Error("отказ тронул реестр или дерево")
				}
				if hookCount(t, te, tc.target, "remove") != 0 {
					t.Error("хук вызван при отказе")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := tc.wantDeps
			if want == nil {
				want = []string{}
			}
			if !slices.Equal(res.Dependents, want) {
				t.Errorf("Dependents = %v, want %v", res.Dependents, want)
			}
			if got := slices.Contains(warningCodes(te), "deps.dependents_forced"); got != (len(want) > 0) {
				t.Errorf("предупреждения: %v", te.Warn.List())
			}
			if removeHasRecord(t, te, tc.target) {
				t.Error("запись осталась")
			}
		})
	}
}

func TestRemoveLibraryNeedsNoHook(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "lib-fixture"))
	res, err := te.Remove(context.Background(), "lib-fixture", RemoveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Hook || removeExists(t, te.L.ExtTree("lib-fixture")) {
		t.Errorf("res = %+v", res)
	}
}

func TestRemoveHookFailed(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	removeSeedFilesMode(t, te)
	writeHook(t, te, "files-mode", "remove", "exit 3")
	before, _, err := te.Store.Get("files-mode")
	if err != nil {
		t.Fatal(err)
	}

	_, err = te.Remove(context.Background(), "files-mode", RemoveOptions{Purge: true})
	wantCode(t, err, cerr.ExitHook, "hook.failed")
	if d := cerr.DetailsOf(err); d["hook_exit"] != 3 {
		t.Errorf("details = %v", d)
	}
	after, ok, err := te.Store.Get("files-mode")
	if err != nil || !ok || after.Status != before.Status || len(after.Files) != len(before.Files) {
		t.Errorf("реестр тронут: %+v, %v", after, err)
	}
	if !removeExists(t, te.L.ExtTree("files-mode")) || !removeExists(t, filepath.Join(te.Root, "etc/files-mode/plain.conf")) {
		t.Error("дерево или files удалены после сбоя хука")
	}
	h := removeHistory(t, te)
	if len(h) != 1 || h[0].Op != "remove" || h[0].Status != state.StatusFailed {
		t.Errorf("history = %+v", h)
	}
}

func TestRemoveHookReboot(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "noop"))
	writeHook(t, te, "noop", "remove", "exit 10")
	res, err := te.Remove(context.Background(), "noop", RemoveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.RebootRequired || !removeExists(t, te.L.RebootRequired()) || removeHasRecord(t, te, "noop") {
		t.Errorf("res = %+v", res)
	}
}

func TestRemoveMissingHook(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, removeNoHookDir(t))

	_, err := te.Remove(context.Background(), "no-remove", RemoveOptions{})
	wantCode(t, err, cerr.ExitData, "hook.missing_remove")
	if !strings.Contains(err.Error(), "--forget") {
		t.Errorf("нет подсказки: %v", err)
	}
	if !removeHasRecord(t, te, "no-remove") {
		t.Error("запись удалена при отказе")
	}

	res, err := te.Remove(context.Background(), "no-remove", RemoveOptions{Forget: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Forgotten || res.Hook || removeHasRecord(t, te, "no-remove") || removeExists(t, te.L.ExtTree("no-remove")) {
		t.Errorf("forget: %+v", res)
	}
	if h := removeHistory(t, te); len(h) != 1 || h[0].Status != RemoveStatusForgotten {
		t.Errorf("history = %+v", h)
	}
}

func TestRemoveForgetSkipsHook(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	removeSeedFilesMode(t, te)
	res, err := te.Remove(context.Background(), "files-mode", RemoveOptions{Forget: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Hook || hookCount(t, te, "files-mode", "remove") != 0 {
		t.Errorf("хук вызван при --forget: %+v", res)
	}
	if removeExists(t, filepath.Join(te.Root, "etc/files-mode/plain.conf")) || removeExists(t, te.L.ExtTree("files-mode")) {
		t.Error("артефакты cream остались после --forget")
	}
}

// Дерево не читается: remove без --forget отказывает, --forget убирает остальное.
func TestRemoveForgetBrokenTree(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "noop"))
	if err := os.Remove(filepath.Join(te.L.ExtTree("noop"), "extension.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := te.Remove(context.Background(), "noop", RemoveOptions{}); err == nil {
		t.Fatal("remove с битым деревом прошёл без --forget")
	}
	if _, err := te.Remove(context.Background(), "noop", RemoveOptions{Forget: true}); err != nil {
		t.Fatal(err)
	}
	if removeHasRecord(t, te, "noop") || removeExists(t, te.L.ExtTree("noop")) {
		t.Error("--forget не убрал битую установку")
	}
}

func TestRemoveDisablesConfigure(t *testing.T) {
	t.Parallel()
	for _, running := range []bool{false, true} {
		t.Run(map[bool]string{false: "offline", true: "running"}[running], func(t *testing.T) {
			t.Parallel()
			te := newTestEngine(t)
			seedInstalled(t, te, fixtureDir(t, "configure-system"))
			target, err := systemd.Enable(te.L, "configure-system", systemd.StageSpec{Stage: "system"})
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, te, "/run/cream/configure-system.reason", "install\n", 0o644)
			link := filepath.Join(te.L.WantsDir(target), systemd.UnitName("configure-system"))
			if !removeExists(t, link) || !removeExists(t, te.L.DropIn("configure-system")) {
				t.Fatal("Enable не включил экземпляр")
			}
			if running {
				te.systemdRunning("running")
			}
			if _, err := te.Remove(context.Background(), "configure-system", RemoveOptions{}); err != nil {
				t.Fatal(err)
			}
			for _, p := range []string{link, te.L.DropIn("configure-system"), te.L.RunReason("configure-system")} {
				if removeExists(t, p) {
					t.Errorf("%s остался", p)
				}
			}
			if got := slices.Contains(te.Sys.Calls(), "daemon-reload"); got != running {
				t.Errorf("daemon-reload = %v, calls %v", got, te.Sys.Calls())
			}
		})
	}
}

func TestRemoveRefusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		ext  string
		o    RemoveOptions
		exit int
		code string
	}{
		{name: "не установлено", ext: "noop", exit: cerr.ExitNotFound, code: "not_found.extension"},
		{name: "не установлено с --forget", ext: "noop", o: RemoveOptions{Forget: true},
			exit: cerr.ExitNotFound, code: "not_found.extension"},
		{name: "недопустимое имя", ext: "../etc", exit: cerr.ExitUsage, code: "usage.ext_name"},
		{name: "--purge и --forget", ext: "noop", o: RemoveOptions{Purge: true, Forget: true},
			exit: cerr.ExitUsage, code: "usage.flag"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			te := newTestEngine(t)
			_, err := te.Remove(context.Background(), tc.ext, tc.o)
			wantCode(t, err, tc.exit, tc.code)
		})
	}
}

func TestRemoveWaitsForExtLock(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "noop"))
	te.Locks = lock.NewLocker(te.L, 200*time.Millisecond, te.Env)
	holdExtLock(t, te, "noop", lock.Shared)
	_, err := te.Remove(context.Background(), "noop", RemoveOptions{})
	wantCode(t, err, cerr.ExitLock, "")
	if !removeHasRecord(t, te, "noop") || hookCount(t, te, "noop", "remove") != 0 {
		t.Error("remove прошёл мимо занятой lock/<ext>")
	}
}

func TestRemoveHookGetsLock(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "noop"))
	writeHook(t, te, "noop", "remove", `echo "$CREAM_LOCK_FD $CREAM_LOCK_MODE" > "$CREAM_STATE_DIR/lockenv"`)
	if _, err := te.Remove(context.Background(), "noop", RemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(readState(t, te, "noop", "lockenv")); got != "3 exclusive" {
		t.Errorf("CREAM_LOCK_FD/MODE = %q", got)
	}
}
