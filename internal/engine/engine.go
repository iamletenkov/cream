// Package engine is cream's executor: it ties the registry, the settings
// domain, locks, hooks and systemd together into the operations of spec §6
// and §8. Every dependency is a field of Engine, so tests substitute them;
// the CLI builds the production Engine (internal/cli/wire.go).
//
// Task 12 owns the core (this file, hook.go, installed.go, set.go,
// configure.go, system.go); install, remove, actions and the build phase live
// in their own files (Tasks 13–16) and use only what is exported here.
package engine

import (
	"context"
	"io"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/lock"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/paths"
	"github.com/iamletenkov/cream/internal/runner"
	"github.com/iamletenkov/cream/internal/state"
	"github.com/iamletenkov/cream/internal/systemd"
)

// Engine is one cream invocation's view of the machine.
type Engine struct {
	// L is the machine layout (--root and friends).
	L paths.Layout
	// Store is the registry and the history journal (§10).
	Store *state.Store
	// Locks takes lock/global and lock/<ext> (§6.3).
	Locks *lock.Locker
	// Run starts hooks and actions.
	Run runner.Runner
	// Systemctl talks to a running systemd; it reports "not running" in build.
	Systemctl systemd.Systemctl
	// Now is the clock.
	Now func() time.Time
	// Cream is the running cream build, for `cream:` checks of manifests.
	Cream manifest.CreamBuild
	// Warn collects warnings for the --json envelope.
	Warn *cerr.Warnings
	// LookPath finds a command for platform.commands (exec.LookPath).
	LookPath func(string) (string, error)
	// Env is cream's own environment (CREAM_PHASE, CREAM_LOCK_FD).
	Env func(string) string
	// Stdin, Stdout, Stderr are cream's streams. Hook output goes to Stderr
	// (§6.3); Stdout carries only what a command prints as its result.
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	// phase is the phase decided by ResolvePhase ("" — not decided).
	phase string
}

// Installed is one registry record with the manifest of its installed tree.
// Manifest is nil in Installed() when the tree cannot be read (a warning
// says why); Find never returns a nil Manifest.
type Installed struct {
	Record   state.Record
	Manifest *manifest.Manifest
}

// Phases of cream (CREAM_PHASE, §6.3).
const (
	PhaseLive  = "live"
	PhaseBuild = "build"
)

// Reasons of configure (CREAM_REASON, §6.3).
const (
	ReasonBoot    = "boot"
	ReasonInstall = "install"
	ReasonUpgrade = "upgrade"
	ReasonApply   = "apply"
)

// Phase is the phase of this invocation. install, upgrade and remove decide
// it first with ResolvePhase (--phase → CREAM_PHASE → PID 1, §10.1); other
// commands keep CREAM_PHASE of a parent cream (a nested call from a build
// hook), otherwise live.
func (e *Engine) Phase() string {
	if e.phase != "" {
		return e.phase
	}
	if e.Env != nil && e.Env("CREAM_PHASE") == PhaseBuild {
		return PhaseBuild
	}
	return PhaseLive
}

// warn adds a warning when the engine collects them.
func (e *Engine) warn(code, format string, a ...any) {
	if e.Warn != nil {
		e.Warn.Add(code, format, a...)
	}
}

// stderr is Stderr or a discarding writer.
func (e *Engine) stderr() io.Writer {
	if e.Stderr == nil {
		return io.Discard
	}
	return e.Stderr
}

// heldLockKey carries the domain lock a caller holds into RunHook.
type heldLockKey struct{}

type heldLock struct {
	h *lock.Handle
	m lock.Mode
}

// WithLock returns ctx that tells RunHook which lock/<ext> the caller holds
// and in which mode: the hook gets it as CREAM_LOCK_FD/CREAM_LOCK_MODE, so a
// nested cream (cream set from a hook) inherits it instead of waiting (§6.3).
func WithLock(ctx context.Context, h *lock.Handle, m lock.Mode) context.Context {
	return context.WithValue(ctx, heldLockKey{}, heldLock{h: h, m: m})
}

func lockFrom(ctx context.Context) (heldLock, bool) {
	hl, ok := ctx.Value(heldLockKey{}).(heldLock)
	return hl, ok && hl.h != nil && hl.h.File() != nil
}
