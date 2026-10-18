package systemd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/iamletenkov/cream/internal/paths"
)

// Systemctl is the part of a running systemd cream talks to.
type Systemctl interface {
	// Running returns the `is-system-running` state; ok is true when systemd
	// is PID 1 and up or coming up: running, degraded, starting, initializing.
	Running(ctx context.Context) (state string, ok bool)
	// DaemonReload runs `systemctl daemon-reload`.
	DaemonReload(ctx context.Context) error
	// Restart runs `systemctl [--no-block] restart <unit>`.
	Restart(ctx context.Context, unit string, noBlock bool) error
}

// ExecFunc runs a command without a shell and returns its stdout and exit
// code; err is set only when the command could not run at all. The adapter
// to os/exec lives with the wiring (cli), tests pass a fake.
type ExecFunc func(ctx context.Context, name string, args ...string) (stdout []byte, code int, err error)

// NewSystemctl returns a Systemctl that calls systemctl through run. Only on
// the real root (ruling Р9) and only with /run/systemd/system present is
// systemctl asked whether systemd runs; under another --root (the build
// phase, fixtures) or without that directory (container without systemd)
// Running is offline.
func NewSystemctl(l paths.Layout, run ExecFunc) Systemctl {
	return &systemctl{l: l, run: run, isDir: isDir}
}

type systemctl struct {
	l     paths.Layout
	run   ExecFunc
	isDir func(path string) bool // tests fake /run/systemd/system on the real root
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// StateOffline is reported when systemd is not PID 1.
const StateOffline = "offline"

// parseState classifies a state `is-system-running` prints (systemctl(1)):
// up is true for a systemd that runs or is coming up.
func parseState(s string) (up, known bool) {
	switch s {
	case "initializing", "starting", "running", "degraded":
		return true, true
	case "maintenance", "stopping", "offline", "unknown":
		return false, true
	}
	return false, false
}

func (s *systemctl) Running(ctx context.Context) (string, bool) {
	if !s.l.NeedsRoot() || !s.isDir(s.l.SystemdRunning()) {
		return StateOffline, false
	}
	// The command exits non-zero for everything but "running": only stdout counts.
	out, _, err := s.run(ctx, "systemctl", "is-system-running")
	if err != nil {
		return "", false
	}
	state := strings.TrimSpace(string(out))
	up, known := parseState(state)
	if !known {
		return "", false
	}
	return state, up
}

func (s *systemctl) DaemonReload(ctx context.Context) error {
	return s.call(ctx, "daemon-reload")
}

func (s *systemctl) Restart(ctx context.Context, unit string, noBlock bool) error {
	if noBlock {
		return s.call(ctx, "--no-block", "restart", unit)
	}
	return s.call(ctx, "restart", unit)
}

func (s *systemctl) call(ctx context.Context, args ...string) error {
	_, code, err := s.run(ctx, "systemctl", args...)
	if err != nil {
		return fmt.Errorf("systemctl %s: %w", strings.Join(args, " "), err)
	}
	if code != 0 {
		return fmt.Errorf("systemctl %s: код выхода %d", strings.Join(args, " "), code)
	}
	return nil
}
