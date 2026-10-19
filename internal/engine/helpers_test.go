package engine

// Общие помощники тестов движка (Task 12). После волны 4 файл заморожен:
// T13–T15 ими пользуются, но не правят (нужна правка — доклад контроллеру).

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/fsx"
	"github.com/iamletenkov/cream/internal/lock"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/paths"
	"github.com/iamletenkov/cream/internal/runner"
	"github.com/iamletenkov/cream/internal/semver"
	"github.com/iamletenkov/cream/internal/state"
	"github.com/iamletenkov/cream/internal/systemd"
)

// testdataRoot — фикстуры §14 в корне репозитория.
const testdataRoot = "../../testdata"

// testNow — часы тестов.
var testNow = time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)

// testLockTimeout — --lock-timeout тестового движка.
const testLockTimeout = 5 * time.Second

// syncBuffer — bytes.Buffer, безопасный для записи из нескольких горутин
// (вывод хуков пишет os/exec).
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// testEngine — Engine на корне t.TempDir() с фейковым systemd и реальным
// runner.Exec. Поля Engine доступны напрямую (встраивание); Sys, Out, Err,
// EnvVars можно менять до вызова операции.
type testEngine struct {
	*Engine
	// Root — корень машины (--root).
	Root string
	// Sys — фейковый systemctl; по умолчанию systemd не запущен (offline).
	Sys *systemd.Fake
	// Out и Err — stdout и stderr движка (вывод хуков — в Err).
	Out, Err *syncBuffer
	// EnvVars — окружение cream (Engine.Env читает отсюда).
	EnvVars map[string]string
}

// newTestEngine собирает движок на пустом временном корне. cream — 1.0.0
// (без platform.dev_build), блокировки — с таймаутом testLockTimeout.
func newTestEngine(t *testing.T) *testEngine {
	t.Helper()
	root := t.TempDir()
	l := paths.Layout{Root: root}
	te := &testEngine{Root: root, Sys: &systemd.Fake{State: systemd.StateOffline},
		Out: &syncBuffer{}, Err: &syncBuffer{}, EnvVars: map[string]string{}}
	env := func(k string) string { return te.EnvVars[k] }
	now := func() time.Time { return testNow }
	te.Engine = &Engine{
		L:         l,
		Store:     state.NewStore(l, now),
		Locks:     lock.NewLocker(l, testLockTimeout, env),
		Run:       runner.Exec{Grace: time.Second},
		Systemctl: te.Sys,
		Now:       now,
		Cream:     manifest.CreamBuild{Version: semver.Version{Major: 1}},
		Warn:      &cerr.Warnings{},
		LookPath:  func(string) (string, error) { return "", os.ErrNotExist },
		Env:       env,
		Stdin:     strings.NewReader(""),
		Stdout:    te.Out,
		Stderr:    te.Err,
	}
	return te
}

// systemdRunning переводит фейковый systemd в состояние state (running,
// starting, …): Running отвечает ok.
func (te *testEngine) systemdRunning(state string) {
	te.Sys.State, te.Sys.OK = state, true
}

// testdataDir — абсолютный путь testdata/<rel…> корня репозитория.
func testdataDir(t *testing.T, rel ...string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join(append([]string{testdataRoot}, rel...)...))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("фикстура %s: %v", p, err)
	}
	return p
}

// fixtureDir — каталог расширения-фикстуры testdata/catalog/extensions/<name>.
func fixtureDir(t *testing.T, name string) string {
	t.Helper()
	return testdataDir(t, "catalog", "extensions", name)
}

