package fsx

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// mkTree builds a tree under root from a compact spec:
// "d:path" dir, "f:path:mode:content" file, "l:path:target" symlink,
// "p:path" FIFO, "h:path:target" hard link to target (relative to root).
type node struct {
	kind    byte
	path    string
	mode    os.FileMode
	content string
	target  string
}

func mkTree(t *testing.T, root string, nodes []node) {
	t.Helper()
	for _, n := range nodes {
		p := filepath.Join(root, filepath.FromSlash(n.path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		var err error
		switch n.kind {
		case 'd':
			err = os.Mkdir(p, 0o755)
		case 'f':
			if err = os.WriteFile(p, []byte(n.content), 0o600); err == nil {
				err = os.Chmod(p, n.mode)
			}
		case 'l':
			err = os.Symlink(n.target, p)
		case 'p':
			err = syscall.Mkfifo(p, 0o644)
		case 'h':
			err = os.Link(filepath.Join(root, filepath.FromSlash(n.target)), p)
		}
		if err != nil {
			t.Fatalf("mkTree %c %s: %v", n.kind, n.path, err)
		}
	}
}

func TestCopyTree(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "src")
	mkTree(t, src, []node{
		{kind: 'f', path: "hooks/install", mode: 0o755, content: "#!/bin/sh\n"},
		{kind: 'f', path: "hooks/private", mode: 0o700, content: "x"},
		{kind: 'f', path: "extension.yaml", mode: 0o644, content: "schema: cream/v1\n"},
		{kind: 'f', path: "secret.txt", mode: 0o600, content: "s"},
		{kind: 'f', path: "group-x", mode: 0o610, content: "g"},
		{kind: 'd', path: "empty"},
		{kind: 'l', path: "hooks/apply", target: "install"},
		{kind: 'l', path: "lib/up", target: "../extension.yaml"},
		{kind: 'l', path: "dirlink", target: "hooks"},
		{kind: 'l', path: "self", target: "."},
	})
	dst := filepath.Join(t.TempDir(), "nested", "dst")
	if err := CopyTree(src, dst); err != nil {
		t.Fatalf("CopyTree: %v", err)
	}
	files := map[string]struct {
		mode    os.FileMode
		content string
	}{
		"hooks/install":  {0o755, "#!/bin/sh\n"},
		"hooks/private":  {0o755, "x"},
		"extension.yaml": {0o644, "schema: cream/v1\n"},
		"secret.txt":     {0o644, "s"},
		"group-x":        {0o755, "g"},
	}
	for p, w := range files {
		full := filepath.Join(dst, p)
		st, err := os.Lstat(full)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			continue
		}
		if !st.Mode().IsRegular() || st.Mode().Perm() != w.mode {
			t.Errorf("%s: mode %v, want regular %o", p, st.Mode(), w.mode)
		}
		if b, _ := os.ReadFile(full); string(b) != w.content {
			t.Errorf("%s: content %q, want %q", p, b, w.content)
		}
	}
	links := map[string]string{"hooks/apply": "install", "lib/up": "../extension.yaml", "dirlink": "hooks", "self": "."}
	for p, want := range links {
		got, err := os.Readlink(filepath.Join(dst, p))
		if err != nil || got != want {
			t.Errorf("%s: link %q, %v; want %q", p, got, err, want)
		}
	}
	for _, d := range []string{".", "empty", "hooks", "lib"} {
		st, err := os.Lstat(filepath.Join(dst, d))
		if err != nil || !st.IsDir() || st.Mode().Perm() != 0o755 {
			t.Errorf("dir %s: %v, %v; want 0755 dir", d, st, err)
		}
	}
}

