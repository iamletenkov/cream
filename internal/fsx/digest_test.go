package fsx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// refTree is the fixed tree behind refDigest. The reference value was
// computed outside Go:
//
//	printf 'F README\0000644\0%s\nD bin\nL bin/link\0run.sh\nF bin/run.sh\0000755\0%s\nD empty\n' \
//	  "$(printf 'hello\n' | sha256sum | cut -d' ' -f1)" \
//	  "$(printf '#!/bin/sh\necho hi\n' | sha256sum | cut -d' ' -f1)" | sha256sum
var refTree = []node{
	{kind: 'f', path: "README", mode: 0o644, content: "hello\n"},
	{kind: 'f', path: "bin/run.sh", mode: 0o755, content: "#!/bin/sh\necho hi\n"},
	{kind: 'l', path: "bin/link", target: "run.sh"},
	{kind: 'd', path: "empty"},
}

const refDigest = "sha256:fc453e71909cada4bd1b8e061f13697be8fcbb194164683169f7e141003c28c4"

func TestTreeDigestReference(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mkTree(t, root, refTree)
	got, err := TreeDigest(root)
	if err != nil {
		t.Fatalf("TreeDigest: %v", err)
	}
	if got != refDigest {
		t.Errorf("TreeDigest = %s, want %s", got, refDigest)
	}
}

func TestTreeDigestChanges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(t *testing.T, root string)
		same   bool
	}{
		{name: "creation order does not matter", same: true, mutate: func(t *testing.T, root string) {
			// Rebuild the same tree in reverse order.
			if err := os.RemoveAll(root); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(root, 0o755); err != nil {
				t.Fatal(err)
			}
			rev := make([]node, len(refTree))
			for i, n := range refTree {
				rev[len(refTree)-1-i] = n
			}
			mkTree(t, root, rev)
		}},
		{name: "group/other bits do not matter", same: true, mutate: func(t *testing.T, root string) {
			chmod(t, filepath.Join(root, "README"), 0o600)
			chmod(t, filepath.Join(root, "bin/run.sh"), 0o700)
		}},
		{name: "mtime does not matter", same: true, mutate: func(t *testing.T, root string) {
			if err := os.Chtimes(filepath.Join(root, "README"), time.Unix(1, 0), time.Unix(1, 0)); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "exec bit set", mutate: func(t *testing.T, root string) {
			chmod(t, filepath.Join(root, "README"), 0o744)
		}},
		{name: "exec bit cleared", mutate: func(t *testing.T, root string) {
			chmod(t, filepath.Join(root, "bin/run.sh"), 0o644)
		}},
		{name: "content", mutate: func(t *testing.T, root string) {
			write(t, filepath.Join(root, "README"), "hello!\n")
		}},
		{name: "rename", mutate: func(t *testing.T, root string) {
			if err := os.Rename(filepath.Join(root, "README"), filepath.Join(root, "README.md")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "link target", mutate: func(t *testing.T, root string) {
			p := filepath.Join(root, "bin/link")
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("../README", p); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "empty dir added", mutate: func(t *testing.T, root string) {
			if err := os.Mkdir(filepath.Join(root, "more"), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "file becomes link with same name", mutate: func(t *testing.T, root string) {
			p := filepath.Join(root, "README")
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("bin/run.sh", p); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := filepath.Join(t.TempDir(), "tree")
			if err := os.Mkdir(root, 0o755); err != nil {
				t.Fatal(err)
			}
			mkTree(t, root, refTree)
			tc.mutate(t, root)
			got, err := TreeDigest(root)
			if err != nil {
				t.Fatalf("TreeDigest: %v", err)
			}
			if (got == refDigest) != tc.same {
				t.Errorf("TreeDigest = %s; same as reference: %v, want %v", got, got == refDigest, tc.same)
			}
			if !strings.HasPrefix(got, "sha256:") || len(got) != len("sha256:")+64 {
				t.Errorf("malformed digest %q", got)
			}
		})
	}
}

func TestTreeDigestRefuses(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	mkTree(t, base, []node{
		{kind: 'p', path: "withfifo/fifo"},
		{kind: 'd', path: "real"},
		{kind: 'l', path: "rootlink", target: "real"},
		{kind: 'f', path: "file", mode: 0o644},
	})
	for _, p := range []string{"withfifo", "rootlink", "file", "missing"} {
		if d, err := TreeDigest(filepath.Join(base, p)); err == nil {
			t.Errorf("TreeDigest(%s) = %s, want error", p, d)
		}
	}
}

func chmod(t *testing.T, p string, m os.FileMode) {
	t.Helper()
	if err := os.Chmod(p, m); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}