// seedInstalled кладёт дерево src (fixtureDir или своё) в /opt/cream/ext/<имя>
// и пишет запись реестра «installed», как после успешной live-установки:
// версия и kind — из манифеста, ext_digest — отпечаток дерева, configure — при
// хуке configure (стадия, multi-user.target). mutate правит запись до Put.
// Хуки дерева можно подменить потом через writeHook.
func seedInstalled(t *testing.T, te *testEngine, src string, mutate ...func(*state.Record)) Installed {
	t.Helper()
	m, err := manifest.Load(src, te.Cream, nil)
	if err != nil {
		t.Fatalf("манифест %s: %v", src, err)
	}
	tree := te.L.ExtTree(m.Name)
	if err := os.RemoveAll(tree); err != nil {
		t.Fatal(err)
	}
	if err := fsx.CopyTree(src, tree); err != nil {
		t.Fatalf("дерево %s: %v", m.Name, err)
	}
	digest, err := fsx.TreeDigest(tree)
	if err != nil {
		t.Fatal(err)
	}
	rec := state.Record{Name: m.Name, Version: m.Version.String(), Kind: string(m.Kind),
		Status: state.StatusInstalled, ExtDigest: digest, Phase: PhaseLive,
		Source:       state.SourceRef{Name: "test", Type: "path", Origin: src},
		CreamVersion: "1.0.0", InstalledAt: testNow}
	if h, ok := m.Hooks[manifest.HookConfigure]; ok {
		rec.Configure = &state.ConfigureRec{Stage: string(h.Stage), Target: "multi-user.target"}
	}
	for _, f := range mutate {
		f(&rec)
	}
	if err := te.Store.Put(rec); err != nil {
		t.Fatal(err)
	}
	got, ok, err := te.Store.Get(m.Name)
	if err != nil || !ok {
		t.Fatalf("реестр %s: ok=%v err=%v", m.Name, ok, err)
	}
	return Installed{Record: *got, Manifest: m}
}

// writeHook подменяет хук name установленного расширения ext скриптом body
// (#!/bin/sh добавляется), режим 0755. Запись — под syscall.ForkLock: иначе
// параллельный тест может форкнуться, пока файл открыт на запись, и унести
// дескриптор в потомка — exec хука тогда получит ETXTBSY.
func writeHook(t *testing.T, te *testEngine, ext, name, body string) {
	t.Helper()
	p := filepath.Join(te.L.ExtTree(ext), "hooks", name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	syscall.ForkLock.RLock()
	err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755)
	syscall.ForkLock.RUnlock()
	if err != nil {
		t.Fatal(err)
	}
}

// writeFile пишет файл под корнем машины (rel — системный путь, «/etc/…»).
func writeFile(t *testing.T, te *testEngine, rel, body string, perm os.FileMode) {
	t.Helper()
	p := filepath.Join(te.Root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), perm); err != nil {
		t.Fatal(err)
	}
}

// readState читает файл из CREAM_STATE_DIR расширения ("" — файла нет).
func readState(t *testing.T, te *testEngine, ext, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(te.L.StateDir(ext), name))
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// hookCount — счётчик вызовов хука фикстуры (<hook>.count), 0 — не вызывался.
func hookCount(t *testing.T, te *testEngine, ext, hook string) int {
	t.Helper()
	s := strings.TrimSpace(readState(t, te, ext, hook+".count"))
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatalf("%s/%s.count: %v", ext, hook, err)
	}
	return n
}

// wantCode проверяет, что err — ошибка cream с кодом выхода exit и error.code code.
func wantCode(t *testing.T, err error, exit int, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("ошибки нет, ждали %d %s", exit, code)
	}
	if got := cerr.ExitCode(err); got != exit {
		t.Fatalf("код выхода %d (%s: %v), ждали %d %s", got, cerr.CodeOf(err), err, exit, code)
	}
	if code != "" && cerr.CodeOf(err) != code {
		t.Fatalf("error.code %q (%v), ждали %q", cerr.CodeOf(err), err, code)
	}
}

// warningCodes — коды собранных предупреждений.
func warningCodes(te *testEngine) []string {
	var out []string
	for _, w := range te.Warn.List() {
		out = append(out, w.Code)
	}
	return out
}

// holdExtLock держит lock/<ext> в режиме m на отдельном Locker до конца теста
// или до вызова release.
func holdExtLock(t *testing.T, te *testEngine, ext string, m lock.Mode) (release func()) {
	t.Helper()
	h, err := lock.NewLocker(te.L, 0, nil).Ext(context.Background(), ext, m)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Release)
	return h.Release
}
