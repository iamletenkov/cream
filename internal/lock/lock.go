// Package lock holds cream's flock(2) locks (spec §6.3).
//
// There are two kinds of lock files under /var/lib/cream/lock:
//   - global — exclusive, held by install, upgrade, remove and assume;
//   - <ext>  — the settings domain of one extension, exclusive for set, apply,
//     configure and actions with "lock: exclusive", shared for "lock: shared".
//
// Order is always global first, then <ext>; a caller that needs both takes
// them in this order and releases in reverse. Actions with "lock: none" take
// nothing (the engine simply does not call the Locker).
//
// A nested cream started by a hook or an action inherits the domain lock: the
// parent passes the descriptor of lock/<ext> in CREAM_LOCK_FD and the mode it
// holds in CREAM_LOCK_MODE. The child checks that the descriptor is the same
// file (device and inode), then tries LOCK_EX|LOCK_NB on a descriptor of its
// own: success means nobody holds the lock — the inheritance is false and the
// child keeps a real lock on its descriptor; EWOULDBLOCK means the lock is
// held and the child takes it over without waiting, unless it asks for
// exclusive while the parent holds shared (75 lock.upgrade, ruling Р19).
// Only the domain lock is inherited (ruling Р2): a nested operation that
// needs global waits for it and fails with 75.
package lock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/paths"
)

// EnvLockFD is the variable a parent sets to the descriptor of lock/<ext>.
const EnvLockFD = "CREAM_LOCK_FD"

// EnvLockMode is the mode the parent holds lock/<ext> in: "shared" or
// "exclusive"; anything else (or nothing) counts as shared (ruling Р19).
const EnvLockMode = "CREAM_LOCK_MODE"

// GlobalName is the reserved name of the global lock file (ruling Р15).
const GlobalName = "global"

// Mode is the flock mode of a domain lock.
type Mode int

const (
	// Exclusive is flock LOCK_EX; the default for actions.
	Exclusive Mode = iota
	// Shared is flock LOCK_SH.
	Shared
)

// String returns "exclusive" or "shared".
func (m Mode) String() string {
	if m == Shared {
		return "shared"
	}
	return "exclusive"
}

const (
	pollFirst   = 5 * time.Millisecond
	pollLongest = 100 * time.Millisecond
)

// Locker takes cream's locks. Timeout is --lock-timeout: how long to wait for
// a busy lock; zero or less means a single attempt.
type Locker struct {
	layout  paths.Layout
	timeout time.Duration
	env     func(string) string
}

// NewLocker returns a Locker for the layout. env is the process environment
// (os.Getenv in production); only CREAM_LOCK_FD is read from it.
func NewLocker(l paths.Layout, timeout time.Duration, env func(string) string) *Locker {
	if env == nil {
		env = func(string) string { return "" }
	}
	return &Locker{layout: l, timeout: timeout, env: env}
}

// Handle is a held lock.
type Handle struct {
	mu        sync.Mutex
	f         *os.File
	inherited bool
}

// File returns the descriptor of the lock file, to be passed to a child in
// ExtraFiles together with CREAM_LOCK_FD. It is nil after Release.
func (h *Handle) File() *os.File {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.f
}

// Inherited reports whether the lock came from CREAM_LOCK_FD.
func (h *Handle) Inherited() bool { return h.inherited }

// Release drops the lock; repeated calls do nothing. An inherited handle only
// closes its own duplicate of the descriptor: the lock stays with the parent.
func (h *Handle) Release() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.f == nil {
		return
	}
	if !h.inherited {
		_ = flock(int(h.f.Fd()), syscall.LOCK_UN)
	}
	_ = h.f.Close()
	h.f = nil
}

// Global takes lock/global exclusively. CREAM_LOCK_FD is never used for it.
func (l *Locker) Global(ctx context.Context) (*Handle, error) {
	return l.acquire(ctx, GlobalName, l.layout.GlobalLock(), Exclusive)
}

// Ext takes the domain lock of ext in mode m. A CREAM_LOCK_FD that refers to
// the same, actually held lock file is taken over without waiting when the
// parent's CREAM_LOCK_MODE covers m; any other value is ignored.
func (l *Locker) Ext(ctx context.Context, ext string, m Mode) (*Handle, error) {
	if err := checkName(ext); err != nil {
		return nil, err
	}
	path := l.layout.ExtLock(ext)
	h, err := l.inherit(ext, path, m)
	if h != nil || err != nil {
		return h, err
	}
	return l.acquire(ctx, ext, path, m)
}

func checkName(ext string) error {
	if ext == "" || ext == GlobalName || ext == "." || ext == ".." || strings.HasPrefix(ext, ".") ||
		strings.ContainsAny(ext, "/\x00") {
		return cerr.New(cerr.ExitInternal, cerr.CodeInternal, "недопустимое имя блокировки %q", ext)
	}
	return nil
}

