package source

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/paths"
)

// SourceDir is a source with its cache directory: what the catalog layer
// searches (§9.3). Synced is false when the cache does not exist yet ("not
// synchronized" differs from "not in sources"); Lock is the sources.lock
// entry, nil if there is none.
type SourceDir struct { //nolint:revive // имя закреплено контрактом плана (catalog.Entry.Source)
	Source Source
	Dir    string
	Synced bool
	Lock   *LockEntry
}

// Dirs returns the cache directories of f's sources in priority order. It
// reads nothing but the existence of each cache directory; it never syncs.
//
// On the real root (Р9, §13) an existing cache directory, its parent
// <CacheRoot>/sources and CacheRoot itself must belong to the current user (root, when cream runs
// as root) and must not be writable by group or others: 77 perm.cache.
func Dirs(l paths.Layout, f *File, lock []LockEntry) ([]SourceDir, error) {
	check := l.NeedsRoot()
	uid := os.Geteuid()
	out := make([]SourceDir, 0, len(f.Sources))
	parentChecked := false
	for _, s := range f.Sources {
		d := SourceDir{Source: s, Dir: l.SourceCache(s.Name)}
		if st, err := os.Lstat(d.Dir); err == nil && st.IsDir() {
			d.Synced = true
			if check {
				if !parentChecked {
					for _, dir := range []string{l.CacheRoot(), filepath.Dir(d.Dir)} {
						if err := checkCacheDir(dir, uid); err != nil {
							return nil, err
						}
					}
					parentChecked = true
				}
				if err := checkCacheDir(d.Dir, uid); err != nil {
					return nil, err
				}
			}
		}
		for i := range lock {
			if lock[i].Name == s.Name {
				e := lock[i]
				d.Lock = &e
				break
			}
		}
		out = append(out, d)
	}
	return out, nil
}

// checkCacheDir refuses a cache directory owned by another user or writable
// by group or others.
func checkCacheDir(dir string, uid int) error {
	st, err := os.Lstat(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return cerr.Wrap(err, cerr.ExitState, cerr.CodeIORead, "кеш источников %s", dir)
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	var bad string
	switch {
	case !st.IsDir():
		bad = "не каталог"
	case !ok:
		bad = "нет сведений о владельце"
	case int(sys.Uid) != uid:
		bad = fmt.Sprintf("владелец uid %d, нужен %d", sys.Uid, uid)
	case st.Mode().Perm()&0o022 != 0:
		bad = fmt.Sprintf("права %04o разрешают запись группе или всем", st.Mode().Perm())
	default:
		return nil
	}
	e := cerr.New(cerr.ExitPerm, "perm.cache", "кеш источников %s: %s", dir, bad)
	e.Details = map[string]any{"path": dir}
	return e
}
