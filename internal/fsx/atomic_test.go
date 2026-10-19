package fsx

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomic(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		old     []byte // nil — no previous file
		oldPerm fs.FileMode
		data    []byte
		perm    fs.FileMode
	}{
		{name: "new file", data: []byte("A=1\n"), perm: 0o644},
		{name: "new secret file", data: []byte("S=x\n"), perm: 0o600},
		{name: "empty content", data: []byte{}, perm: 0o644},
		{name: "replace keeps nothing of old", old: []byte("old content, longer\n"), oldPerm: 0o644, data: []byte("new\n"), perm: 0o644},
		{name: "replace tightens mode", old: []byte("x"), oldPerm: 0o644, data: []byte("y"), perm: 0o600},
		{name: "mode ignores umask", data: []byte("x"), perm: 0o666},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "config")
			if tc.old != nil {
				if err := os.WriteFile(path, tc.old, tc.oldPerm); err != nil {
					t.Fatal(err)
				}
			}
			if err := WriteAtomic(path, tc.data, tc.perm); err != nil {
				t.Fatalf("WriteAtomic: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(tc.data) {
				t.Errorf("content = %q, want %q", got, tc.data)
			}
			st, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if st.Mode().Perm() != tc.perm {
				t.Errorf("mode = %o, want %o", st.Mode().Perm(), tc.perm)
			}
			assertOnlyEntries(t, dir, "config")
		})
	}
}

// A crash between write and rename must leave the old file whole and no
// temporary file behind.
func TestWriteAtomicFailureBeforeRename(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		old  []byte
	}{
		{name: "existing file stays intact", old: []byte("A=old\n")},
		{name: "no file stays absent"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "config")
			if tc.old != nil {
				if err := os.WriteFile(path, tc.old, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			boom := errors.New("injected crash")
			var sawTemp string
			err := writeAtomic(path, []byte("A=new\n"), 0o644, func(tmp string) error {
				sawTemp = tmp
				data, err := os.ReadFile(tmp)
				if err != nil || string(data) != "A=new\n" {
					t.Errorf("temp file before rename: %q, %v", data, err)
				}
				if filepath.Dir(tmp) != dir {
					t.Errorf("temp file %s is not in the target directory", tmp)
				}
				return boom
			})
			if !errors.Is(err, boom) {
				t.Fatalf("err = %v, want injected", err)
			}
			if sawTemp == "" {
				t.Fatal("hook was not called")
			}
			got, err := os.ReadFile(path)
			if tc.old == nil {
				if !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("target appeared: %q, %v", got, err)
				}
				assertOnlyEntries(t, dir)
				return
			}
			if err != nil || string(got) != string(tc.old) {
				t.Errorf("old file = %q, %v; want %q", got, err, tc.old)
			}
			assertOnlyEntries(t, dir, "config")
		})
	}
}

func TestWriteAtomicErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := WriteAtomic(filepath.Join(dir, "missing", "config"), []byte("x"), 0o644); err == nil {
		t.Error("write into a missing directory: want error")
	}
	sub := filepath.Join(dir, "isdir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(sub, []byte("x"), 0o644); err == nil {
		t.Error("replace a directory: want error")
	}
	assertOnlyEntries(t, dir, "isdir")
}

func assertOnlyEntries(t *testing.T, dir string, want ...string) {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range ents {
		got = append(got, e.Name())
	}
	if len(got) != len(want) {
		t.Fatalf("dir entries = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("dir entries = %v, want %v", got, want)
		}
	}
}
