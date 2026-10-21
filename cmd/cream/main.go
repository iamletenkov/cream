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
	ctx, stop := signalContext(context.Background())
	code := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv)
	stop()
	os.Exit(code)
}

// signalContext is canceled by the first SIGINT, SIGTERM or SIGHUP: lock waits
// stop, the process group of a running hook or action is killed, locks are
// released. Once canceled the signals get their default action back, so a
// second Ctrl-C kills cream at once if that cleanup hangs.
func signalContext(parent context.Context) (context.Context, context.CancelFunc) {
	sig, stopSig := signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	ctx, cancel := context.WithCancelCause(parent)
	go func() {
		<-sig.Done()
		stopSig() // the handler is gone before ctx is seen canceled
		cancel(context.Cause(sig))
	}()
	return ctx, func() { stopSig(); cancel(context.Canceled) }
}

// run builds the process App and returns the exit code of cli.Run.
func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, env func(string) string) int {
	return cli.Run(args, cli.App{
		Stdin: stdin, Stdout: stdout, Stderr: stderr,
		Env: env, Euid: os.Geteuid, Now: time.Now, Context: ctx,
		Interactive: interactive,
	})
}
