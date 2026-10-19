package engine

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/config"
	"github.com/iamletenkov/cream/internal/lock"
	"github.com/iamletenkov/cream/internal/paths"
	"github.com/iamletenkov/cream/internal/systemd"
)

func TestSetApplyHook(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "apply-hook"))
	res, err := te.Set(context.Background(), "apply-hook",
		[]config.Change{{Key: "LEVEL", Value: "high"}, {Key: "LIMIT", Value: "11"}}, SetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Apply != ApplyHook || !slices.Equal(res.ChangedKeys, []string{"LEVEL", "LIMIT"}) {
		t.Errorf("res = %+v", res)
	}
	if n := hookCount(t, te, "apply-hook", "apply"); n != 1 {
		t.Errorf("apply вызван %d раз", n)
	}
	if log := readState(t, te, "apply-hook", "apply.log"); !strings.Contains(log, "changed=LEVEL LIMIT ") {
		t.Errorf("CREAM_CHANGED_KEYS: %q", log)
	}
	if len(te.Sys.Calls()) != 0 {
		t.Errorf("systemctl вызван при хуке apply: %v", te.Sys.Calls())
	}

	// Повтор с теми же значениями ничего не меняет и apply не вызывает.
	res, err = te.Set(context.Background(), "apply-hook", []config.Change{{Key: "LEVEL", Value: "high"}}, SetOptions{})
	if err != nil || res.Changed || res.Apply != ApplyNone || len(res.ChangedKeys) != 0 {
		t.Errorf("повтор: %+v, %v", res, err)
	}
	if n := hookCount(t, te, "apply-hook", "apply"); n != 1 {
		t.Errorf("apply после повтора вызван %d раз", n)
	}
}

func TestSetApplyHookGetsLock(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "apply-hook"))
	writeHook(t, te, "apply-hook", "apply", `echo "$CREAM_LOCK_FD $CREAM_LOCK_MODE" > "$CREAM_STATE_DIR/lockenv"`)
	if _, err := te.Set(context.Background(), "apply-hook", []config.Change{{Key: "LEVEL", Value: "high"}}, SetOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(readState(t, te, "apply-hook", "lockenv")); got != "3 exclusive" {
		t.Errorf("CREAM_LOCK_FD/MODE = %q", got)
	}
}

func TestSetApplyHookFails(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "apply-hook"))
	writeHook(t, te, "apply-hook", "apply", "exit 1")
	res, err := te.Set(context.Background(), "apply-hook", []config.Change{{Key: "LEVEL", Value: "high"}}, SetOptions{})
	wantCode(t, err, cerr.ExitHook, "hook.failed")
	if !res.Changed {
		t.Errorf("res = %+v", res)
	}
	if v, err := te.Get("apply-hook", "LEVEL", false); err != nil || v != "high" {
		t.Errorf("запись не сохранена после сбоя apply: %q, %v", v, err)
	}
}

func TestSetConfigureRestart(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		state     string // "" — systemd не запущен
		opts      SetOptions
		wantApply string
		wantCalls []string
		wantWarn  bool
	}{
		{name: "running", state: "running", wantApply: ApplyQueued,
			wantCalls: []string{"is-system-running", "restart --no-block cream-configure@configure-system.service"}},
		{name: "wait", state: "degraded", opts: SetOptions{Wait: true}, wantApply: ApplyRestart,
			wantCalls: []string{"is-system-running", "restart cream-configure@configure-system.service"}},
		{name: "wait-starting", state: "starting", opts: SetOptions{Wait: true}, wantApply: ApplyQueued, wantWarn: true,
			wantCalls: []string{"is-system-running", "restart --no-block cream-configure@configure-system.service"}},
		{name: "no-systemd", wantApply: ApplyNone, wantCalls: []string{"is-system-running"}},
		{name: "no-apply", state: "running", opts: SetOptions{NoApply: true}, wantApply: ApplySkipped},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			te := newTestEngine(t)
			if tc.state != "" {
				te.systemdRunning(tc.state)
			}
			seedInstalled(t, te, fixtureDir(t, "configure-system"))
			res, err := te.Set(context.Background(), "configure-system", []config.Change{{Key: "MESSAGE", Value: "hi"}}, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if res.Apply != tc.wantApply {
				t.Errorf("apply = %q, want %q", res.Apply, tc.wantApply)
			}
			if got := te.Sys.Calls(); !slices.Equal(got, tc.wantCalls) {
				t.Errorf("systemctl = %v, want %v", got, tc.wantCalls)
			}
			marker, err := os.ReadFile(te.L.RunReason("configure-system"))
			restarted := tc.wantApply == ApplyQueued || tc.wantApply == ApplyRestart
			if restarted && (err != nil || strings.TrimSpace(string(marker)) != ReasonApply) {
				t.Errorf("маркер причины = %q, %v", marker, err)
			}
			if !restarted && err == nil {
				t.Errorf("маркер без перезапуска: %q", marker)
			}
			if gotWarn := slices.Contains(warningCodes(te), "configure.no_block"); gotWarn != tc.wantWarn {
				t.Errorf("предупреждения %v", warningCodes(te))
			}
			if n := hookCount(t, te, "configure-system", "configure"); n != 0 {
				t.Errorf("configure вызван напрямую %d раз", n)
			}
		})
	}
}

func TestSetRestartFails(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	te.systemdRunning("running")
	te.Sys.RestartErr = os.ErrDeadlineExceeded
	seedInstalled(t, te, fixtureDir(t, "configure-system"))
	_, err := te.Set(context.Background(), "configure-system", []config.Change{{Key: "MESSAGE", Value: "hi"}}, SetOptions{Wait: true})
	wantCode(t, err, cerr.ExitHook, "hook.failed")
}

