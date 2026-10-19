package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/config"
	"github.com/iamletenkov/cream/internal/lock"
	"github.com/iamletenkov/cream/internal/state"
)

// actionWrite подменяет файл действия name установленной фикстуры action-json.
func actionWrite(t *testing.T, te *testEngine, name, body string) {
	t.Helper()
	p := filepath.Join(te.L.ExtTree("action-json"), "actions", name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func actionRun(te *testEngine, ext, action string, args ...string) (ActionResult, error) {
	return te.RunAction(context.Background(), ActionRequest{Ext: ext, Action: action, Args: args})
}

func TestRunActionJSON(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "action-json"))

	res, err := actionRun(te, "action-json", "status")
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != "action-json/status" || res.ExitCode != 0 {
		t.Errorf("result = %+v", res)
	}
	var out map[string]any
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatalf("output %q: %v", res.Output, err)
	}
	if out["ext"] != "action-json" || out["phase"] != "live" {
		t.Errorf("output = %v", out)
	}
	// Без конверта stdout — ровно вывод действия.
	if got := te.Out.String(); got != "{\"ok\":true,\"ext\":\"action-json\",\"phase\":\"live\"}\n" {
		t.Errorf("stdout = %q", got)
	}

	// С конвертом stdout не печатается: объект — в result.output.
	te2 := newTestEngine(t)
	seedInstalled(t, te2, fixtureDir(t, "action-json"))
	res, err = te2.RunAction(context.Background(), ActionRequest{Ext: "action-json", Action: "status", Envelope: true})
	if err != nil || len(res.Output) == 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if te2.Out.String() != "" {
		t.Errorf("stdout с конвертом = %q", te2.Out.String())
	}
}

func TestRunActionText(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "action-json"))
	res, err := actionRun(te, "action-json", "echo-args", "--json", "a b")
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != nil || res.ExitCode != 0 {
		t.Errorf("result = %+v", res)
	}
	if got := te.Out.String(); got != "--json\na b\n" {
		t.Errorf("stdout = %q", got)
	}
}

func TestRunActionExitCodes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		ext      string
		action   string
		args     []string
		wantCode int    // код действия при успехе запуска
		wantExit int    // код cream при ошибке
		wantErr  string // error.code
	}{
		{name: "0", ext: "action-json", action: "exit-code", args: []string{"0"}},
		{name: "1 насквозь", ext: "action-json", action: "exit-code", args: []string{"1"}, wantCode: 1},
		{name: "63 насквозь", ext: "action-json", action: "exit-code", args: []string{"63"}, wantCode: 63},
		{name: "64 — протокол", ext: "action-json", action: "exit-code", args: []string{"64"}, wantExit: 76, wantErr: "action.protocol"},
		{name: "100 — протокол", ext: "action-bad-protocol", action: "exit-high", wantExit: 76, wantErr: "action.protocol"},
		{name: "не JSON", ext: "action-bad-protocol", action: "bad-json", wantExit: 76, wantErr: "action.protocol"},
		{name: "два объекта", ext: "action-bad-protocol", action: "two-objects", wantExit: 76, wantErr: "action.protocol"},
		{name: "массив", ext: "action-bad-protocol", action: "json-array", wantExit: 76, wantErr: "action.protocol"},
		{name: "нет действия", ext: "action-json", action: "nope", wantExit: 66, wantErr: "not_found.action"},
		{name: "нет расширения", ext: "absent", action: "status", wantExit: 66, wantErr: "not_found.extension"},
		{name: "плохое имя расширения", ext: "../x", action: "status", wantExit: 64, wantErr: "usage.ext_name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			te := newTestEngine(t)
			seedInstalled(t, te, fixtureDir(t, "action-json"))
			seedInstalled(t, te, fixtureDir(t, "action-bad-protocol"))
			res, err := actionRun(te, tc.ext, tc.action, tc.args...)
			if tc.wantExit != 0 {
				wantCode(t, err, tc.wantExit, tc.wantErr)
				if te.Out.String() != "" {
					t.Errorf("stdout при нарушении протокола = %q", te.Out.String())
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if res.ExitCode != tc.wantCode {
				t.Errorf("exit_code = %d, ждали %d", res.ExitCode, tc.wantCode)
			}
		})
	}
}

func TestRunActionJSONNonZeroEmpty(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "action-json"))
	// Сбой без вывода — допустим: output null, код насквозь.
	actionWrite(t, te, "status", "echo сбой >&2; exit 2")
	res, err := actionRun(te, "action-json", "status")
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 2 || res.Output != nil {
		t.Errorf("result = %+v", res)
	}
	// Успех без вывода — нарушение протокола.
	actionWrite(t, te, "status", "exit 0")
	_, err = actionRun(te, "action-json", "status")
	wantCode(t, err, 76, "action.protocol")
}

