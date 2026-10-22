package source

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
)

// memTarEntry — запись тестового архива.
type memTarEntry struct {
	name, body, link string
	typ              byte
}

func memTarGz(t *testing.T, entries ...memTarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Typeflag: e.typ, Mode: 0o644, Linkname: e.link}
		if e.typ == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		if e.typ == tar.TypeDir {
			h.Mode = 0o755
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if e.typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestReadTarGzKeepsWantedAndStripsTop(t *testing.T) {
	t.Parallel()
	data := memTarGz(t,
		memTarEntry{name: "cat-1.0/", typ: tar.TypeDir},
		memTarEntry{name: "cat-1.0/cream-catalog.yaml", body: "catalog", typ: tar.TypeReg},
		memTarEntry{name: "cat-1.0/extensions/a/extension.yaml", body: "a", typ: tar.TypeReg},
		memTarEntry{name: "cat-1.0/extensions/a/hooks/install", body: "#!/bin/sh\n", typ: tar.TypeReg},
		memTarEntry{name: "cat-1.0/extensions/a/link", link: "hooks/install", typ: tar.TypeSymlink},
	)
	tree, err := ReadTarGz(bytes.NewReader(data), func(rel string) bool {
		return strings.HasSuffix(rel, ".yaml")
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(tree.Files["cream-catalog.yaml"]); got != "catalog" {
		t.Errorf("cream-catalog.yaml = %q", got)
	}
	if got := string(tree.Files["extensions/a/extension.yaml"]); got != "a" {
		t.Errorf("extension.yaml = %q", got)
	}
	if _, ok := tree.Files["extensions/a/hooks/install"]; ok {
		t.Error("ненужный файл прочитан в память")
	}
	for _, d := range []string{"extensions", "extensions/a", "extensions/a/hooks"} {
		if !tree.Dirs[d] {
			t.Errorf("нет каталога %s в %v", d, tree.Dirs)
		}
	}
	if tree.Dirs["extensions/a/link"] || tree.Dirs["cat-1.0"] {
		t.Errorf("лишний каталог: %v", tree.Dirs)
	}
}

func TestReadTarGzRefusesHostile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		entry memTarEntry
	}{
		{"dotdot", memTarEntry{name: "a/../../etc/passwd", body: "x", typ: tar.TypeReg}},
		{"absolute", memTarEntry{name: "/etc/passwd", body: "x", typ: tar.TypeReg}},
		{"link-out", memTarEntry{name: "a/l", link: "../../etc", typ: tar.TypeSymlink}},
		{"link-abs", memTarEntry{name: "a/l", link: "/etc", typ: tar.TypeSymlink}},
		{"fifo", memTarEntry{name: "a/f", typ: tar.TypeFifo}},
		{"under-file", memTarEntry{name: "a/x.yaml/y", body: "y", typ: tar.TypeReg}},
		{"under-link", memTarEntry{name: "a/l/y", body: "y", typ: tar.TypeReg}},
		{"hardlink-to-symlink", memTarEntry{name: "a/h", link: "a/l", typ: tar.TypeLink}},
		{"hardlink-missing", memTarEntry{name: "a/h", link: "a/none", typ: tar.TypeLink}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := memTarGz(t, memTarEntry{name: "a/x.yaml", body: "x", typ: tar.TypeReg},
				memTarEntry{name: "a/l", link: "x.yaml", typ: tar.TypeSymlink}, tc.entry)
			if _, err := ReadTarGz(bytes.NewReader(data), func(string) bool { return true }); err == nil {
				t.Error("враждебный архив принят")
			}
		})
	}
}

func TestReadTarGzLimits(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("x", 100)
	data := memTarGz(t, memTarEntry{name: "a.yaml", body: big, typ: tar.TypeReg},
		memTarEntry{name: "b.yaml", body: big, typ: tar.TypeReg})
	if _, err := readTarGz(bytes.NewReader(data), func(string) bool { return true },
		memLimits{entries: 10, bytes: 150, file: 1000}); err == nil {
		t.Error("предел байт не сработал")
	}
	if _, err := readTarGz(bytes.NewReader(data), func(string) bool { return true },
		memLimits{entries: 1, bytes: 1000, file: 1000}); err == nil {
		t.Error("предел записей не сработал")
	}
	if _, err := readTarGz(bytes.NewReader(data), func(string) bool { return true },
		memLimits{entries: 10, bytes: 1000, file: 50}); err == nil {
		t.Error("предел файла не сработал")
	}
	linked := memTarGz(t, memTarEntry{name: "a.yaml", body: big, typ: tar.TypeReg},
		memTarEntry{name: "b.yaml", link: "a.yaml", typ: tar.TypeLink})
	if _, err := readTarGz(bytes.NewReader(linked), func(string) bool { return false },
		memLimits{entries: 10, bytes: 150, file: 1000}); err == nil {
		t.Error("жёсткая ссылка обходит предел байт")
	}
	if _, err := readTarGz(bytes.NewReader(data), func(string) bool { return false },
		memLimits{entries: 10, bytes: 1000, file: 50}); err != nil {
		t.Errorf("ненужный большой файл не должен мешать: %v", err)
	}
}

// Жёсткая ссылка с нужным именем на ненужный файл — отказ, а не тихая пропажа тела.
func TestReadTarGzHardlinkToUnkept(t *testing.T) {
	t.Parallel()
	data := memTarGz(t, memTarEntry{name: "a/x.txt", body: "x", typ: tar.TypeReg},
		memTarEntry{name: "a/h.yaml", link: "a/x.txt", typ: tar.TypeLink})
	if _, err := ReadTarGz(bytes.NewReader(data), func(rel string) bool { return strings.HasSuffix(rel, ".yaml") }); err == nil {
		t.Error("принято")
	}
}
