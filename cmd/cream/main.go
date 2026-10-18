// Command cream is the extension runner (docs/specs/2026-10-18-cream-core.md).
// main only sets the process umask and hands over to internal/cli.
package main

import (
	"io"
	"os"
	"syscall"
	"time"

	"github.com/iamletenkov/cream/internal/cli"
)

func main() {
	// umask 022 once per process; hooks and actions inherit it (spec §6.3, ruling Р7).
	syscall.Umask(0o022)
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv))
}

// run builds the process App and returns the exit code of cli.Run.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer, env func(string) string) int {
	return cli.Run(args, cli.App{
		Stdin: stdin, Stdout: stdout, Stderr: stderr,
		Env: env, Euid: os.Geteuid, Now: time.Now,
	})
}
