package lock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/paths"
)

// noEnv is an environment without CREAM_LOCK_FD.
func noEnv(string) string { return "" }

func newLocker(t *testing.T, l paths.Layout, timeout time.Duration) *Locker {
	t.Helper()
	return NewLocker(l, timeout, noEnv)
}

func layout(t *testing.T) paths.Layout {
	t.Helper()
	return paths.Layout{Root: t.TempDir()}
}

func mustExt(t *testing.T, lk *Locker, ext string, m Mode) *Handle {
	t.Helper()
	h, err := lk.Ext(context.Background(), ext, m)
	if err != nil {
		t.Fatalf("Ext(%s, %v): %v", ext, m, err)
	}
	t.Cleanup(h.Release)
	return h
}

func assertTimeout(t *testing.T, err error) {
	const code = "lock.timeout"
	t.Helper()
	if cerr.ExitCode(err) != cerr.ExitLock || cerr.CodeOf(err) != code {
		t.Fatalf("err = %v (exit %d, code %q), want 75 %s", err, cerr.ExitCode(err), cerr.CodeOf(err), code)
	}
}

// Compatibility of an already held lock with a new request, without waiting
// (timeout 0 is a single attempt).
func TestCompatibility(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		held, req Mode
		ok        bool
	}{
		{name: "shared with shared", held: Shared, req: Shared, ok: true},
		{name: "exclusive blocks exclusive", held: Exclusive, req: Exclusive},
		{name: "exclusive blocks shared", held: Exclusive, req: Shared},
		{name: "shared blocks exclusive", held: Shared, req: Exclusive},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l := layout(t)
			mustExt(t, newLocker(t, l, 0), "teleport", tc.held)
			h, err := newLocker(t, l, 0).Ext(context.Background(), "teleport", tc.req)
			if tc.ok {
				if err != nil {
					t.Fatalf("Ext: %v", err)
				}
				h.Release()
				return
			}
			assertTimeout(t, err)
		})
	}
}

func TestDomainsAreIndependent(t *testing.T) {
	t.Parallel()
	l := layout(t)
	lk := newLocker(t, l, 0)
	g, err := lk.Global(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer g.Release()
	mustExt(t, lk, "a", Exclusive)
	mustExt(t, lk, "b", Exclusive)
	if _, err := lk.Global(context.Background()); err == nil {
		t.Fatal("second Global: want 75")
	} else {
		assertTimeout(t, err)
	}
	st, err := os.Stat(l.GlobalLock())
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("lock file mode = %o, want 600", st.Mode().Perm())
	}
}

