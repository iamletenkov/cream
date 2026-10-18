package source

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/fsx"
)

// LockEntry is one line of sources.lock (§9.2): what was observed at the
// last successful sync. Resolved is "sha256:<hex>" of the archive or the
// tree digest (§9.5) of a path source.
type LockEntry struct {
	Name           string    `json:"name"`
	Type           string    `json:"type"`
	Origin         string    `json:"origin"`
	Resolved       string    `json:"resolved"`
	SyncedAt       time.Time `json:"synced_at"`
	CatalogVersion string    `json:"catalog_version"`
}

type lockFile struct {
	Sources []LockEntry `json:"sources"`
}

// ReadLock reads sources.lock; a missing file is an empty list, a corrupt
// one is 74 state.corrupt.
func ReadLock(path string) ([]LockEntry, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return []LockEntry{}, nil
	}
	if err != nil {
		return nil, cerr.Wrap(err, cerr.ExitState, "state.read", "sources.lock %s", path)
	}
	var lf lockFile
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&lf); err != nil {
		return nil, cerr.Wrap(err, cerr.ExitState, "state.corrupt", "sources.lock %s", path)
	}
	if lf.Sources == nil {
		lf.Sources = []LockEntry{}
	}
	return lf.Sources, nil
}

// updateLock replaces (or, with e == nil, removes) the entry called name
// and writes the file atomically, entries sorted by name.
func updateLock(path, name string, e *LockEntry) error {
	cur, err := ReadLock(path)
	if err != nil {
		return err
	}
	out := make([]LockEntry, 0, len(cur)+1)
	for _, c := range cur {
		if c.Name != name {
			out = append(out, c)
		}
	}
	if e == nil && len(out) == len(cur) {
		return nil
	}
	if e != nil {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	data, err := json.MarshalIndent(lockFile{Sources: out}, "", "  ")
	if err != nil {
		return cerr.Wrap(err, cerr.ExitInternal, cerr.CodeInternal, "кодирование sources.lock")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return cerr.Wrap(err, cerr.ExitState, "state.write", "каталог %s", filepath.Dir(path))
	}
	if err := fsx.WriteAtomic(path, append(data, '\n'), 0o644); err != nil {
		return cerr.Wrap(err, cerr.ExitState, "state.write", "sources.lock %s", path)
	}
	return nil
}
