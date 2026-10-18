package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/paths"
)

var (
	t0 = time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	t1 = time.Date(2026, 10, 18, 11, 30, 0, 0, time.UTC)
)

func fixedNow(t time.Time) func() time.Time { return func() time.Time { return t } }

func newTestStore(t *testing.T, now time.Time) (*Store, paths.Layout) {
	t.Helper()
	l := paths.Layout{Root: t.TempDir()}
	return NewStore(l, fixedNow(now)), l
}

// teleportRecord is the record of spec §10.
func teleportRecord() Record {
	return Record{
		Name: "teleport", Version: "4.0.0", Kind: "extension", Status: StatusInstalled,
		Source: SourceRef{
			Name: "stvor", Type: "archive", Origin: "https://example.test/cream-extensions-4.0.0.tar.gz",
			Resolved: "sha256:9f2c", CatalogVersion: "4.0.0",
		},
		ExtDigest:    "sha256:aaaa",
		ParamsDigest: "sha256:bbbb",
		Phase:        "live",
		CreamVersion: "1.0.0",
		Files:        []FileRec{{Dst: "/etc/systemd/system/teleport.service", SHA256: "cccc"}},
		Bin:          []string{"/usr/local/bin/tsh"},
		Configure:    &ConfigureRec{Stage: "system", Target: "multi-user.target"},
	}
}

const teleportGolden = `{
  "schema": "cream-state/v1",
  "name": "teleport",
  "version": "4.0.0",
  "kind": "extension",
  "status": "installed",
  "source": {
    "name": "stvor",
    "type": "archive",
    "origin": "https://example.test/cream-extensions-4.0.0.tar.gz",
    "resolved": "sha256:9f2c",
    "catalog_version": "4.0.0"
  },
  "ext_digest": "sha256:aaaa",
  "params_digest": "sha256:bbbb",
  "phase": "live",
  "installed_at": "2026-10-18T10:00:00Z",
  "updated_at": "2026-10-18T10:00:00Z",
  "cream_version": "1.0.0",
  "files": [
    {
      "dst": "/etc/systemd/system/teleport.service",
      "sha256": "cccc"
    }
  ],
  "bin": [
    "/usr/local/bin/tsh"
  ],
  "configure": {
    "stage": "system",
    "target": "multi-user.target"
  },
  "reboot_required": false
}
`

func TestPutGolden(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		rec  func() Record
		want string
	}{
		{name: "spec §10 record", rec: teleportRecord, want: teleportGolden},
		{
			name: "library without files, bin, configure; prev_version kept",
			rec: func() Record {
				return Record{
					Name: "lib", Version: "1.0.0", Kind: "library", Status: StatusFailedUpgrade,
					Source: SourceRef{Name: "local", Type: "path", Origin: "/srv/cat"},
					Phase:  "build", CreamVersion: "1.0.0", PrevVersion: "0.9.0", RebootRequired: true,
				}
			},
			want: `{
  "schema": "cream-state/v1",
  "name": "lib",
  "version": "1.0.0",
  "kind": "library",
  "status": "failed-upgrade",
  "source": {
    "name": "local",
    "type": "path",
    "origin": "/srv/cat",
    "resolved": "",
    "catalog_version": ""
  },
  "ext_digest": "",
  "params_digest": "",
  "phase": "build",
  "installed_at": "2026-10-18T10:00:00Z",
  "updated_at": "2026-10-18T10:00:00Z",
  "cream_version": "1.0.0",
  "files": [],
  "bin": [],
  "configure": null,
  "reboot_required": true,
  "prev_version": "0.9.0"
}
`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, l := newTestStore(t, t0)
			r := tc.rec()
			if err := s.Put(r); err != nil {
				t.Fatalf("Put: %v", err)
			}
			got, err := os.ReadFile(l.InstalledFile(r.Name))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("record.json:\n%s\nwant:\n%s", got, tc.want)
			}
			st, err := os.Stat(l.InstalledFile(r.Name))
			if err != nil {
				t.Fatal(err)
			}
			if st.Mode().Perm() != 0o644 {
				t.Errorf("mode = %o, want 644", st.Mode().Perm())
			}
		})
	}
}

