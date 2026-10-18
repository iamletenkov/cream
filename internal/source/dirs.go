package source

import (
	"os"

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
func Dirs(l paths.Layout, f *File, lock []LockEntry) []SourceDir {
	out := make([]SourceDir, 0, len(f.Sources))
	for _, s := range f.Sources {
		d := SourceDir{Source: s, Dir: l.SourceCache(s.Name)}
		if st, err := os.Lstat(d.Dir); err == nil && st.IsDir() {
			d.Synced = true
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
	return out
}