func TestRunActionSecret(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "action-json"))
	const secret = "S3cr3t-value"
	// Действие печатает свой cmdline и окружение в stderr, секрет — тоже
	// (фильтр маскирует), а полученный со stdin секрет сохраняет для сверки.
	actionWrite(t, te, "join", `set -eu
secret=$(cat)
printf '%s' "$secret" > "$CREAM_STATE_DIR/got"
tr '\0' ' ' < /proc/$$/cmdline >&2; echo >&2
env >&2
echo "leak: $secret" >&2
printf '{"joined":true,"proxy":"%s","action":"%s","output":"%s"}\n' "$FIXTURE_PROXY" "$CREAM_ACTION" "$CREAM_OUTPUT"`)
	te.Stdin = strings.NewReader(secret + "\n")
	res, err := actionRun(te, "action-json", "join", "FIXTURE_PROXY=proxy.example:443", "FIXTURE_SECRET=-")
	if err != nil {
		t.Fatalf("%v; stderr:\n%s", err, te.Err.String())
	}
	if got := readState(t, te, "action-json", "got"); got != secret {
		t.Errorf("stdin действия = %q, ждали секрет", got)
	}
	stderr := te.Err.String()
	if strings.Contains(stderr, secret) {
		t.Errorf("секрет в cmdline/окружении/stderr действия:\n%s", stderr)
	}
	if !strings.Contains(stderr, "leak: ***") {
		t.Errorf("stderr не прошёл фильтр секретов:\n%s", stderr)
	}
	if !strings.Contains(stderr, "FIXTURE_PROXY=proxy.example:443 FIXTURE_SECRET=-") {
		t.Errorf("cmdline без пар как есть:\n%s", stderr)
	}
	var out map[string]any
	if err := json.Unmarshal(res.Output, &out); err != nil {
		t.Fatal(err)
	}
	if out["proxy"] != "proxy.example:443" || out["action"] != "join" || out["output"] != "json" {
		t.Errorf("output = %v", out)
	}
}

func TestRunActionStdinPass(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "action-json"))
	// echo-args — stdin: none: действие получает /dev/null.
	actionWrite(t, te, "echo-args", "cat")
	te.Stdin = strings.NewReader("поток")
	if _, err := actionRun(te, "action-json", "echo-args"); err != nil {
		t.Fatal(err)
	}
	if te.Out.String() != "" {
		t.Errorf("stdin: none, а действие прочитало %q", te.Out.String())
	}
	// stdin: pass — поток cream как есть.
	mf := filepath.Join(te.L.ExtTree("action-json"), "extension.yaml")
	data, err := os.ReadFile(mf)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data),
		`echo-args: {summary: "печатает аргументы как есть", args: free, lock: shared}`,
		`echo-args: {summary: "печатает аргументы как есть", args: free, lock: shared, stdin: pass}`, 1))
	if err := os.WriteFile(mf, data, 0o644); err != nil {
		t.Fatal(err)
	}
	te.Stdin = strings.NewReader("поток")
	if _, err := actionRun(te, "action-json", "echo-args"); err != nil {
		t.Fatal(err)
	}
	if te.Out.String() != "поток" {
		t.Errorf("stdin: pass — действие получило %q", te.Out.String())
	}
}