func TestSetWaitsForDomainLock(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "with-config"))
	release := holdExtLock(t, te, "with-config", lock.Exclusive)
	done := make(chan error, 1)
	go func() {
		_, err := te.Set(context.Background(), "with-config", []config.Change{{Key: "COUNT", Value: "5"}}, SetOptions{})
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("set не ждал блокировку: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	if _, err := os.Stat(te.L.ConfigFile("with-config")); err == nil {
		t.Fatal("запись до получения блокировки")
	}
	release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if v, _ := te.Get("with-config", "COUNT", false); v != "5" {
		t.Errorf("COUNT = %q", v)
	}
}

func TestSetLockTimeout(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "with-config"))
	holdExtLock(t, te, "with-config", lock.Shared)
	te.Locks = lock.NewLocker(te.L, 50*time.Millisecond, te.Env)
	_, err := te.Set(context.Background(), "with-config", []config.Change{{Key: "COUNT", Value: "5"}}, SetOptions{})
	wantCode(t, err, cerr.ExitLock, "lock.timeout")
}

func TestSetRefusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		ext      string
		ch       []config.Change
		opts     SetOptions
		wantExit int
		wantCode string
	}{
		{name: "not-installed", ext: "noop", ch: []config.Change{{Key: "A", Value: "1"}}, wantExit: 66, wantCode: "not_found.extension"},
		{name: "bad-name", ext: "../x", wantExit: 64, wantCode: "usage.ext_name"},
		{name: "readonly", ext: "with-config", ch: []config.Change{{Key: "RO_KEY", Value: "x"}}, wantExit: 65, wantCode: "config.readonly"},
		{name: "invalid", ext: "with-config", ch: []config.Change{{Key: "COUNT", Value: "x"}}, wantExit: 65, wantCode: "config.invalid"},
		{name: "unknown", ext: "with-config", ch: []config.Change{{Key: "NOPE", Value: "x"}}, wantExit: 65, wantCode: "config.unknown_key"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			te := newTestEngine(t)
			seedInstalled(t, te, fixtureDir(t, "with-config"))
			_, err := te.Set(context.Background(), tc.ext, tc.ch, tc.opts)
			wantCode(t, err, tc.wantExit, tc.wantCode)
		})
	}
	t.Run("allow-readonly", func(t *testing.T) {
		t.Parallel()
		te := newTestEngine(t)
		seedInstalled(t, te, fixtureDir(t, "with-config"))
		res, err := te.Set(context.Background(), "with-config", []config.Change{{Key: "RO_KEY", Value: "x"}}, SetOptions{AllowReadonly: true})
		if err != nil || !res.Changed || res.Apply != ApplyNone {
			t.Errorf("%+v, %v", res, err)
		}
	})
}

// lockProbeSystemctl — фейковый systemctl, который в Restart проверяет,
// свободна ли lock/<ext>: юнит configure берёт её сам, держать её при
// перезапуске — взаимная блокировка.
type lockProbeSystemctl struct {
	*systemd.Fake
	l    paths.Layout
	ext  string
	held atomic.Bool
}

func (s *lockProbeSystemctl) Restart(ctx context.Context, unit string, noBlock bool) error {
	h, err := lock.NewLocker(s.l, 0, nil).Ext(ctx, s.ext, lock.Exclusive)
	if err != nil {
		s.held.Store(true)
	} else {
		h.Release()
	}
	return s.Fake.Restart(ctx, unit, noBlock)
}

func TestSetConfigureReleasesLockBeforeRestart(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	te.systemdRunning("running")
	probe := &lockProbeSystemctl{Fake: te.Sys, l: te.L, ext: "configure-system"}
	te.Systemctl = probe
	seedInstalled(t, te, fixtureDir(t, "configure-system"))
	res, err := te.Set(context.Background(), "configure-system", []config.Change{{Key: "MESSAGE", Value: "hi"}}, SetOptions{Wait: true})
	if err != nil || res.Apply != ApplyRestart {
		t.Fatalf("%+v, %v", res, err)
	}
	if probe.held.Load() {
		t.Error("lock/configure-system занята во время перезапуска cream-configure@")
	}
}

// TestSetReadsManifestUnderLock — set, ждавший lock/<ext> (её держит upgrade),
// проверяет ключи по манифесту, установленному к моменту захвата.
func TestSetReadsManifestUnderLock(t *testing.T) {
	t.Parallel()
	next := installPatched(t, fixtureDir(t, "with-config"), "with-config", func(s string) string {
		return strings.Replace(s, "    FLAG:", "    NEW_KEY: {type: str, default: a, description: \"ключ новой версии\"}\n    FLAG:", 1)
	})
	te := newTestEngine(t)
	seedInstalled(t, te, fixtureDir(t, "with-config"))
	release := holdExtLock(t, te, "with-config", lock.Exclusive)
	done := make(chan error, 1)
	go func() {
		_, err := te.Set(context.Background(), "with-config", []config.Change{{Key: "NEW_KEY", Value: "b"}}, SetOptions{})
		done <- err
	}()
	time.Sleep(200 * time.Millisecond)
	// «upgrade» под блокировкой: новое дерево до её снятия.
	seedInstalled(t, te, filepath.Join(next, "extensions", "with-config"))
	release()
	if err := <-done; err != nil {
		t.Fatalf("set после upgrade: %v", err)
	}
	if v, err := te.Get("with-config", "NEW_KEY", false); err != nil || v != "b" {
		t.Errorf("NEW_KEY = %q, %v", v, err)
	}
}