// inherit handles CREAM_LOCK_FD. It returns (nil, nil) when the variable is
// absent or refers to another file — the caller then waits as usual. When the
// file is the same it probes a descriptor of its own with LOCK_EX|LOCK_NB:
// taken — nobody held the lock: exclusive keeps the probe as the real lock,
// shared closes it and returns (nil, nil), so the caller waits as usual;
// busy — the lock is held, and the handle is a duplicate of the inherited
// descriptor, provided the parent's mode covers m. The original
// descriptor gets close-on-exec so that it does not leak into hooks unless
// passed explicitly.
func (l *Locker) inherit(name, path string, m Mode) (*Handle, error) {
	v := l.env(EnvLockFD)
	if v == "" {
		return nil, nil
	}
	fd, err := strconv.Atoi(v)
	if err != nil || fd < 0 {
		return nil, nil
	}
	var fst, pst syscall.Stat_t
	if syscall.Fstat(fd, &fst) != nil || syscall.Stat(path, &pst) != nil {
		return nil, nil
	}
	if fst.Dev != pst.Dev || fst.Ino != pst.Ino || fst.Mode&syscall.S_IFMT != syscall.S_IFREG {
		return nil, nil
	}
	syscall.CloseOnExec(fd)
	probe, err := os.OpenFile(path, os.O_RDWR|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, cerr.Wrap(err, cerr.ExitState, "state.lock_file", "файл блокировки %s", path)
	}
	err = flock(int(probe.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	switch {
	case err == nil:
		// False inheritance: nobody holds the lock. Exclusive keeps the probe.
		// Shared does not convert EX→SH in place: flock(2) conversion is not
		// atomic (the lock is dropped first), another process may take EX in
		// the gap and LOCK_NB would fail. The probe is closed and the caller
		// waits for SH as usual (acquire, --lock-timeout). The gap cannot be
		// hit deterministically in a test; TestInheritedFDNotHeld/shared
		// checks the resulting lock.
		if m == Shared {
			_ = probe.Close()
			return nil, nil
		}
		return &Handle{f: probe}, nil
	case !errors.Is(err, syscall.EWOULDBLOCK):
		_ = probe.Close()
		return nil, cerr.Wrap(err, cerr.ExitState, "state.lock_file", "flock %s", name)
	}
	_ = probe.Close()
	held := Shared
	if l.env(EnvLockMode) == Exclusive.String() {
		held = Exclusive
	}
	if m == Exclusive && held == Shared {
		e := cerr.New(cerr.ExitLock, "lock.upgrade",
			"блокировка %s унаследована разделяемой: эксклюзивный захват невозможен", name)
		e.Details = map[string]any{"lock": name, "mode": m.String(), "held": held.String()}
		return nil, e
	}
	syscall.ForkLock.RLock()
	dup, err := syscall.Dup(fd)
	if err == nil {
		syscall.CloseOnExec(dup)
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return nil, cerr.Wrap(err, cerr.ExitState, "state.lock_file", "dup %s", EnvLockFD)
	}
	return &Handle{f: os.NewFile(uintptr(dup), path), inherited: true}, nil
}

func (l *Locker) acquire(ctx context.Context, name, path string, m Mode) (*Handle, error) {
	if err := os.MkdirAll(l.layout.LockDir(), 0o755); err != nil {
		return nil, cerr.Wrap(err, cerr.ExitState, "state.lock_dir", "каталог блокировок %s", l.layout.LockDir())
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, cerr.Wrap(err, cerr.ExitState, "state.lock_file", "файл блокировки %s", path)
	}
	if err := l.wait(ctx, int(f.Fd()), name, m); err != nil {
		_ = f.Close()
		return nil, err
	}
	return &Handle{f: f}, nil
}

// wait polls a non-blocking flock until it succeeds, the timeout passes or ctx
// ends: a blocking flock could not be interrupted by either.
func (l *Locker) wait(ctx context.Context, fd int, name string, m Mode) error {
	how := syscall.LOCK_EX
	if m == Shared {
		how = syscall.LOCK_SH
	}
	deadline := time.Now().Add(l.timeout)
	delay := pollFirst
	for {
		err := flock(fd, how|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return cerr.Wrap(err, cerr.ExitState, "state.lock_file", "flock %s", name)
		}
		left := time.Until(deadline)
		if left <= 0 {
			return l.timeoutErr(name, m)
		}
		if err := ctx.Err(); err != nil {
			return l.canceledErr(err, name, m)
		}
		t := time.NewTimer(min(delay, left))
		select {
		case <-ctx.Done():
			t.Stop()
			return l.canceledErr(ctx.Err(), name, m)
		case <-t.C:
		}
		delay = min(2*delay, pollLongest)
	}
}

func (l *Locker) timeoutErr(name string, m Mode) error {
	e := cerr.New(cerr.ExitLock, "lock.timeout",
		"блокировка %s (%s) занята: ожидание %s истекло", name, m, l.timeout)
	e.Details = map[string]any{"lock": name, "mode": m.String(), "timeout": l.timeout.String()}
	return e
}

func (l *Locker) canceledErr(err error, name string, m Mode) error {
	code := "lock.canceled"
	if errors.Is(err, context.DeadlineExceeded) {
		code = "lock.timeout"
	}
	e := cerr.Wrap(err, cerr.ExitLock, code, "ожидание блокировки %s (%s) прервано", name, m)
	e.Details = map[string]any{"lock": name, "mode": m.String()}
	return e
}

func flock(fd, how int) error {
	for {
		err := syscall.Flock(fd, how)
		if !errors.Is(err, syscall.EINTR) {
			if err != nil {
				return fmt.Errorf("flock: %w", err)
			}
			return nil
		}
	}
}
