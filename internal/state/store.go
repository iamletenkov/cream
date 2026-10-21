package state

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/fsx"
	"github.com/iamletenkov/cream/internal/paths"
	"github.com/iamletenkov/cream/internal/version"
)

// Store reads and writes the registry and the journal of one layout.
type Store struct {
	layout paths.Layout
	now    func() time.Time
	// write is fsx.WriteAtomic; tests inject a crash.
	write func(path string, data []byte, perm fs.FileMode) error
	// cream is the build version written to cream.json (version.Version).
	cream string
	// Warn receives labels.write_failed; nil drops the warning.
	Warn *cerr.Warnings
}

// NewStore returns a Store; now stamps installed_at, updated_at and journal lines.
func NewStore(l paths.Layout, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{layout: l, now: now, write: fsx.WriteAtomic, cream: version.Version}
}

func (s *Store) stamp() time.Time { return s.now().UTC().Truncate(time.Second) }

// checkName guards the registry paths; real validation is the manifest's.
func checkName(name string) error {
	if name == "" || strings.HasPrefix(name, ".") || strings.ContainsAny(name, "/\x00") {
		return cerr.New(cerr.ExitInternal, cerr.CodeInternal, "недопустимое имя записи реестра %q", name)
	}
	return nil
}

// Get returns the record of name; ok is false when the extension is not installed.
func (s *Store) Get(name string) (rec *Record, ok bool, err error) {
	if err := checkName(name); err != nil {
		return nil, false, err
	}
	path := s.layout.InstalledFile(name)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, cerr.Wrap(err, cerr.ExitState, cerr.CodeIORead, "реестр %s", name)
	}
	r, err := decode(path, name, data)
	if err != nil {
		return nil, false, err
	}
	return r, true, nil
}

// List returns all records sorted by name; temporary and foreign files are
// skipped. A corrupt record is skipped with a state.corrupt warning naming
// the extension (Р20) — Get of it still fails with 74; a newer schema and a
// read error stay fatal. w may be nil.
func (s *Store) List(w *cerr.Warnings) ([]Record, error) {
	dir := s.layout.InstalledDir()
	ents, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return []Record{}, nil
	}
	if err != nil {
		return nil, cerr.Wrap(err, cerr.ExitState, cerr.CodeIORead, "реестр %s", dir)
	}
	out := []Record{}
	for _, e := range ents {
		n := e.Name()
		if !e.Type().IsRegular() || strings.HasPrefix(n, ".") || !strings.HasSuffix(n, ".json") {
			continue
		}
		name := strings.TrimSuffix(n, ".json")
		r, ok, err := s.Get(name)
		if err != nil {
			if cerr.CodeOf(err) == "state.corrupt" {
				if w != nil {
					w.AddDetails("state.corrupt", map[string]any{"ext": name}, "запись реестра %s повреждена и пропущена: %v", name, err)
				}
				continue
			}
			return nil, err
		}
		if ok {
			out = append(out, *r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func decode(path, name string, data []byte) (*Record, error) {
	var head struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, cerr.Wrap(err, cerr.ExitState, "state.corrupt", "реестр %s повреждён", path)
	}
	major, ok := schemaMajorOf(head.Schema)
	switch {
	case !ok || major < schemaMajor:
		return nil, cerr.New(cerr.ExitState, "state.corrupt", "реестр %s: неизвестная схема %q", path, head.Schema)
	case major > schemaMajor:
		e := cerr.New(cerr.ExitState, "state.newer",
			"реестр %s новее исполнителя: схема %s, cream читает %s", path, head.Schema, SchemaV1)
		e.Details = map[string]any{"schema": head.Schema, "supported": SchemaV1}
		return nil, e
	}
	var r Record
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, cerr.Wrap(err, cerr.ExitState, "state.corrupt", "реестр %s повреждён", path)
	}
	if r.Name != name {
		return nil, cerr.New(cerr.ExitState, "state.corrupt", "реестр %s: имя записи %q не совпадает с файлом", path, r.Name)
	}
	return &r, nil
}

// schemaMajorOf parses "cream-state/v<N>".
func schemaMajorOf(s string) (int, bool) {
	rest, ok := strings.CutPrefix(s, schemaPrefix)
	if !ok || rest == "" || strings.HasPrefix(rest, "+") || strings.HasPrefix(rest, "-") {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil {
		return 0, false
	}
	return n, true
}

// Put writes the record atomically (mode 0644). Schema is set to SchemaV1,
// UpdatedAt to now, InstalledAt to now when zero; times are UTC seconds.
// nil Files and Bin are written as empty lists. After the record the image
// labels (cream.json) are rewritten; their failure is only a warning.
func (s *Store) Put(r Record) error {
	if err := checkName(r.Name); err != nil {
		return err
	}
	now := s.stamp()
	r.Schema = SchemaV1
	r.UpdatedAt = now
	if r.InstalledAt.IsZero() {
		r.InstalledAt = now
	}
	r.InstalledAt = r.InstalledAt.UTC().Truncate(time.Second)
	if r.Files == nil {
		r.Files = []FileRec{}
	}
	if r.Bin == nil {
		r.Bin = []string{}
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return cerr.Wrap(err, cerr.ExitInternal, cerr.CodeInternal, "реестр %s", r.Name)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(s.layout.InstalledDir(), 0o755); err != nil {
		return cerr.Wrap(err, cerr.ExitState, cerr.CodeIOWrite, "реестр %s", r.Name)
	}
	if err := s.write(s.layout.InstalledFile(r.Name), data, 0o644); err != nil {
		return cerr.Wrap(err, cerr.ExitState, cerr.CodeIOWrite, "реестр %s", r.Name)
	}
	s.writeLabels()
	return nil
}

// Delete removes the record and rewrites the image labels; a missing record
// is not an error.
func (s *Store) Delete(name string) error {
	if err := checkName(name); err != nil {
		return err
	}
	err := os.Remove(s.layout.InstalledFile(name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return cerr.Wrap(err, cerr.ExitState, cerr.CodeIOWrite, "удаление записи реестра %s", name)
	}
	if err := syncDir(filepath.Dir(s.layout.InstalledFile(name))); err != nil {
		return cerr.Wrap(err, cerr.ExitState, cerr.CodeIOWrite, "удаление записи реестра %s: fsync каталога", name)
	}
	s.writeLabels()
	return nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return err
	}
	return d.Close()
}
