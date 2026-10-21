package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/paths"
)

// writeManifest lays out the installed tree of name with its provides.
func writeManifest(t *testing.T, l paths.Layout, name, body string) {
	t.Helper()
	dir := l.ExtTree(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extension.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readLabels(t *testing.T, l paths.Layout) map[string]string {
	t.Helper()
	data, err := os.ReadFile(l.ImageLabels())
	if err != nil {
		t.Fatalf("cream.json: %v", err)
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("cream.json is not {key: string}: %v\n%s", err, data)
	}
	return m
}

func desktopRecord() Record {
	return Record{
		Name: "desktop", Version: "2.1.0", Kind: "extension", Status: StatusInstalled,
		Source:    SourceRef{Name: "core", Type: "path", Origin: "/srv/catalog", Resolved: "sha256:dddd", CatalogVersion: "5.2.0"},
		ExtDigest: "sha256:eeee", ParamsDigest: "sha256:ffff", Phase: "build",
		Files: []FileRec{{Dst: "/etc/desktop.conf", SHA256: "1111"}},
	}
}

// TestLabelsShape: §8.2 of the spec — three keys, extensions and sources as
// JSON strings, sorted by name, only name/version/source/digest/provides.
func TestLabelsShape(t *testing.T) {
	t.Parallel()
	s, l := newTestStore(t, t0)
	s.cream = "v1.2.0"
	writeManifest(t, l, "desktop", "schema: cream/v1\nname: desktop\nprovides: [desktop-session, x11]\n")
	if err := s.Put(teleportRecord()); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(desktopRecord()); err != nil {
		t.Fatal(err)
	}
	got := readLabels(t, l)
	want := map[string]string{
		"cream.version": "1.2.0",
		"cream.extensions": `[{"name":"desktop","version":"2.1.0","source":"core","digest":"sha256:eeee","provides":["desktop-session","x11"]},` +
			`{"name":"teleport","version":"4.0.0","source":"stvor","digest":"sha256:aaaa","provides":[]}]`,
		"cream.sources": `[{"name":"core","type":"path","catalog_version":"5.2.0"},` +
			`{"name":"stvor","type":"archive","catalog_version":"4.0.0"}]`,
	}
	if len(got) != len(want) {
		t.Errorf("keys = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s =\n%s\nwant\n%s", k, got[k], v)
		}
	}
}

// TestLabelsNoSecrets: parameters, params_digest, origins and paths never
// reach the labels (§8.2).
func TestLabelsNoSecrets(t *testing.T) {
	t.Parallel()
	s, l := newTestStore(t, t0)
	if err := s.Put(teleportRecord()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(l.ImageLabels())
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"params_digest", "sha256:bbbb", "example.test", "/etc/", "/usr/", "sha256:9f2c", "multi-user"} {
		if strings.Contains(string(data), leak) {
			t.Errorf("cream.json carries %q:\n%s", leak, data)
		}
	}
}

// TestLabelsOnlyInstalled: failed, failed-upgrade and skipped records do not
// count as installed for a descendant image.
func TestLabelsOnlyInstalled(t *testing.T) {
	t.Parallel()
	s, l := newTestStore(t, t0)
	for _, st := range []string{StatusFailed, StatusFailedUpgrade, StatusSkipped} {
		r := teleportRecord()
		r.Name, r.Status = "x-"+strings.ReplaceAll(st, "-", ""), st
		if err := s.Put(r); err != nil {
			t.Fatal(err)
		}
	}
	if got := readLabels(t, l)["cream.extensions"]; got != "[]" {
		t.Errorf("cream.extensions = %s, want []", got)
	}
	if got := readLabels(t, l)["cream.sources"]; got != "[]" {
		t.Errorf("cream.sources = %s, want []", got)
	}
}

// TestLabelsRewrittenOnDelete: after remove the file describes the rest.
func TestLabelsRewrittenOnDelete(t *testing.T) {
	t.Parallel()
	s, l := newTestStore(t, t0)
	if err := s.Put(teleportRecord()); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(desktopRecord()); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("teleport"); err != nil {
		t.Fatal(err)
	}
	got := readLabels(t, l)
	if strings.Contains(got["cream.extensions"], "teleport") || !strings.Contains(got["cream.extensions"], `"desktop"`) {
		t.Errorf("cream.extensions after remove = %s", got["cream.extensions"])
	}
	if strings.Contains(got["cream.sources"], "stvor") {
		t.Errorf("cream.sources after remove = %s", got["cream.sources"])
	}
	if err := s.Delete("desktop"); err != nil {
		t.Fatal(err)
	}
	if got := readLabels(t, l)["cream.extensions"]; got != "[]" {
		t.Errorf("cream.extensions of an empty registry = %s, want []", got)
	}
}

// TestLabelsAtomic: the file goes through the atomic writer (temporary file
// + rename) with mode 0644.
func TestLabelsAtomic(t *testing.T) {
	t.Parallel()
	s, l := newTestStore(t, t0)
	var paths []string
	inner := s.write
	s.write = func(path string, data []byte, perm os.FileMode) error {
		paths = append(paths, path)
		if path == l.ImageLabels() && perm != 0o644 {
			t.Errorf("cream.json mode = %o, want 644", perm)
		}
		return inner(path, data, perm)
	}
	if err := s.Put(teleportRecord()); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] != l.InstalledFile("teleport") || paths[1] != l.ImageLabels() {
		t.Errorf("writes = %v, want the record, then cream.json", paths)
	}
	ents, err := os.ReadDir(filepath.Dir(l.ImageLabels()))
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 1 || ents[0].Name() != "cream.json" {
		t.Errorf("image-labels.d = %v, want only cream.json", ents)
	}
}

