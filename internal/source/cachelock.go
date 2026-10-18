package source

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/paths"
)

// Pauses between attempts to take the cache lock.
const (
	cacheLockPollFirst   = 10 * time.Millisecond
	cacheLockPollLongest = 500 * time.Millisecond
)

// CacheLockPath is the lock of the caller's own sources (ruling Р24):
// <cache root>/.lock — the --cache directory or the XDG cache.
func CacheLockPath(l paths.Layout) string {
	return filepath.Join(l.CacheRoot(), ".lock")
}

// LockCache takes CacheLockPath exclusively for `source add/remove/sync` of
// the caller's own sources (XDG mode or --sources with --cache), which do not
// touch the machine and so do not take lock/global (Р24). It waits up to
// timeout (75 lock.timeout) or until ctx ends (75 lock.canceled). release
// drops the lock; repeated calls do nothing.
func LockCache(ctx context.Context, l paths.Layout, timeout time.Duration) (release func(), err error) {
	p := CacheLockPath(l)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, cerr.Wrap(err, cerr.ExitState, "state.lock_dir", "каталог кеша %s", filepath.Dir(p))
	}
	f, err := os.OpenFile(p, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, cerr.Wrap(err, cerr.ExitState, "state.lock_file", "файл блокировки %s", p)
	}
	if err := cacheLockWait(ctx, f, p, timeout); err != nil {
		_ = f.Close()
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			_ = f.Close()
		})
	}, nil
}

// cacheLockWait polls a non-blocking flock: a blocking one could be
// interrupted neither by the timeout nor by ctx.
func cacheLockWait(ctx context.Context, f *os.File, p string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	delay := cacheLockPollFirst
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, syscall.EINTR):
			continue
		case !errors.Is(err, syscall.EWOULDBLOCK):
			return cerr.Wrap(err, cerr.ExitState, "state.lock_file", "flock %s", p)
		}
		details := map[string]any{"lock": p, "mode": "exclusive", "timeout": timeout.String()}
		left := time.Until(deadline)
		if left <= 0 {
			ce := cerr.New(cerr.ExitLock, "lock.timeout", "блокировка кеша источников %s занята: ожидание %s истекло", p, timeout)
			ce.Details = details
			return ce
		}
		t := time.NewTimer(min(delay, left))
		select {
		case <-ctx.Done():
			t.Stop()
			ce := cerr.Wrap(ctx.Err(), cerr.ExitLock, "lock.canceled", "ожидание блокировки кеша источников %s прервано", p)
			ce.Details = details
			return ce
		case <-t.C:
		}
		delay = min(2*delay, cacheLockPollLongest)
	}
}
