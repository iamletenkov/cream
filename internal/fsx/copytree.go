package fsx

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
)

// CopyTree copies the directory src to dst, which must not exist (its
// parents are created). Both trees are accessed through os.Root, so every
// child is opened relative to its root and a link swapped in mid-copy cannot
// lead outside. Links are never followed: a relative link that stays inside
// src is recreated with the same target; an absolute link, a link leading
// outside src, `..` after a regular component or a newline in the target is
// refused. Regular files get 0755 if any execute bit is set, otherwise 0644;
// a file with more than one hard link is refused. Directories get 0755.
// Devices, FIFOs and sockets are refused. On failure dst is removed.
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
	sr, err := os.OpenRoot(src)
	if err != nil {
		return fmt.Errorf("копия %s: %w", src, err)
	}
	defer func() { _ = sr.Close() }()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("копия в %s: %w", dst, err)
	}
	if err := os.Mkdir(dst, 0o755); err != nil {
		return fmt.Errorf("копия в %s: %w", dst, err)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dst)
		}
	}()
	dr, err := os.OpenRoot(dst)
	if err != nil {
		return fmt.Errorf("копия в %s: %w", dst, err)
	}
	defer func() { _ = dr.Close() }()
	if err := dr.Chmod(".", 0o755); err != nil {
		return fmt.Errorf("копия в %s: %w", dst, err)
	}
	return fs.WalkDir(sr.FS(), ".", func(rel string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return fmt.Errorf("копия %s: %w", filepath.Join(src, rel), werr)
		}
		if rel == "." {
			return nil
		}
		var err error
		switch t := d.Type(); {
		case t.IsDir():
			if err = dr.Mkdir(rel, 0o755); err == nil {
				err = dr.Chmod(rel, 0o755)
			}
		case t&fs.ModeSymlink != 0:
			var link string
			if link, err = sr.Readlink(rel); err == nil {
				if err = checkLink(rel, link); err == nil {
					err = dr.Symlink(link, rel)
				}
			}
		case t.IsRegular():
			err = copyFile(sr, dr, rel)
		default:
			err = fmt.Errorf("особый файл (%v) не поддерживается", t)
		}
		if err != nil {
			return fmt.Errorf("копия %s: %w", filepath.Join(src, rel), err)
		}
		return nil
	})
}

// checkLink accepts a link at rel (slash-separated, relative to the tree
// root) pointing at target only when the target is relative, has no
// newline, uses `..` only as leading components and does not climb above
// the root. `..` after a regular component is refused because that
// component may itself be a link and the kernel would resolve `..` against
// its target.
func checkLink(rel, target string) error {
	if target == "" {
		return errors.New("пустая цель ссылки")
	}
	if strings.ContainsRune(target, '\n') {
		return fmt.Errorf("перевод строки в цели ссылки %q", target)
	}
	if path.IsAbs(target) {
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

// copyFile copies the regular file rel from sr to dr. The source is opened
// with O_NOFOLLOW and checked on the same descriptor.
func copyFile(sr, dr *os.Root, rel string) error {
	in, err := openRegular(sr, rel)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	mode := normMode(st.Mode())
	out, err := dr.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
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

// openRegular opens rel inside r without following a final link and checks,
// on the opened descriptor, that it is a regular file with one hard link: a
// second link could tie the content to a file outside the tree.
func openRegular(r *os.Root, rel string) (*os.File, error) {
	f, err := r.OpenFile(rel, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err == nil && !st.Mode().IsRegular() {
		err = errors.New("файл подменён во время обхода")
	}
	if err == nil {
		if sys, ok := st.Sys().(*syscall.Stat_t); !ok || sys.Nlink != 1 {
			err = errors.New("у файла несколько жёстких ссылок")
		}
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// normMode maps a file mode to the only two modes cream keeps: 0755 when
// any execute bit is set, otherwise 0644.
func normMode(m fs.FileMode) fs.FileMode {
	if m.Perm()&0o111 != 0 {
		return 0o755
	}
	return 0o644
}
