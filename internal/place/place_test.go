package place

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/paths"
	"github.com/iamletenkov/cream/internal/state"
)

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// fixture makes an extension tree with the given files (name → content, mode).
func fixture(t *testing.T, files map[string]struct {
	body string
	mode fs.FileMode
}) string {
	t.Helper()
	tree := t.TempDir()
	for name, f := range files {
		p := filepath.Join(tree, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f.body), f.mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, f.mode); err != nil {
			t.Fatal(err)
		}
	}
	return tree
}

type src = struct {
	body string
	mode fs.FileMode
}

// registry is an Owner over a map dst → ext.
func registry(m map[string]string) Owner {
	return func(dst string) (string, bool) {
		ext, ok := m[dst]
		return ext, ok
	}
}

func TestPlace(t *testing.T) {
	t.Parallel()
	tree := fixture(t, map[string]src{
		"files/teleport.service": {"[Unit]\n", 0o644},
		"files/join.sh":          {"#!/bin/sh\n", 0o755},
		"files/token.conf":       {"secret-ish\n", 0o644},
	})
	tests := []struct {
		name     string
		file     manifest.File
		wantMode fs.FileMode
		body     string
	}{
		{"default 0644", manifest.File{Src: "files/teleport.service", Dst: "/etc/systemd/system/teleport.service"}, 0o644, "[Unit]\n"},
		{"default exec 0755", manifest.File{Src: "files/join.sh", Dst: "/usr/local/libexec/teleport/join.sh"}, 0o755, "#!/bin/sh\n"},
		{"explicit mode", manifest.File{Src: "files/token.conf", Dst: "/etc/teleport.d/token.conf", Mode: 0o600}, 0o600, "secret-ish\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l := paths.Layout{Root: t.TempDir()}
			recs, err := Place(l, "teleport", tree, []manifest.File{tc.file}, registry(nil))
			if err != nil {
				t.Fatal(err)
			}
			want := []state.FileRec{{Dst: tc.file.Dst, SHA256: sum(tc.body)}}
			if !reflect.DeepEqual(recs, want) {
				t.Errorf("recs = %+v, want %+v", recs, want)
			}
			p := onDisk(l, tc.file.Dst)
			got, err := os.ReadFile(p)
			if err != nil || string(got) != tc.body {
				t.Fatalf("content %q, %v", got, err)
			}
			st, _ := os.Stat(p)
			if st.Mode().Perm() != tc.wantMode {
				t.Errorf("mode %#o, want %#o", st.Mode().Perm(), tc.wantMode)
			}
		})
	}
}

func TestPlaceConflict(t *testing.T) {
	t.Parallel()
	tree := fixture(t, map[string]src{"a": {"A\n", 0o644}, "b": {"B\n", 0o644}})
	l := paths.Layout{Root: t.TempDir()}
	files := []manifest.File{
		{Src: "a", Dst: "/etc/ok.conf"},
		{Src: "b", Dst: "/etc/systemd/system/docker.service"},
	}
	_, err := Place(l, "teleport", tree, files, registry(map[string]string{"/etc/systemd/system/docker.service": "docker"}))
	if cerr.ExitCode(err) != cerr.ExitGraph || cerr.CodeOf(err) != CodeFileOwner {
		t.Fatalf("err = %v (exit %d, code %q)", err, cerr.ExitCode(err), cerr.CodeOf(err))
	}
	d := cerr.DetailsOf(err)
	if d["dst"] != "/etc/systemd/system/docker.service" || d["owner"] != "docker" {
		t.Errorf("details = %v", d)
	}
	// Ownership is checked before anything is written.
	if _, err := os.Lstat(onDisk(l, "/etc/ok.conf")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("file written before conflict check: %v", err)
	}
}

