//go:build linux && (amd64 || arm64)

package source

import (
	"syscall"
	"unsafe"
)

// atFDCWD is AT_FDCWD: paths relative to the working directory.
const atFDCWD = -100

// renameExchange is RENAME_EXCHANGE of renameat2(2).
const renameExchange = 0x2

// exchange swaps the directory entries a and b atomically (Linux ≥ 3.15).
func exchange(a, b string) error {
	pa, err := syscall.BytePtrFromString(a)
	if err != nil {
		return err
	}
	pb, err := syscall.BytePtrFromString(b)
	if err != nil {
		return err
	}
	cwd := atFDCWD
	_, _, errno := syscall.Syscall6(sysRenameat2,
		uintptr(cwd), uintptr(unsafe.Pointer(pa)), uintptr(cwd), uintptr(unsafe.Pointer(pb)), renameExchange, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
