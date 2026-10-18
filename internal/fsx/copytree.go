package fsx

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// CopyTree copies the directory src to dst, which must not exist (its
// parents are created). Links are never followed: a relative link that
// stays inside src is recreated with the same target; an absolute link, a
// link leading outside src, or `..` after a regular component is refused.
// Regular files get 0755 if any execute bit is set, otherwise 0644;
// directories get 0755. Devices, FIFOs and sockets are refused. On failure
// dst is removed.
func CopyTree(src, dst string) (err error) {
	st, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("копия %s: %w", src, err)
	}
	if !st.IsDir() {
		return fmt.Errorf("копия %s: не каталог", src)
	}
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("копия в %s: уже существует", dst)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("копия в %s: %w", dst, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("копия в %s: %w", dst, err)
	}
	if err := mkdir(dst); err != nil {
		return fmt.Errorf("копия в %s: %w", dst, err)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dst)
		}
	}()
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return fmt.Errorf("копия %s: %w", p, werr)
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		switch t := d.Type(); {
		case t.IsDir():
			err = mkdir(target)
		case t&fs.ModeSymlink != 0:
			var link string
			if link, err = os.Readlink(p); err == nil {
				if err = checkLink(filepath.ToSlash(rel), link); err == nil {
					err = os.Symlink(link, target)
				}
			}
		case t.IsRegular():
			err = copyFile(p, target)
		default:
			err = fmt.Errorf("особый файл (%v) не поддерживается", t)
		}
		if err != nil {
			return fmt.Errorf("копия %s: %w", filepath.Join(src, rel), err)
		}
		return nil
	})
}

func mkdir(p string) error {
	if err := os.Mkdir(p, 0o755); err != nil {
		return err
	}
	return os.Chmod(p, 0o755)
}

// checkLink accepts a link at rel (slash-separated, relative to the tree
// root) pointing at target only when the target is relative, uses `..` only
// as leading components and does not climb above the root. `..` after a
// regular component is refused because that component may itself be a link
// and the kernel would resolve `..` against its target.
func checkLink(rel, target string) error {
	if target == "" {
		return errors.New("пустая цель ссылки")
	}
	if strings.HasPrefix(target, "/") {
		return fmt.Errorf("абсолютная ссылка на %s", target)
	}
	depth := strings.Count(rel, "/")
	up, seenName := 0, false
	for _, c := range strings.Split(target, "/") {
		switch c {
		case "", ".":
		case "..":
			if seenName {
				return fmt.Errorf("ссылка на %s: `..` после имени", target)
			}
			up++
			if up > depth {
				return fmt.Errorf("ссылка на %s ведёт за пределы дерева", target)
			}
		default:
			seenName = true
		}
	}
	return nil
}

// copyFile copies the regular file p without following a link swapped in
// after the walk saw it.
func copyFile(p, target string) error {
	in, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return errors.New("файл подменён во время копии")
	}
	mode := normMode(st.Mode())
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Chmod(mode); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// normMode maps a file mode to the only two modes cream keeps: 0755 when
// any execute bit is set, otherwise 0644.
func normMode(m fs.FileMode) fs.FileMode {
	if m.Perm()&0o111 != 0 {
		return 0o755
	}
	return 0o644
}