func TestPutGetRoundTrip(t *testing.T) {
	t.Parallel()
	s, l := newTestStore(t, t0)
	want := teleportRecord()
	if err := s.Put(want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Get("teleport")
	if err != nil || !ok {
		t.Fatalf("Get = %v, %v", ok, err)
	}
	want.Schema = SchemaV1
	want.InstalledAt, want.UpdatedAt = t0, t0
	if !recordsEqual(*got, want) {
		t.Errorf("Get = %+v\nwant %+v", *got, want)
	}

	// A later Put keeps installed_at and moves updated_at.
	later := NewStore(l, fixedNow(t1.Add(123*time.Millisecond)))
	got.Version = "4.1.0"
	if err := later.Put(*got); err != nil {
		t.Fatal(err)
	}
	again, _, err := later.Get("teleport")
	if err != nil {
		t.Fatal(err)
	}
	if !again.InstalledAt.Equal(t0) || !again.UpdatedAt.Equal(t1) {
		t.Errorf("installed_at = %v, updated_at = %v; want %v, %v (seconds, UTC)", again.InstalledAt, again.UpdatedAt, t0, t1)
	}
}

func recordsEqual(a, b Record) bool {
	if a.Name != b.Name || a.Schema != b.Schema || a.Version != b.Version || a.Kind != b.Kind || a.Status != b.Status ||
		a.Source != b.Source || a.ExtDigest != b.ExtDigest || a.ParamsDigest != b.ParamsDigest || a.Phase != b.Phase ||
		!a.InstalledAt.Equal(b.InstalledAt) || !a.UpdatedAt.Equal(b.UpdatedAt) || a.CreamVersion != b.CreamVersion ||
		a.RebootRequired != b.RebootRequired || a.PrevVersion != b.PrevVersion ||
		len(a.Files) != len(b.Files) || len(a.Bin) != len(b.Bin) || (a.Configure == nil) != (b.Configure == nil) {
		return false
	}
	for i := range a.Files {
		if a.Files[i] != b.Files[i] {
			return false
		}
	}
	for i := range a.Bin {
		if a.Bin[i] != b.Bin[i] {
			return false
		}
	}
	return a.Configure == nil || *a.Configure == *b.Configure
}

func TestGetMissing(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t, t0)
	r, ok, err := s.Get("absent")
	if err != nil || ok || r != nil {
		t.Errorf("Get(absent) = %v, %v, %v; want nil, false, nil", r, ok, err)
	}
}

func TestGetBadRecords(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		data     string
		wantCode string
	}{
		{name: "newer major schema", data: `{"schema":"cream-state/v2","name":"x"}`, wantCode: "state.newer"},
		{name: "much newer schema", data: `{"schema":"cream-state/v10","name":"x","future":1}`, wantCode: "state.newer"},
		{name: "foreign schema", data: `{"schema":"other/v1","name":"x"}`, wantCode: "state.corrupt"},
		{name: "no schema", data: `{"name":"x"}`, wantCode: "state.corrupt"},
		{name: "old schema v0", data: `{"schema":"cream-state/v0","name":"x"}`, wantCode: "state.corrupt"},
		{name: "not json", data: `{"schema":`, wantCode: "state.corrupt"},
		{name: "name differs from file", data: `{"schema":"cream-state/v1","name":"y"}`, wantCode: "state.corrupt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, l := newTestStore(t, t0)
			writeRaw(t, l, "x", tc.data)
			_, _, err := s.Get("x")
			assertCode(t, err, cerr.ExitState, tc.wantCode)
			_, err = s.List()
			assertCode(t, err, cerr.ExitState, tc.wantCode)
		})
	}
}