func TestExclusiveWaitsForRelease(t *testing.T) {
	t.Parallel()
	l := layout(t)
	held := mustExt(t, newLocker(t, l, 0), "teleport", Exclusive)
	waiter := newLocker(t, l, time.Minute)
	got := make(chan error, 1)
	go func() {
		h, err := waiter.Ext(context.Background(), "teleport", Exclusive)
		if err == nil {
			h.Release()
		}
		got <- err
	}()
	// The waiter must still be polling after a short, explicit pause: it
	// cannot finish while the lock is held.
	select {
	case err := <-got:
		t.Fatalf("waiter finished while the lock was held: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	held.Release()
	select {
	case err := <-got:
		if err != nil {
			t.Fatalf("Ext after release: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("waiter did not get the lock after release")
	}
}

func TestTimeout(t *testing.T) {
	t.Parallel()
	l := layout(t)
	mustExt(t, newLocker(t, l, 0), "teleport", Exclusive)
	const timeout = 50 * time.Millisecond
	start := time.Now()
	_, err := newLocker(t, l, timeout).Ext(context.Background(), "teleport", Shared)
	assertTimeout(t, err)
	if el := time.Since(start); el < timeout {
		t.Errorf("gave up after %v, before the timeout %v", el, timeout)
	}
	if d := cerr.DetailsOf(err); d["lock"] != "teleport" {
		t.Errorf("details = %v, want lock=teleport", d)
	}
}

func TestContextCanceled(t *testing.T) {
	t.Parallel()
	l := layout(t)
	mustExt(t, newLocker(t, l, 0), "teleport", Exclusive)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newLocker(t, l, time.Minute).Ext(ctx, "teleport", Exclusive)
	if cerr.ExitCode(err) != cerr.ExitLock || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want 75 wrapping context.Canceled", err)
	}
}

func TestReleaseIsIdempotentAndFrees(t *testing.T) {
	t.Parallel()
	l := layout(t)
	lk := newLocker(t, l, 0)
	h := mustExt(t, lk, "teleport", Exclusive)
	if h.File() == nil {
		t.Fatal("File() = nil")
	}
	h.Release()
	h.Release()
	mustExt(t, lk, "teleport", Exclusive)
}

func TestBadExtName(t *testing.T) {
	t.Parallel()
	lk := newLocker(t, layout(t), 0)
	for _, name := range []string{"", "global", ".x", "a/b", ".."} {
		if _, err := lk.Ext(context.Background(), name, Exclusive); err == nil {
			t.Errorf("Ext(%q): want error", name)
		}
	}
}

// CREAM_LOCK_FD in the same process: an fd of the same lock file is taken
// over without waiting; anything else is ignored.
func TestInheritedFDInProcess(t *testing.T) {
	t.Parallel()
	l := layout(t)
	parent := newLocker(t, l, 0)
	held := mustExt(t, parent, "teleport", Exclusive)
	other := mustExt(t, parent, "other", Exclusive)
	fd := func(h *Handle) string { return strconv.Itoa(int(h.File().Fd())) }
	tests := []struct {
		name  string
		value string
		ok    bool
	}{
		{name: "same file", value: fd(held), ok: true},
		{name: "foreign lock file", value: fd(other)},
		{name: "not a number", value: "three"},
		{name: "negative", value: "-1"},
		{name: "closed fd", value: "987654"},
		{name: "empty", value: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := func(k string) string {
				switch k {
				case EnvLockFD:
					return tc.value
				case EnvLockMode:
					return "exclusive"
				}
				return ""
			}
			h, err := NewLocker(l, 0, env).Ext(context.Background(), "teleport", Exclusive)
			if !tc.ok {
				assertTimeout(t, err)
				return
			}
			if err != nil {
				t.Fatalf("Ext with inherited fd: %v", err)
			}
			if !h.Inherited() {
				t.Error("Inherited() = false")
			}
			// Releasing the inherited handle must not drop the parent's lock.
			h.Release()
			if _, err := newLocker(t, l, 0).Ext(context.Background(), "teleport", Shared); err == nil {
				t.Error("parent's lock was released by the child handle")
			}
		})
	}
}

// A real nested cream: a child process gets the fd through ExtraFiles
// (fd 3) and CREAM_LOCK_FD=3.
func TestInheritedFDChildProcess(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		pass     string // lock whose fd is passed: "teleport" (same) or "other" (foreign)
		held     Mode   // mode the parent holds teleport in (and passes in CREAM_LOCK_MODE)
		wantExit int
	}{
		{name: "same file — no wait", pass: "teleport", held: Exclusive, wantExit: 0},
		{name: "foreign file — ignored, normal wait times out", pass: "other", held: Exclusive, wantExit: cerr.ExitLock},
		{name: "shared parent — no upgrade to exclusive", pass: "teleport", held: Shared, wantExit: cerr.ExitLock},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l := layout(t)
			parent := newLocker(t, l, 0)
			handles := map[string]*Handle{
				"teleport": mustExt(t, parent, "teleport", tc.held),
				"other":    mustExt(t, parent, "other", Exclusive),
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestHelperChild$")
			cmd.Env = []string{
				"CREAM_LOCK_HELPER=1",
				"CREAM_LOCK_HELPER_ROOT=" + l.Root,
				EnvLockFD + "=3",
				EnvLockMode + "=" + tc.held.String(),
				"GORACE=atexit_sleep_ms=0",
			}
			cmd.ExtraFiles = []*os.File{handles[tc.pass].File()}
			out, err := cmd.CombinedOutput()
			code := 0
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				code = ee.ExitCode()
			} else if err != nil {
				t.Fatal(err)
			}
			if code != tc.wantExit {
				t.Fatalf("child exit = %d, want %d; output:\n%s", code, tc.wantExit, out)
			}
			// The parent still holds its lock after the child exited.
			if _, err := newLocker(t, l, 0).Ext(context.Background(), "teleport", Exclusive); err == nil {
				t.Error("parent's lock is gone after the child exited")
			}
		})
	}
}

