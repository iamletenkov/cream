package source

import (
	"archive/tar"
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractTarGzHostile(t *testing.T) {
	t.Parallel()
	ok := tarEntry{name: "cream-catalog.yaml", body: "x"}
	tests := []struct {
		name    string
		entries []tarEntry
	}{
		{name: "absolute path", entries: []tarEntry{ok, {name: "/etc/evil", body: "x"}}},
		{name: "dot-dot", entries: []tarEntry{ok, {name: "a/../../evil", body: "x"}}},
		{name: "leading dot-dot", entries: []tarEntry{ok, {name: "../evil", body: "x"}}},
		{name: "symlink outside", entries: []tarEntry{ok, {name: "l", typ: tar.TypeSymlink, link: "../../etc"}}},
		{name: "absolute symlink", entries: []tarEntry{ok, {name: "l", typ: tar.TypeSymlink, link: "/etc/passwd"}}},
		{name: "write through symlink", entries: []tarEntry{ok, {name: "l", typ: tar.TypeSymlink, link: "d"},
			{name: "d/", typ: tar.TypeDir}, {name: "l/x", body: "x"}}},
		{name: "hardlink outside", entries: []tarEntry{ok, {name: "h", typ: tar.TypeLink, link: "../../etc/passwd"}}},
		{name: "absolute hardlink", entries: []tarEntry{ok, {name: "h", typ: tar.TypeLink, link: "/etc/passwd"}}},
		{name: "hardlink to missing", entries: []tarEntry{ok, {name: "h", typ: tar.TypeLink, link: "none"}}},
		{name: "char device", entries: []tarEntry{ok, {name: "dev", typ: tar.TypeChar}}},
		{name: "block device", entries: []tarEntry{ok, {name: "dev", typ: tar.TypeBlock}}},
		{name: "fifo", entries: []tarEntry{ok, {name: "fifo", typ: tar.TypeFifo}}},
		{name: "duplicate entry", entries: []tarEntry{ok, {name: "cream-catalog.yaml", body: "y"}}},
		{name: "newline in name", entries: []tarEntry{ok, {name: "a\nb", body: "x"}}},
		{name: "symlink outside after strip", entries: []tarEntry{{name: "top/", typ: tar.TypeDir},
			{name: "top/a", body: "x"}, {name: "top/l", typ: tar.TypeSymlink, link: "../top/a"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			parent := t.TempDir()
			dst := filepath.Join(parent, "tree")
			err := ExtractTarGz(bytes.NewReader(makeTarGz(t, tc.entries)), dst)
			if err == nil {
				t.Fatal("враждебный архив распакован")
			}
			if _, err := os.Lstat(dst); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("dst остался после отказа: %v", err)
			}
			ents, _ := os.ReadDir(parent)
			if len(ents) != 0 {
				t.Errorf("мусор после отказа: %v", ents)
			}
		})
	}
}

func TestExtractTarGz(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		entries []tarEntry
		want    map[string]string // rel → content; "->x" — ссылка на x; "/" — каталог
	}{
		{
			name: "single top directory is stripped",
			entries: []tarEntry{{name: "cream-extensions-4.0.0/", typ: tar.TypeDir},
				{name: "cream-extensions-4.0.0/cream-catalog.yaml", body: "cat"},
				{name: "cream-extensions-4.0.0/ext/docker/extension.yaml", body: "m"},
				{name: "cream-extensions-4.0.0/ext/docker/hooks/install", body: "#!/bin/sh\n", mode: 0o700},
				{name: "cream-extensions-4.0.0/ext/docker/lib", typ: tar.TypeSymlink, link: "../../shared"},
				{name: "cream-extensions-4.0.0/shared/", typ: tar.TypeDir}},
			want: map[string]string{"cream-catalog.yaml": "cat", "ext/docker/extension.yaml": "m",
				"ext/docker/hooks/install": "#!/bin/sh\n", "ext/docker/lib": "->../../shared", "shared": "/"},
		},
		{
			name:    "implicit top directory is stripped too",
			entries: []tarEntry{{name: "./top/cream-catalog.yaml", body: "cat"}, {name: "./top/a/b", body: "b"}},
			want:    map[string]string{"cream-catalog.yaml": "cat", "a/b": "b"},
		},
		{
			name:    "several top entries are kept",
			entries: []tarEntry{{name: "cream-catalog.yaml", body: "cat"}, {name: "ext/a", body: "a"}},
			want:    map[string]string{"cream-catalog.yaml": "cat", "ext/a": "a"},
		},
		{
			name:    "single top file is not stripped",
			entries: []tarEntry{{name: "cream-catalog.yaml", body: "cat"}},
			want:    map[string]string{"cream-catalog.yaml": "cat"},
		},
		{
			name: "hardlink inside becomes a copy",
			entries: []tarEntry{{name: "a", body: "same"}, {name: "b/", typ: tar.TypeDir},
				{name: "b/h", typ: tar.TypeLink, link: "a"}},
			want: map[string]string{"a": "same", "b/h": "same"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dst := filepath.Join(t.TempDir(), "tree")
			if err := ExtractTarGz(bytes.NewReader(makeTarGz(t, tc.entries)), dst); err != nil {
				t.Fatalf("ExtractTarGz: %v", err)
			}
			got := map[string]string{}
			err := filepath.WalkDir(dst, func(p string, d fs.DirEntry, err error) error {
				if err != nil || p == dst {
					return err
				}
				rel, _ := filepath.Rel(dst, p)
				switch {
				case d.Type()&fs.ModeSymlink != 0:
					l, _ := os.Readlink(p)
					got[rel] = "->" + l
				case d.IsDir():
					got[rel] = "/"
				default:
					got[rel] = readFile(t, p)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			for rel, want := range tc.want {
				if got[rel] != want {
					t.Errorf("%s = %q, ждали %q", rel, got[rel], want)
				}
			}
			for rel, v := range got {
				if _, ok := tc.want[rel]; !ok && v != "/" {
					t.Errorf("лишний %s", rel)
				}
			}
			st, err := os.Stat(dst)
			if err != nil || st.Mode().Perm() != 0o755 {
				t.Errorf("режим корня %v, %v", st.Mode().Perm(), err)
			}
			for rel, v := range got {
				if v == "/" || strings.HasPrefix(v, "->") {
					continue
				}
				st, _ := os.Stat(filepath.Join(dst, rel))
				want := os.FileMode(0o644)
				if strings.HasSuffix(rel, "install") {
					want = 0o755
				}
				if st.Mode().Perm() != want {
					t.Errorf("%s режим %v, ждали %v", rel, st.Mode().Perm(), want)
				}
			}
		})
	}
}

func TestExtractTarGzLimits(t *testing.T) {
	t.Parallel()
	data := makeTarGz(t, []tarEntry{{name: "a", body: strings.Repeat("x", 100)}, {name: "b", body: "y"}})
	tests := []struct {
		name string
		lim  extractLimits
		ok   bool
	}{
		{name: "fits", lim: extractLimits{bytes: 101, entries: 2}, ok: true},
		{name: "too many bytes", lim: extractLimits{bytes: 100, entries: 2}},
		{name: "too many entries", lim: extractLimits{bytes: 1000, entries: 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := extractTarGz(bytes.NewReader(data), filepath.Join(t.TempDir(), "tree"), tc.lim)
			if (err == nil) != tc.ok {
				t.Errorf("err = %v, ждали успех=%v", err, tc.ok)
			}
		})
	}
}

func TestExtractTarGzRefusesExistingDst(t *testing.T) {
	t.Parallel()
	dst := t.TempDir()
	if err := ExtractTarGz(bytes.NewReader(makeTarGz(t, []tarEntry{{name: "a", body: "x"}})), dst); err == nil {
		t.Error("распаковка в существующий каталог")
	}
	if err := ExtractTarGz(strings.NewReader("not gzip"), filepath.Join(dst, "x")); err == nil {
		t.Error("не gzip принят")
	}
}