// TestLabelsWriteFailedWarns: a failed write is the labels.write_failed
// warning, the operation succeeds; the same for Delete.
func TestLabelsWriteFailedWarns(t *testing.T) {
	t.Parallel()
	s, l := newTestStore(t, t0)
	w := &cerr.Warnings{}
	s.Warn = w
	inner := s.write
	s.write = func(path string, data []byte, perm os.FileMode) error {
		if path == l.ImageLabels() {
			return errors.New("read-only /etc")
		}
		return inner(path, data, perm)
	}
	if err := s.Put(teleportRecord()); err != nil {
		t.Fatalf("Put = %v, want success despite the labels", err)
	}
	if err := s.Delete("teleport"); err != nil {
		t.Fatalf("Delete = %v, want success despite the labels", err)
	}
	list := w.List()
	if len(list) != 2 || list[0].Code != "labels.write_failed" || list[1].Code != "labels.write_failed" {
		t.Fatalf("warnings = %+v, want two labels.write_failed", list)
	}
	if !strings.Contains(list[0].Message, "read-only /etc") {
		t.Errorf("message = %q, want the cause", list[0].Message)
	}
	if _, err := os.Stat(l.ImageLabels()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("cream.json exists after failed writes: %v", err)
	}
	// Без приёмника предупреждений сбой тоже не роняет операцию.
	s.Warn = nil
	if err := s.Put(teleportRecord()); err != nil {
		t.Fatalf("Put without Warn = %v", err)
	}
}

// TestLabelsBrokenManifest: an unreadable manifest of the installed tree
// gives empty provides, not a failure.
func TestLabelsBrokenManifest(t *testing.T) {
	t.Parallel()
	s, l := newTestStore(t, t0)
	writeManifest(t, l, "desktop", "provides: [unclosed\n")
	if err := s.Put(desktopRecord()); err != nil {
		t.Fatal(err)
	}
	if got := readLabels(t, l)["cream.extensions"]; !strings.Contains(got, `"provides":[]`) {
		t.Errorf("cream.extensions = %s, want empty provides", got)
	}
}

// TestLabelsDevVersion: an unset build version is "dev".
func TestLabelsDevVersion(t *testing.T) {
	t.Parallel()
	s, l := newTestStore(t, t0)
	s.cream = ""
	if err := s.Put(teleportRecord()); err != nil {
		t.Fatal(err)
	}
	if got := readLabels(t, l)["cream.version"]; got != "dev" {
		t.Errorf("cream.version = %q, want dev", got)
	}
}
