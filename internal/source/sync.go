package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/fsx"
	"github.com/iamletenkov/cream/internal/paths"
)

// Syncer refreshes source caches: the only place cream uses the network.
//
// Validate checks a candidate catalog tree (cream-catalog.yaml and every
// manifest, §4.1) and returns its catalog version; an *cerr.Error it returns
// is passed through, any other error becomes 65 source.catalog. Now stamps
// synced_at.
//
// Sync does not lock: the caller serializes syncs (and readers of the cache)
// with its own lock.
type Syncer struct {
	Layout   paths.Layout
	Fetch    Fetcher
	Validate func(dir string) (catalogVersion string, err error)
	Now      func() time.Time

	// writeLock writes sources.lock; nil is fsx.WriteAtomic (tests fail it).
	writeLock func(path string, data []byte, perm fs.FileMode) error
}

// Sync builds a new cache of src in a private directory next to the cache,
// validates it, prepares the new sources.lock (a corrupt one refuses the
// sync before anything changes), swaps the cache in with one rename (an
// atomic exchange when the cache exists) and writes sources.lock. On any
// failure the previous cache and sources.lock stay as they were: a failed
// lock write swaps the previous cache back.
//
// archive: download (limit 256 MiB), sha256 compared before extraction
// (69 source.sha256_mismatch), safe extraction (69 source.bad_archive).
// path: a copy without following links (69 source.path), resolved by its
// tree digest.
func (s *Syncer) Sync(ctx context.Context, src Source) (LockEntry, error) {
	src.SHA256 = strings.ToLower(src.SHA256)
	if err := check(src); err != nil {
		var r reservedError
		if errors.As(err, &r) {
			return LockEntry{}, cerr.New(cerr.ExitData, "source.reserved", "источник %s: тип %s %s", src.Name, src.Type, reservedMsg)
		}
		return LockEntry{}, cerr.Wrap(err, cerr.ExitData, "source.invalid", "источник %q", src.Name)
	}
	if s.Validate == nil {
		return LockEntry{}, cerr.New(cerr.ExitInternal, cerr.CodeInternal, "синхронизация %s: нет валидатора каталога", src.Name)
	}
	cache := s.Layout.SourceCache(src.Name)
	parent := filepath.Dir(cache)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return LockEntry{}, cerr.Wrap(err, cerr.ExitState, "state.write", "кеш источников %s", parent)
	}
	staging, err := os.MkdirTemp(parent, "."+src.Name+".sync-*")
	if err != nil {
		return LockEntry{}, cerr.Wrap(err, cerr.ExitState, "state.write", "кеш источников %s", parent)
	}
	defer func() { _ = os.RemoveAll(staging) }()
	tree := filepath.Join(staging, "tree")

	e := LockEntry{Name: src.Name, Type: src.Type}
	switch src.Type {
	case TypeArchive:
		e.Origin = src.URL
		e.Resolved, err = s.fetchArchive(ctx, src, staging, tree)
	case TypePath:
		e.Origin = src.Path
		e.Resolved, err = copyPath(src, tree)
	}
	if err != nil {
		return LockEntry{}, err
	}
	e.CatalogVersion, err = s.Validate(tree)
	if err != nil {
		var ce *cerr.Error
		if errors.As(err, &ce) {
			return LockEntry{}, err
		}
		return LockEntry{}, cerr.Wrap(err, cerr.ExitData, "source.catalog", "источник %s: каталог не прошёл проверку", src.Name)
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	e.SyncedAt = now().UTC().Truncate(time.Second)
	lockPath := s.Layout.SourcesLock()
	data, _, err := prepareLock(lockPath, src.Name, &e)
	if err != nil {
		return LockEntry{}, err
	}
	if err := lockDir(lockPath); err != nil {
		return LockEntry{}, err
	}
	undo, err := swapIn(tree, cache, filepath.Join(staging, "old"), exchange)
	if err != nil {
		return LockEntry{}, cerr.Wrap(err, cerr.ExitState, "state.write", "кеш источника %s", src.Name)
	}
	write := s.writeLock
	if write == nil {
		write = fsx.WriteAtomic
	}
	if err := writeLockFile(lockPath, data, write); err != nil {
		if uerr := undo(); uerr != nil {
			return LockEntry{}, cerr.Wrap(errors.Join(err, uerr), cerr.ExitState, "state.write",
				"источник %s: sources.lock не записан, прежний кеш не возвращён", src.Name)
		}
		return LockEntry{}, err
	}
	return e, nil
}