func TestRunActionLock(t *testing.T) {
	t.Parallel()
	t.Run("none не ждёт занятую", func(t *testing.T) {
		t.Parallel()
		te := newTestEngine(t)
		seedInstalled(t, te, fixtureDir(t, "lock-restart"))
		holdExtLock(t, te, "lock-restart", lock.Exclusive)
		start := time.Now()
		if _, err := actionRun(te, "lock-restart", "render"); err != nil {
			t.Fatal(err)
		}
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("lock: none ждало %s", d)
		}
		if readState(t, te, "lock-restart", "render.log") == "" {
			t.Error("render не выполнился")
		}
	})
	t.Run("exclusive ждёт и падает 75", func(t *testing.T) {
		t.Parallel()
		te := newTestEngine(t)
		te.Locks = lock.NewLocker(te.L, 300*time.Millisecond, te.Env)
		seedInstalled(t, te, fixtureDir(t, "lock-restart"))
		holdExtLock(t, te, "lock-restart", lock.Shared)
		_, err := actionRun(te, "lock-restart", "set-managed", "env=prod")
		wantCode(t, err, 75, "lock.timeout")
		if readState(t, te, "lock-restart", "labels") != "" {
			t.Error("действие запустилось без блокировки")
		}
	})
	t.Run("shared уживается с shared", func(t *testing.T) {
		t.Parallel()
		te := newTestEngine(t)
		te.Locks = lock.NewLocker(te.L, 300*time.Millisecond, te.Env)
		seedInstalled(t, te, fixtureDir(t, "action-json"))
		holdExtLock(t, te, "action-json", lock.Shared)
		if _, err := actionRun(te, "action-json", "echo-args", "x"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("блокировка передаётся действию", func(t *testing.T) {
		t.Parallel()
		te := newTestEngine(t)
		seedInstalled(t, te, fixtureDir(t, "action-json"))
		actionWrite(t, te, "echo-args", `echo "$CREAM_LOCK_FD $CREAM_LOCK_MODE"`)
		if _, err := actionRun(te, "action-json", "echo-args"); err != nil {
			t.Fatal(err)
		}
		if got := te.Out.String(); got != "3 shared\n" {
			t.Errorf("CREAM_LOCK_FD/MODE = %q", got)
		}
		te.Out.buf.Reset()
		actionWrite(t, te, "exit-code", `echo "[${CREAM_LOCK_FD:-}]"`)
		if _, err := actionRun(te, "action-json", "exit-code"); err != nil {
			t.Fatal(err)
		}
		if got := te.Out.String(); got != "[]\n" {
			t.Errorf("lock: none получило блокировку: %q", got)
		}
	})
}

func TestRunActionPhaseAndRoot(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "action-json"))
	te.EnvVars["CREAM_PHASE"] = PhaseBuild
	_, err := actionRun(te, "action-json", "join", "FIXTURE_PROXY=p.example", "FIXTURE_SECRET=-")
	wantCode(t, err, 68, "action.phase")
	// status допускает build: CREAM_PHASE доходит до действия.
	res, err := actionRun(te, "action-json", "status")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.Output), `"phase":"build"`) {
		t.Errorf("output = %s", res.Output)
	}

	// root: проверка только на настоящем корне (Р9).
	te2 := newTestEngine(t)
	seedInstalled(t, te2, fixtureDir(t, "action-json"))
	if _, err := te2.RunAction(context.Background(), ActionRequest{Ext: "action-json", Action: "status", Euid: 1000}); err != nil {
		t.Errorf("не-root на временном корне: %v", err)
	}
}

func TestRunActionTimeout(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "action-json"))
	mf := filepath.Join(te.L.ExtTree("action-json"), "extension.yaml")
	data, err := os.ReadFile(mf)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data),
		`exit-code: {summary: "завершается кодом из аргумента", args: free, lock: none}`,
		`exit-code: {summary: "завершается кодом из аргумента", args: free, lock: none, timeout: 1s}`, 1))
	if err := os.WriteFile(mf, data, 0o644); err != nil {
		t.Fatal(err)
	}
	actionWrite(t, te, "exit-code", "exec sleep 30")
	_, err = actionRun(te, "action-json", "exit-code")
	wantCode(t, err, 79, "action.timeout")
}

func TestActions(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "action-json"))
	seedInstalled(t, te, fixtureDir(t, "noop"))
	res, err := te.Actions("action-json")
	if err != nil {
		t.Fatal(err)
	}
	if res.Ext != "action-json" || res.Version != "1.0.0" || len(res.Actions) != 4 {
		t.Fatalf("result = %+v", res)
	}
	names := []string{}
	for _, a := range res.Actions {
		names = append(names, a.Name)
	}
	if strings.Join(names, ",") != "echo-args,exit-code,join,status" {
		t.Errorf("порядок = %v", names)
	}
	join := res.Actions[2]
	if join.Args != "pairs" || join.Stdin != "secret" || join.Lock != "exclusive" || join.Output != "json" ||
		join.TimeoutSeconds != 30 || len(join.Params) != 2 || !join.Params[1].Secret || join.Params[0].Type != "host" {
		t.Errorf("join = %+v", join)
	}
	empty, err := te.Actions("noop")
	if err != nil || empty.Actions == nil || len(empty.Actions) != 0 {
		t.Errorf("noop: %+v, %v", empty, err)
	}
	_, err = te.Actions("absent")
	wantCode(t, err, 66, "not_found.extension")
}

