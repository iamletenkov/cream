package fsx

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// TreeDigest returns "sha256:<hex>" of the tree under root (spec §9.5): the
// sha256 of the records of every entry below root, sorted by their
// slash-separated relative path (byte order):
//
//	F <path>\0<0644|0755>\0<sha256 of content>\n
//	L <path>\0<link target>\n
//	D <path>\n
//
// The file mode is reduced to the execute bit; owners, times and other
// permission bits do not count. Links are recorded, not followed. root must
// be a directory, not a link; special files and names with a newline are
// refused.
func TreeDigest(root string) (string, error) {
	st, err := os.Lstat(root)
	if err != nil {
		return "", fmt.Errorf("отпечаток %s: %w", root, err)
	}
	if !st.IsDir() {
		return "", fmt.Errorf("отпечаток %s: не каталог", root)
	}
	type entry struct {
		rel  string
		full string
		typ  fs.FileMode
	}
	var entries []entry
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if strings.ContainsRune(rel, '\n') {
			return fmt.Errorf("%q: перевод строки в имени", rel)
		}
		t := d.Type()
		if !t.IsDir() && !t.IsRegular() && t&fs.ModeSymlink == 0 {
			return fmt.Errorf("%s: особый файл (%v) не поддерживается", rel, t)
		}
		entries = append(entries, entry{rel: rel, full: p, typ: t})
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
			target, err := os.Readlink(e.full)
			if err != nil {
				return "", fmt.Errorf("отпечаток %s: %w", e.full, err)
			}
			rec = "L " + e.rel + "\x00" + target + "\n"
		default:
			sum, mode, err := fileDigest(e.full)
			if err != nil {
				return "", fmt.Errorf("отпечаток %s: %w", e.full, err)
			}
			rec = fmt.Sprintf("F %s\x00%04o\x00%s\n", e.rel, mode, sum)
		}
		_, _ = io.WriteString(h, rec)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func fileDigest(p string) (sum string, mode fs.FileMode, err error) {
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return "", 0, err
	}
	if !st.Mode().IsRegular() {
		return "", 0, errors.New("файл подменён во время обхода")
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), normMode(st.Mode()), nil
}
