package main

import (
	"os"
	"syscall"
	"unsafe"
)

// isTerminal reports whether f is a terminal: ioctl TCGETS succeeds (isatty(3)).
// /dev/null and pipes are not terminals, so cron, systemd units and scripts
// never get the y/N question of cream upgrade.
func isTerminal(f *os.File) bool {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&t)))
	return errno == 0
}

// interactive: both stdin (the answer) and stderr (the question) are a terminal.
func interactive() bool {
	return isTerminal(os.Stdin) && isTerminal(os.Stderr)
}
