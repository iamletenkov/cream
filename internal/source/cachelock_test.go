package source

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/paths"
)

// Р24: свои источники (XDG или --sources+--cache) сериализуются flock на
// <кеш>/.lock, а не lock/global машины.
func TestLockCacheSerializes(t *testing.T) {
	t.Parallel()
	cache := filepath.Join(t.TempDir(), "cache")
	l := paths.Layout{Root: t.TempDir(), SourcesFile: "/x/sources.yaml", CacheDir: cache}
	release, err := LockCache(context.Background(), l, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got := CacheLockPath(l); got != filepath.Join(cache, ".lock") {
		t.Errorf("путь блокировки %s", got)
	}
	// Занята — второй ждёт до таймаута: 75 lock.timeout.
	_, err = LockCache(context.Background(), l, 50*time.Millisecond)
	if cerr.ExitCode(err) != cerr.ExitLock || cerr.CodeOf(err) != "lock.timeout" {
		t.Fatalf("занятая блокировка: %v", err)
	}
	// Отмена ожидания — 75 lock.canceled.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = LockCache(ctx, l, time.Second)
	if cerr.ExitCode(err) != cerr.ExitLock {
		t.Fatalf("отменённое ожидание: %v", err)
	}
	// Ждавший получает блокировку после снятия.
	done := make(chan error, 1)
	go func() {
		rel, err := LockCache(context.Background(), l, 5*time.Second)
		if err == nil {
			rel()
		}
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	release()
	if err := <-done; err != nil {
		t.Fatalf("после снятия: %v", err)
	}
	release() // повтор ничего не делает
}