func TestRunActionJSONOverLimit(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "action-json"))
	// Объект в начале и пробелы за пределом: обрезанный буфер сам по себе — валидный
	// JSON, отказ даёт только признак переполнения.
	actionWrite(t, te, "status", `printf '{"ok":true}'; head -c 5000000 /dev/zero | tr '\0' ' '`)
	res, err := actionRun(te, "action-json", "status")
	wantCode(t, err, cerr.ExitProtocol, "action.protocol")
	if res.Output != nil {
		t.Errorf("output при переполнении: %s", res.Output)
	}
	if n := len(te.Out.String()); n != 0 {
		t.Errorf("stdout при переполнении: %d байт", n)
	}
}

// TestRunActionSeesValueWrittenWhileWaiting — exclusive-действие, ждавшее
// lock/<ext> (её держит set), читает домен уже после неё: видит новое значение.
func TestRunActionSeesValueWrittenWhileWaiting(t *testing.T) {
	t.Parallel()
	dir := installPatched(t, fixtureDir(t, "action-json"), "action-json", func(s string) string {
		return strings.Replace(s, `exit-code: {summary: "завершается кодом из аргумента", args: free, lock: none}`,
			`exit-code: {summary: "завершается кодом из аргумента", args: free, lock: exclusive}`, 1)
	})
	te := newTestEngine(t)
	in := seedInstalled(t, te, filepath.Join(dir, "extensions", "action-json"))
	actionWrite(t, te, "exit-code", `printf '%s\n' "$FIXTURE_PROXY"`)
	release := holdExtLock(t, te, "action-json", lock.Exclusive)
	done := make(chan error, 1)
	go func() {
		_, err := actionRun(te, "action-json", "exit-code")
		done <- err
	}()
	time.Sleep(200 * time.Millisecond)
	// «set» под блокировкой: новое значение до её снятия.
	if _, err := te.Domain(in).Set([]config.Change{{Key: "FIXTURE_PROXY", Value: "proxy.example:443"}},
		config.SetOpts{AllowReadonly: true}); err != nil {
		t.Fatal(err)
	}
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := te.Out.String(); got != "proxy.example:443\n" {
		t.Errorf("действие видит %q, ждали значение, записанное до снятия блокировки", got)
	}
}

// actionWaitUpgrade запускает exclusive-действие exit-code фикстуры action-json,
// пока lock/<ext> держит «upgrade», и до снятия блокировки правит запись реестра
// mutate. Возвращает ошибку действия.
func actionWaitUpgrade(t *testing.T, te *testEngine, body string, mutate func(*state.Record)) error {
	t.Helper()
	dir := installPatched(t, fixtureDir(t, "action-json"), "action-json", func(s string) string {
		return strings.Replace(s, `exit-code: {summary: "завершается кодом из аргумента", args: free, lock: none}`,
			`exit-code: {summary: "завершается кодом из аргумента", args: free, lock: exclusive}`, 1)
	})
	in := seedInstalled(t, te, filepath.Join(dir, "extensions", "action-json"))
	actionWrite(t, te, "exit-code", body)
	release := holdExtLock(t, te, "action-json", lock.Exclusive)
	done := make(chan error, 1)
	go func() {
		_, err := actionRun(te, "action-json", "exit-code")
		done <- err
	}()
	time.Sleep(200 * time.Millisecond)
	rec := in.Record
	mutate(&rec)
	if err := te.Store.Put(rec); err != nil {
		t.Fatal(err)
	}
	release()
	return <-done
}

// TestRunActionUpgradeWhileWaiting — upgrade, сменивший дерево расширения за
// время ожидания lock/<ext>, — 75 lock.changed, даже если lock действия тот же
// (Р27): действие не запускается со старыми аргументами на новом дереве.
func TestRunActionUpgradeWhileWaiting(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	err := actionWaitUpgrade(t, te, `echo ran > "$CREAM_STATE_DIR/ran"`, func(r *state.Record) {
		r.Version, r.ExtDigest = "1.1.0", "sha256:"+strings.Repeat("0", 64)
	})
	wantCode(t, err, 75, "lock.changed")
	if readState(t, te, "action-json", "ran") != "" {
		t.Error("действие запустилось на сменившемся дереве")
	}
}

// TestRunActionRecordReadUnderLock — без смены дерева действие, ждавшее
// lock/<ext>, видит запись реестра, прочитанную уже под блокировкой
// (CREAM_EXT_VERSION — текущая версия).
func TestRunActionRecordReadUnderLock(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	err := actionWaitUpgrade(t, te, `printf '%s\n' "$CREAM_EXT_VERSION"`, func(r *state.Record) {
		r.Version = "7.7.7"
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := te.Out.String(); got != "7.7.7\n" {
		t.Errorf("CREAM_EXT_VERSION = %q, ждали версию записи под блокировкой", got)
	}
}
