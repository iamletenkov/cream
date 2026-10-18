//go:build !linux || !(amd64 || arm64)

package source

import "syscall"

// exchange is unsupported here; swapIn falls back to two renames.
func exchange(_, _ string) error { return syscall.ENOSYS }