func TestGetSameMajorUnknownFields(t *testing.T) {
	t.Parallel()
	s, l := newTestStore(t, t0)
	writeRaw(t, l, "x", `{"schema":"cream-state/v1","name":"x","version":"1.0.0","added_in_minor":true}`)
	r, ok, err := s.Get("x")
	if err != nil || !ok || r.Version != "1.0.0" {
		t.Fatalf("Get = %+v, %v, %v", r, ok, err)
	}
}

func TestBadNames(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t, t0)
	for _, name := range []string{"", ".hidden", "a/b", "..", "a\x00b"} {
		if _, _, err := s.Get(name); err == nil {
			t.Errorf("Get(%q): want error", name)
		}
		if err := s.Put(Record{Name: name}); err == nil {
			t.Errorf("Put(%q): want error", name)
		}
		if err := s.Delete(name); err == nil {
			t.Errorf("Delete(%q): want error", name)
		}
	}
}

// A crash between the write of the temporary file and the rename leaves the
// old record whole; List skips the leftover temporary file.
func TestPutAtomicOnCrash(t *testing.T) {
	t.Parallel()
	s, l := newTestStore(t, t0)
	if err := s.Put(teleportRecord()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(l.InstalledFile("teleport"))
	if err != nil {
		t.Fatal(err)
	}
	crash := errors.New("power cut")
	s.write = func(path string, data []byte, _ os.FileMode) error {
		tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp-123")
		if err := os.WriteFile(tmp, data[:len(data)/2], 0o644); err != nil {
			t.Fatal(err)
		}
		return crash
	}
	r := teleportRecord()
	r.Version = "5.0.0"
	if err := s.Put(r); !errors.Is(err, crash) {
		t.Fatalf("Put = %v, want the crash", err)
	} else {
		assertCode(t, err, cerr.ExitState, "state.write")
	}
	after, err := os.ReadFile(l.InstalledFile("teleport"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Errorf("record changed after a crash:\n%s", after)
	}
	list, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Version != "4.0.0" {
		t.Errorf("List = %+v, want only teleport 4.0.0", list)
	}
}

func TestList(t *testing.T) {
	t.Parallel()
	s, l := newTestStore(t, t0)
	if list, err := s.List(); err != nil || len(list) != 0 || list == nil {
		t.Fatalf("List on empty machine = %#v, %v; want empty non-nil", list, err)
	}
	for _, n := range []string{"zeta", "alpha", "mid"} {
		r := teleportRecord()
		r.Name = n
		if err := s.Put(r); err != nil {
			t.Fatal(err)
		}
	}
	dir := l.InstalledDir()
	for name, data := range map[string]string{
		".zeta.json.tmp-999": "garbage",
		"notes.txt":          "garbage",
		"README":             "garbage",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "sub.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range list {
		names = append(names, r.Name)
	}
	if strings.Join(names, ",") != "alpha,mid,zeta" {
		t.Errorf("List names = %v, want alpha,mid,zeta", names)
	}
}

func TestDelete(t *testing.T) {
	t.Parallel()
	s, _ := newTestStore(t, t0)
	if err := s.Put(teleportRecord()); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("teleport"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok, err := s.Get("teleport"); ok || err != nil {
		t.Errorf("Get after Delete = %v, %v", ok, err)
	}
	if err := s.Delete("teleport"); err != nil {
		t.Errorf("second Delete = %v, want nil (idempotent)", err)
	}
}

func writeRaw(t *testing.T, l paths.Layout, name, data string) {
	t.Helper()
	if err := os.MkdirAll(l.InstalledDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.InstalledFile(name), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertCode(t *testing.T, err error, exit int, code string) {
	t.Helper()
	if got := cerr.ExitCode(err); got != exit {
		t.Errorf("exit = %d (%v), want %d", got, err, exit)
	}
	if got := cerr.CodeOf(err); got != code {
		t.Errorf("code = %q (%v), want %q", got, err, code)
	}
}
