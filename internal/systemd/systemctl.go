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

// NewSystemctl returns a Systemctl that calls systemctl through run. Whether
// systemd runs is decided by /run/systemd/system under l.Root first: without
// it (build phase, container without systemd) systemctl is never asked.
func NewSystemctl(l paths.Layout, run ExecFunc) Systemctl {
	return &systemctl{l: l, run: run}
}

type systemctl struct {
	l   paths.Layout
	run ExecFunc
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
	if st, err := os.Stat(s.l.SystemdRunning()); err != nil || !st.IsDir() {
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
