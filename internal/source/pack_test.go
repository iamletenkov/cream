package source

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"testing"
	"time"
)

func TestPackDeterministic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"cream-catalog.yaml": "c", "ext/b/extension.yaml": "b",
		"ext/a/extension.yaml": "a", "ext/a/hooks/install": "#!/bin/sh\n", "empty/": ""})
	if err := os.Chmod(filepath.Join(dir, "ext/a/hooks/install"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../b", filepath.Join(dir, "ext/a/lib")); err != nil {
		t.Fatal(err)
	}
	epoch := time.Unix(1760000000, 0)
	pack := func() []byte {
		var buf bytes.Buffer
		if err := Pack(dir, &buf, epoch); err != nil {
			t.Fatalf("Pack: %v", err)
		}
		return buf.Bytes()
	}
	first := pack()
	// Время изменения файлов не влияет на архив.
	old := time.Unix(1000, 0)
	if err := os.Chtimes(filepath.Join(dir, "ext/b/extension.yaml"), old, old); err != nil {
		t.Fatal(err)
	}
	second := pack()
	if sha256Hex(first) != sha256Hex(second) {
		t.Fatal("два прохода дали разные архивы")
	}

	zr, err := gzip.NewReader(bytes.NewReader(first))
	if err != nil {
		t.Fatal(err)
	}
	if zr.Name != "" || !zr.ModTime.IsZero() || zr.Comment != "" {
		t.Errorf("gzip-заголовок не пуст: name=%q mtime=%v", zr.Name, zr.ModTime)
	}
	tr := tar.NewReader(zr)
	var names []string
	modes := map[string]int64{}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, h.Name)
		modes[h.Name] = h.Mode
		if h.Uid != 0 || h.Gid != 0 || h.Uname != "" || h.Gname != "" {
			t.Errorf("%s: владелец %d:%d %q:%q", h.Name, h.Uid, h.Gid, h.Uname, h.Gname)
		}
		if !h.ModTime.Equal(epoch) {
			t.Errorf("%s: mtime %v", h.Name, h.ModTime)
		}
		if h.Name == "ext/a/lib" && (h.Typeflag != tar.TypeSymlink || h.Linkname != "../b") {
			t.Errorf("ссылка: %+v", h)
		}
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("не отсортировано: %v", names)
	}
	want := []string{"cream-catalog.yaml", "empty/", "ext/", "ext/a/", "ext/a/extension.yaml", "ext/a/hooks/",
		"ext/a/hooks/install", "ext/a/lib", "ext/b/", "ext/b/extension.yaml"}
	if len(names) != len(want) {
		t.Fatalf("записи %v, ждали %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("[%d] %q, ждали %q", i, names[i], want[i])
		}
	}
	if modes["ext/a/hooks/install"] != 0o755 || modes["ext/a/extension.yaml"] != 0o644 || modes["ext/"] != 0o755 {
		t.Errorf("режимы: %v", modes)
	}

	// Архив Pack распаковывается ExtractTarGz без снятия верхнего каталога.
	out := filepath.Join(t.TempDir(), "x")
	if err := ExtractTarGz(bytes.NewReader(first), out); err != nil {
		t.Fatal(err)
	}
	if readFile(t, filepath.Join(out, "ext/a/extension.yaml")) != "a" {
		t.Error("распакованное дерево не совпало")
	}
}

func TestPackRefuses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(t *testing.T, dir string)
	}{
		{name: "link outside", setup: func(t *testing.T, dir string) {
			if err := os.Symlink("../../etc", filepath.Join(dir, "l")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "fifo", setup: func(t *testing.T, dir string) {
			if err := syscall.Mkfifo(filepath.Join(dir, "f"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeTree(t, dir, map[string]string{"cream-catalog.yaml": "c"})
			tc.setup(t, dir)
			if err := Pack(dir, io.Discard, time.Unix(0, 0)); err == nil {
				t.Error("Pack не отказал")
			}
		})
	}
}