func (s *Syncer) fetchArchive(ctx context.Context, src Source, staging, tree string) (string, error) {
	if s.Fetch == nil {
		return "", cerr.New(cerr.ExitInternal, cerr.CodeInternal, "синхронизация %s: нет загрузчика", src.Name)
	}
	file := filepath.Join(staging, "archive.tar.gz")
	f, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", cerr.Wrap(err, cerr.ExitState, "state.write", "загрузка %s", src.Name)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if err := s.Fetch.Fetch(ctx, src.URL, io.MultiWriter(f, h), MaxArchiveSize); err != nil {
		var ce *cerr.Error
		if errors.As(err, &ce) {
			return "", err
		}
		return "", cerr.Wrap(err, cerr.ExitSource, "source.fetch", "источник %s", src.Name)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != src.SHA256 {
		e := cerr.New(cerr.ExitSource, "source.sha256_mismatch",
			"источник %s: sha256 архива %s, ждали %s", src.Name, got, src.SHA256)
		e.Details = map[string]any{"expected": src.SHA256, "actual": got}
		return "", e
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", cerr.Wrap(err, cerr.ExitState, "state.read", "архив %s", src.Name)
	}
	if err := ExtractTarGz(f, tree); err != nil {
		return "", cerr.Wrap(err, cerr.ExitSource, "source.bad_archive", "источник %s", src.Name)
	}
	return "sha256:" + got, nil
}

func copyPath(src Source, tree string) (string, error) {
	if err := fsx.CopyTree(src.Path, tree); err != nil {
		return "", cerr.Wrap(err, cerr.ExitSource, "source.path", "источник %s", src.Name)
	}
	d, err := fsx.TreeDigest(tree)
	if err != nil {
		return "", cerr.Wrap(err, cerr.ExitSource, "source.path", "источник %s", src.Name)
	}
	return d, nil
}

// swapIn puts the directory next in place of cache. Without a cache it is a
// plain rename; otherwise the two are exchanged atomically with xchg
// (renameat2 RENAME_EXCHANGE) so readers never see the cache missing, and
// the old tree ends up at next. Where the exchange is unsupported (ENOSYS,
// EINVAL), the old cache is moved to old first and next renamed in. The
// returned undo puts the previous state back (the previous cache in place,
// or no cache), the new tree returning to next.
func swapIn(next, cache, old string, xchg func(a, b string) error) (undo func() error, err error) {
	if _, err := os.Lstat(cache); errors.Is(err, fs.ErrNotExist) {
		if err := os.Rename(next, cache); err != nil {
			return nil, err
		}
		return func() error { return os.Rename(cache, next) }, nil
	} else if err != nil {
		return nil, err
	}
	err = xchg(next, cache)
	if err == nil {
		return func() error { return xchg(next, cache) }, nil
	}
	if !errors.Is(err, syscall.ENOSYS) && !errors.Is(err, syscall.EINVAL) {
		return nil, fmt.Errorf("обмен каталогов: %w", err)
	}
	if err := os.Rename(cache, old); err != nil {
		return nil, err
	}
	if err := os.Rename(next, cache); err != nil {
		_ = os.Rename(old, cache)
		return nil, err
	}
	return func() error {
		if err := os.Rename(cache, next); err != nil {
			return err
		}
		return os.Rename(old, cache)
	}, nil
}

// Forget removes the cache and the sources.lock entry of name (for
// `source remove`); nothing to remove is not an error.
func Forget(l paths.Layout, name string) error {
	if !nameRE.MatchString(name) {
		return cerr.New(cerr.ExitUsage, "usage.source", "имя источника %q", name)
	}
	if err := os.RemoveAll(l.SourceCache(name)); err != nil {
		return cerr.Wrap(err, cerr.ExitState, "state.write", "кеш источника %s", name)
	}
	return updateLock(l.SourcesLock(), name, nil)
}
