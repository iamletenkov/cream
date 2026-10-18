package fsx

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// ErrUnsafePerm marks a file whose owner or permissions make it unsafe to
// trust (the consumer maps it to exit code 77).
var ErrUnsafePerm = errors.New("небезопасные владелец или права")

// CheckOwnerPerm verifies that path is not a link, is owned by root, is not
// writable by group or others and has no permission bits beyond wantMode.
// A missing file is reported as is (errors.Is(err, fs.ErrNotExist)), not as
// ErrUnsafePerm.
func CheckOwnerPerm(path string, wantMode fs.FileMode) error {
	return checkOwnerPerm(path, wantMode, 0)
}

func checkOwnerPerm(path string, want fs.FileMode, uid int) error {
	st, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("проверка прав %s: %w", path, err)
	}
	if st.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%s: символическая ссылка: %w", path, ErrUnsafePerm)
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%s: нет сведений о владельце: %w", path, ErrUnsafePerm)
	}
	if int(sys.Uid) != uid {
		return fmt.Errorf("%s: владелец uid %d, нужен %d: %w", path, sys.Uid, uid, ErrUnsafePerm)
	}
	perm := st.Mode().Perm()
	if perm&0o022 != 0 {
		return fmt.Errorf("%s: права %04o разрешают запись группе или всем: %w", path, perm, ErrUnsafePerm)
	}
	if perm&^want.Perm() != 0 {
		return fmt.Errorf("%s: права %04o шире %04o: %w", path, perm, want.Perm(), ErrUnsafePerm)
	}
	return nil
}
