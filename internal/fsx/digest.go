package fsx

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"
)

// TreeDigest returns "sha256:<hex>" of the tree under root (spec §9.5): the
// sha256 of the records of every entry below root, sorted by their
// slash-separated relative path (byte order):
//
//	F <path>\0<0644|0755>\0<sha256 of content>\n
//	L <path>\0<link target>\n
//	D <path>\n
//
// The encoding is unambiguous because neither a path nor a link target may
// contain NUL (the kernel forbids it) or a newline (refused here): every
// record is exactly one line and its fields are split by NUL.
//
// The file mode is reduced to the execute bit; owners, times and other
// permission bits do not count. Links are recorded, not followed. The tree
// is read through os.Root, so a link swapped in mid-walk cannot lead outside,
// and every file is checked after the open to be the entry itself, not a
// link inside the tree (openInRoot).
// root must be a directory, not a link; special files are refused.
func TreeDigest(root string) (string, error) {
	st, err := os.Lstat(root)
	if err != nil {
		return "", fmt.Errorf("отпечаток %s: %w", root, err)
	}
	if !st.IsDir() {
		return "", fmt.Errorf("отпечаток %s: не каталог", root)
	}
	r, err := openRootSame(root, st)
	if err != nil {
		return "", fmt.Errorf("отпечаток %s: %w", root, err)
	}
	defer func() { _ = r.Close() }()
	type entry struct {
		rel string
		typ fs.FileMode
	}
	var entries []entry
	err = fs.WalkDir(r.FS(), ".", func(rel string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if rel == "." {
			return nil
		}
		if strings.ContainsRune(rel, '\n') {
			return fmt.Errorf("%q: перевод строки в имени", rel)
		}
		t := d.Type()
		if !t.IsDir() && !t.IsRegular() && t&fs.ModeSymlink == 0 {
			return fmt.Errorf("%s: особый файл (%v) не поддерживается", rel, t)
		}
		entries = append(entries, entry{rel: rel, typ: t})
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("отпечаток %s: %w", root, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })
	h := sha256.New()
	for _, e := range entries {
		var rec string
		switch {
		case e.typ.IsDir():
			rec = "D " + e.rel + "\n"
		case e.typ&fs.ModeSymlink != 0:
			target, err := r.Readlink(e.rel)
			if err != nil {
				return "", fmt.Errorf("отпечаток %s/%s: %w", root, e.rel, err)
			}
			if strings.ContainsRune(target, '\n') {
				return "", fmt.Errorf("отпечаток %s/%s: перевод строки в цели ссылки", root, e.rel)
			}
			rec = "L " + e.rel + "\x00" + target + "\n"
		default:
			sum, mode, err := fileDigest(r, e.rel)
			if err != nil {
				return "", fmt.Errorf("отпечаток %s/%s: %w", root, e.rel, err)
			}
			rec = fmt.Sprintf("F %s\x00%04o\x00%s\n", e.rel, mode, sum)
		}
		_, _ = io.WriteString(h, rec)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func fileDigest(r *os.Root, rel string) (sum string, mode fs.FileMode, err error) {
	f, st, err := openInRoot(r, rel)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), normMode(st.Mode()), nil
}