// Р11: A puts a file into B's settings directory; B's registry does not list
// it, so it is A's file. The parent directory is created and survives Unplace.
func TestPlaceIntoForeignEtcDir(t *testing.T) {
	t.Parallel()
	tree := fixture(t, map[string]src{"files/label": {"robot=yes\n", 0o644}})
	l := paths.Layout{Root: t.TempDir()}
	dst := "/etc/cream/teleport/command-labels.d/stvor-agent"
	files := []manifest.File{{Src: "files/label", Dst: dst}}
	reg := map[string]string{"/etc/systemd/system/teleport.service": "teleport"}
	recs, err := Place(l, "stvor-agent", tree, files, registry(reg))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Dst != dst {
		t.Fatalf("recs = %+v", recs)
	}
	// Repeated Place of A's own file (A's registry now lists it) is no conflict.
	reg[dst] = "stvor-agent"
	if _, err := Place(l, "stvor-agent", tree, files, registry(reg)); err != nil {
		t.Fatalf("repeat: %v", err)
	}
	var w cerr.Warnings
	if err := Unplace(l, recs, &w); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(onDisk(l, dst)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("file kept: %v", err)
	}
	if st, err := os.Stat(onDisk(l, filepath.Dir(dst))); err != nil || !st.IsDir() {
		t.Errorf("parent dir removed: %v", err)
	}
	if len(w.List()) != 0 {
		t.Errorf("warnings = %v", w.List())
	}
}

func TestPlaceErrors(t *testing.T) {
	t.Parallel()
	tree := fixture(t, map[string]src{"a": {"A\n", 0o644}})
	if err := os.Mkdir(filepath.Join(tree, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(tree, "out")); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		file manifest.File
		prep func(l paths.Layout)
	}{
		{"missing src", manifest.File{Src: "nope", Dst: "/etc/x"}, nil},
		{"src is dir", manifest.File{Src: "dir", Dst: "/etc/x"}, nil},
		{"src escapes tree", manifest.File{Src: "out", Dst: "/etc/x"}, nil},
		{"dst is dir", manifest.File{Src: "a", Dst: "/etc/x"}, func(l paths.Layout) {
			if err := os.MkdirAll(onDisk(l, "/etc/x"), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l := paths.Layout{Root: t.TempDir()}
			if tc.prep != nil {
				tc.prep(l)
			}
			if _, err := Place(l, "x", tree, []manifest.File{tc.file}, registry(nil)); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

// On a failure in the middle Place returns the records of the files already
// placed, so the caller can record or remove them.
func TestPlacePartial(t *testing.T) {
	t.Parallel()
	tree := fixture(t, map[string]src{"a": {"A\n", 0o644}})
	l := paths.Layout{Root: t.TempDir()}
	files := []manifest.File{{Src: "a", Dst: "/etc/a"}, {Src: "missing", Dst: "/etc/b"}}
	recs, err := Place(l, "x", tree, files, registry(nil))
	if err == nil {
		t.Fatal("want error")
	}
	if len(recs) != 1 || recs[0].Dst != "/etc/a" {
		t.Errorf("recs = %+v", recs)
	}
}

func TestUnplace(t *testing.T) {
	t.Parallel()
	tree := fixture(t, map[string]src{"a": {"A\n", 0o644}, "b": {"B\n", 0o644}})
	l := paths.Layout{Root: t.TempDir()}
	files := []manifest.File{{Src: "a", Dst: "/etc/a.conf"}, {Src: "b", Dst: "/etc/b.conf"}}
	recs, err := Place(l, "x", tree, files, registry(nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(onDisk(l, "/etc/b.conf"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A dangling record (Р11: B purged its directory) is skipped.
	recs = append(recs, state.FileRec{Dst: "/etc/cream/gone/x", SHA256: sum("x")})
	var w cerr.Warnings
	for range 2 { // repeat is a no-op: everything is already gone
		if err := Unplace(l, recs, &w); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Lstat(onDisk(l, "/etc/a.conf")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("unchanged file kept: %v", err)
	}
	if _, err := os.Lstat(onDisk(l, "/etc/b.conf")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("modified file left in place: %v", err)
	}
	got, err := os.ReadFile(onDisk(l, "/etc/b.conf.cream-modified"))
	if err != nil || string(got) != "edited\n" {
		t.Errorf("modified copy: %q %v", got, err)
	}
	list := w.List()
	if len(list) != 1 || list[0].Code != CodeFileModified {
		t.Errorf("warnings = %+v", list)
	}
}

// Unplace never follows or removes something that is not a regular file.
func TestUnplaceNotRegular(t *testing.T) {
	t.Parallel()
	l := paths.Layout{Root: t.TempDir()}
	p := onDisk(l, "/etc/link")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", p); err != nil {
		t.Fatal(err)
	}
	var w cerr.Warnings
	if err := Unplace(l, []state.FileRec{{Dst: "/etc/link", SHA256: sum("x")}}, &w); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(p); err != nil {
		t.Errorf("link removed: %v", err)
	}
	if list := w.List(); len(list) != 1 || list[0].Code != CodeFileNotRegular {
		t.Errorf("warnings = %+v", list)
	}
}
