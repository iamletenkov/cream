package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/engine"
	"github.com/iamletenkov/cream/internal/lock"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/runner"
	"github.com/iamletenkov/cream/internal/state"
	"github.com/iamletenkov/cream/internal/systemd"
	"github.com/iamletenkov/cream/internal/version"
)

// newEngine builds the production engine of one invocation from the global
// flags and the process (App): the layout, --lock-timeout, CREAM_LOCK_FD,
// systemctl through os/exec, hooks through runner.Exec. With --json the
// streams are already redirected by the dispatcher (stdout — the envelope).
func newEngine(ctx *Context) *engine.Engine {
	l := ctx.Layout
	now := ctx.App.Now
	return &engine.Engine{
		L:         l,
		Store:     state.NewStore(l, now),
		Locks:     lock.NewLocker(l, ctx.Globals.LockTimeout, ctx.App.Env),
		Run:       runner.Exec{},
		Systemctl: systemd.NewSystemctl(l, execCommand),
		Now:       now,
		Cream:     manifest.ParseCreamBuild(version.Version),
		Warn:      ctx.Warn,
		LookPath:  exec.LookPath,
		Env:       ctx.App.Env,
		Stdin:     ctx.App.Stdin,
		Stdout:    ctx.App.Stdout,
		Stderr:    ctx.App.Stderr,
	}
}

// phaseEngine is newEngine with the phase decided (§10.1): --phase (phase,
// "" — not given), else CREAM_PHASE of a parent cream, else PID 1. The
// decision goes to the --json envelope and, unless -q, to stderr as one line
// (the build log of an image names the phase and why, §10.1).
func phaseEngine(ctx *Context, phase string) (*engine.Engine, error) {
	e := newEngine(ctx)
	p, err := e.ResolvePhase(phase)
	if err != nil {
		return nil, err
	}
	if ctx.Phase != nil {
		*ctx.Phase = p
	}
	if !ctx.Globals.Quiet {
		_, _ = fmt.Fprintf(ctx.Stderr(), "cream: фаза %s (%s)\n", p.Phase, p.Reason)
	}
	return e, nil
}

// execCommand runs a command without a shell (systemd.ExecFunc): stdout and
// the exit code; err only when it could not run at all.
func execCommand(ctx context.Context, name string, args ...string) ([]byte, int, error) {
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = &out
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return out.Bytes(), 0, nil
	case errors.As(err, &exitErr):
		return out.Bytes(), exitErr.ExitCode(), nil
	default:
		return nil, -1, err
	}
}

// requireRoot refuses (77 perm.root) a command that needs root, on the real
// root only (ruling Р9).
func requireRoot(ctx *Context, what string) error {
	if ctx.Layout.NeedsRoot() && ctx.App.Euid() != 0 {
		return cerr.New(cerr.ExitPerm, "perm.root", "%s: нужен root", what)
	}
	return nil
}