func TestCopyTreeRefuses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		nodes []node
	}{
		{name: "absolute link", nodes: []node{{kind: 'l', path: "a", target: "/etc/passwd"}}},
		{name: "link to parent of root", nodes: []node{{kind: 'l', path: "a", target: ".."}}},
		{name: "link escaping from subdir", nodes: []node{{kind: 'l', path: "d/a", target: "../../x"}}},
		{name: "dot-dot after a component", nodes: []node{
			{kind: 'l', path: "d/s", target: "."},
			{kind: 'l', path: "d/a", target: "s/../../x"},
		}},
		{name: "dot then dot-dot escape", nodes: []node{{kind: 'l', path: "a", target: "./../src/x"}}},
		{name: "FIFO", nodes: []node{{kind: 'p', path: "fifo"}}},
		{name: "newline in link target", nodes: []node{{kind: 'f', path: "x\nD y", mode: 0o644}, {kind: 'l', path: "a", target: "x\nD y"}}},
		{name: "hard link inside", nodes: []node{{kind: 'f', path: "f", mode: 0o644, content: "x"}, {kind: 'h', path: "g", target: "f"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := filepath.Join(t.TempDir(), "src")
			if err := os.Mkdir(src, 0o755); err != nil {
				t.Fatal(err)
			}
			mkTree(t, src, tc.nodes)
			dst := filepath.Join(t.TempDir(), "dst")
			if err := CopyTree(src, dst); err == nil {
				t.Fatal("CopyTree: want error")
			}
			if _, err := os.Lstat(dst); !os.IsNotExist(err) {
				t.Errorf("dst left behind after failure: %v", err)
			}
		})
	}
}

func TestCopyTreeBadRoots(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	mkTree(t, base, []node{
		{kind: 'd', path: "src"},
		{kind: 'l', path: "srclink", target: "src"},
		{kind: 'f', path: "file", mode: 0o644},
		{kind: 'd', path: "exists"},
	})
	tests := []struct{ name, src, dst string }{
		{name: "src is a symlink", src: "srclink", dst: "d1"},
		{name: "src is a file", src: "file", dst: "d2"},
		{name: "src missing", src: "nope", dst: "d3"},
		{name: "dst exists", src: "src", dst: "exists"},
	}
	for _, tc := range tests {
		if err := CopyTree(filepath.Join(base, tc.src), filepath.Join(base, tc.dst)); err == nil {
			t.Errorf("%s: want error", tc.name)
		}
	}
}

// A file hard-linked from outside the tree is refused: its content belongs
// to someone else and may change under the copy.
func TestCopyTreeRefusesHardLinkFromOutside(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	mkTree(t, base, []node{{kind: 'f', path: "outside", mode: 0o644, content: "x"}, {kind: 'd', path: "src"}})
	if err := os.Link(filepath.Join(base, "outside"), filepath.Join(base, "src", "in")); err != nil {
		t.Fatal(err)
	}
	if err := CopyTree(filepath.Join(base, "src"), filepath.Join(base, "dst")); err == nil {
		t.Fatal("CopyTree: want error for a hard link")
	}
}

// The root opened by path must be the directory vetted by Lstat: a root
// swapped between the two is refused.
func TestOpenRootSame(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	mkTree(t, base, []node{{kind: 'd', path: "a"}, {kind: 'd', path: "b"}})
	stA, err := os.Lstat(filepath.Join(base, "a"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := openRootSame(filepath.Join(base, "a"), stA)
	if err != nil {
		t.Fatalf("same directory: %v", err)
	}
	_ = r.Close()
	if r, err := openRootSame(filepath.Join(base, "b"), stA); err == nil {
		_ = r.Close()
		t.Fatal("swapped root: want error")
	}
}

// os.Root.OpenFile follows a link that stays inside the root even with
// O_NOFOLLOW, so a file swapped for such a link mid-walk must be refused
// after the open.
func TestOpenRegularRefusesInRootLink(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	mkTree(t, base, []node{
		{kind: 'f', path: "file", mode: 0o644, content: "x"},
		{kind: 'l', path: "link", target: "file"},
		{kind: 'l', path: "sub/link", target: "../file"},
	})
	r, err := os.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	for _, rel := range []string{"link", "sub/link"} {
		if f, _, err := openRegular(r, rel); err == nil {
			_ = f.Close()
			t.Errorf("openRegular(%s): want error", rel)
		}
		if _, _, err := fileDigest(r, rel); err == nil {
			t.Errorf("fileDigest(%s): want error", rel)
		}
	}
	f, _, err := openRegular(r, "file")
	if err != nil {
		t.Fatalf("openRegular(file): %v", err)
	}
	_ = f.Close()
	if _, _, err := fileDigest(r, "file"); err != nil {
		t.Fatalf("fileDigest(file): %v", err)
	}
}
