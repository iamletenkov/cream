package fsx

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// ErrUnsafePerm marks a file whose owner or permissions (or those of a
// directory on its path) make it unsafe to trust; the consumer maps it to
// exit code 77.
var ErrUnsafePerm = errors.New("небезопасные владелец или права")

// CheckOwnerPerm is OpenTrusted(path, 0, wantMode) with the file closed
// right away: every directory on the path and the file itself are owned by
// root and safe. A missing file is reported as is (fs.ErrNotExist).
func CheckOwnerPerm(path string, wantMode fs.FileMode) error {
	f, err := OpenTrusted(path, 0, wantMode)
	if err != nil {
		return err
	}
	return f.Close()
}

// OpenTrusted opens the absolute path for reading without following links
// and returns the descriptor that was checked, so the caller reads exactly
// the file it vetted. Every directory from / down is opened relative to its
// parent with O_NOFOLLOW and must be owned by uid and not writable by group
// or others; the file must be a regular file passing CheckOwnerPermFile. A
// link anywhere on the path is ErrUnsafePerm; a missing component is
// reported as is (errors.Is(err, fs.ErrNotExist)).
func OpenTrusted(path string, uid int, wantMode fs.FileMode) (*os.File, error) {
	return openTrusted(path, uid, wantMode, "/")
}

// openTrusted checks only directories at or below top (tests run under a
// temporary directory whose ancestors belong to someone else).
func openTrusted(path string, uid int, want fs.FileMode, top string) (*os.File, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("проверка прав %s: путь не абсолютный", path)
	}
	path, top = filepath.Clean(path), filepath.Clean(top)
	checked := func(dir string) bool {
		return top == "/" || dir == top || strings.HasPrefix(dir, top+"/")
	}
	fd, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("проверка прав %s: %w", path, err)
	}
	cur := "/"
	comps := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, c := range comps {
		if checked(cur) {
			if err := checkDirFD(fd, cur, uid); err != nil {
				_ = syscall.Close(fd)
				return nil, err
			}
		}
		last := i == len(comps)-1
		flags := syscall.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
		if last {
			flags |= syscall.O_NONBLOCK // a FIFO must not block the open
		} else {
			flags |= syscall.O_DIRECTORY
		}
		next := filepath.Join(cur, c)
		nfd, err := syscall.Openat(fd, c, flags, 0)
		_ = syscall.Close(fd)
		if errors.Is(err, syscall.ELOOP) || (!last && errors.Is(err, syscall.ENOTDIR)) {
			return nil, fmt.Errorf("%s: ссылка или не каталог на пути: %w", next, ErrUnsafePerm)
		}
		if err != nil {
			return nil, fmt.Errorf("проверка прав %s: %w", next, err)
		}
		fd, cur = nfd, next
	}
	f := os.NewFile(uintptr(fd), path)
	st, err := f.Stat()
	if err == nil && !st.Mode().IsRegular() {
		err = fmt.Errorf("%s: не обычный файл: %w", path, ErrUnsafePerm)
	}
	if err == nil {
		err = CheckOwnerPermFile(f, uid, want)
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// checkDirFD requires an open directory to be owned by uid and not writable
// by group or others.
func checkDirFD(fd int, name string, uid int) error {
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		return fmt.Errorf("проверка прав %s: %w", name, err)
	}
	return checkOwnerMode(name, int(st.Uid), fs.FileMode(st.Mode&0o777), uid, 0o777)
}

// CheckOwnerPermFile checks the open file f: owned by uid, not writable by
// group or others, no permission bits beyond wantMode.
func CheckOwnerPermFile(f *os.File, uid int, wantMode fs.FileMode) error {
	st, err := f.Stat()
	if err != nil {
		return fmt.Errorf("проверка прав %s: %w", f.Name(), err)
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%s: нет сведений о владельце: %w", f.Name(), ErrUnsafePerm)
	}
	return checkOwnerMode(f.Name(), int(sys.Uid), st.Mode().Perm(), uid, wantMode.Perm())
}

func checkOwnerMode(name string, owner int, perm fs.FileMode, uid int, want fs.FileMode) error {
	if owner != uid {
		return fmt.Errorf("%s: владелец uid %d, нужен %d: %w", name, owner, uid, ErrUnsafePerm)
	}
	if perm&0o022 != 0 {
		return fmt.Errorf("%s: права %04o разрешают запись группе или всем: %w", name, perm, ErrUnsafePerm)
	}
	if perm&^want != 0 {
		return fmt.Errorf("%s: права %04o шире %04o: %w", name, perm, want, ErrUnsafePerm)
	}
	return nil
}