// TestHelperChild is the nested cream of TestInheritedFDChildProcess; it does
// nothing in a normal run.
func TestHelperChild(t *testing.T) {
	if os.Getenv("CREAM_LOCK_HELPER") != "1" {
		t.Skip("helper process only")
	}
	l := paths.Layout{Root: os.Getenv("CREAM_LOCK_HELPER_ROOT")}
	h, err := NewLocker(l, 0, os.Getenv).Ext(context.Background(), "teleport", Exclusive)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(cerr.ExitCode(err))
	}
	h.Release()
	os.Exit(0)
}

func lockEnv(fd int, mode string) func(string) string {
	return func(k string) string {
		switch k {
		case EnvLockFD:
			return strconv.Itoa(fd)
		case EnvLockMode:
			return mode
		}
		return ""
	}
}

// Р19: the mode the parent holds (CREAM_LOCK_MODE) bounds what the child may
// take over: shared cannot be upgraded to exclusive (75 lock.upgrade); an
// unknown or missing mode counts as shared.
func TestInheritedMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		held     Mode
		envMode  string
		req      Mode
		wantCode string // "" — inherited
	}{
		{name: "exclusive covers exclusive", held: Exclusive, envMode: "exclusive", req: Exclusive},
		{name: "exclusive covers shared", held: Exclusive, envMode: "exclusive", req: Shared},
		{name: "shared covers shared", held: Shared, envMode: "shared", req: Shared},
		{name: "shared is not upgraded", held: Shared, envMode: "shared", req: Exclusive, wantCode: "lock.upgrade"},
		{name: "missing mode is shared", held: Exclusive, envMode: "", req: Exclusive, wantCode: "lock.upgrade"},
		{name: "unknown mode is shared", held: Exclusive, envMode: "EXCLUSIVE", req: Shared},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l := layout(t)
			held := mustExt(t, newLocker(t, l, 0), "teleport", tc.held)
			h, err := NewLocker(l, time.Minute, lockEnv(int(held.File().Fd()), tc.envMode)).
				Ext(context.Background(), "teleport", tc.req)
			if tc.wantCode != "" {
				if cerr.ExitCode(err) != cerr.ExitLock || cerr.CodeOf(err) != tc.wantCode {
					t.Fatalf("err = %v, want 75 %s", err, tc.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("Ext: %v", err)
			}
			defer h.Release()
			if !h.Inherited() {
				t.Error("Inherited() = false")
			}
		})
	}
}

// Р19: a CREAM_LOCK_FD of the same file that nobody holds a lock on is a false
// inheritance: the child takes the lock for real, on its own descriptor.
func TestInheritedFDNotHeld(t *testing.T) {
	t.Parallel()
	for _, m := range []Mode{Exclusive, Shared} {
		t.Run(m.String(), func(t *testing.T) {
			t.Parallel()
			l := layout(t)
			// Create the lock file, then drop the lock but keep an open fd.
			mustExt(t, newLocker(t, l, 0), "teleport", Exclusive).Release()
			f, err := os.Open(l.ExtLock("teleport"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = f.Close() }()
			h, err := NewLocker(l, 0, lockEnv(int(f.Fd()), "exclusive")).Ext(context.Background(), "teleport", m)
			if err != nil {
				t.Fatalf("Ext: %v", err)
			}
			defer h.Release()
			if h.Inherited() {
				t.Fatal("Inherited() = true for an fd nobody locks")
			}
			if h.File() == nil || h.File().Fd() == f.Fd() {
				t.Fatal("lock must sit on a descriptor of its own")
			}
			// The lock is really held in mode m.
			_, err = newLocker(t, l, 0).Ext(context.Background(), "teleport", Exclusive)
			assertTimeout(t, err)
			other, err := newLocker(t, l, 0).Ext(context.Background(), "teleport", Shared)
			if m == Shared {
				if err != nil {
					t.Fatalf("shared next to shared: %v", err)
				}
				other.Release()
			} else {
				assertTimeout(t, err)
			}
			h.Release()
			mustExt(t, newLocker(t, l, 0), "teleport", Exclusive)
		})
	}
}
