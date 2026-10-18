// Command cream is the extension runner (docs/specs/2026-10-18-cream-core.md).
// main only sets the process umask and the signal context and hands over to
// internal/cli.
package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/iamletenkov/cream/internal/cli"
)

func main() {
	// umask 022 once per process; hooks and actions inherit it (spec §6.3, ruling Р7).
	syscall.Umask(0o022)
	// SIGINT, SIGTERM, SIGHUP cancel the invocation: lock waits stop, the
	// process group of a running hook or action is killed, locks are released.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	code := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv)
	stop()
	os.Exit(code)
}

// run builds the process App and returns the exit code of cli.Run.
func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, env func(string) string) int {
	return cli.Run(args, cli.App{
		Stdin: stdin, Stdout: stdout, Stderr: stderr,
		Env: env, Euid: os.Geteuid, Now: time.Now, Context: ctx,
	})
}
