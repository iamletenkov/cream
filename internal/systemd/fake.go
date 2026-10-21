package systemd

import (
	"context"
	"sync"
)

// Fake is a Systemctl for tests of the engine: it answers Running with
// State/OK, fails with ReloadErr/RestartErr/StopErr and records every call as
// "is-system-running", "daemon-reload", "restart [--no-block] <unit>",
// "stop <unit>".
// Set the fields before use; Calls is safe for concurrent use.
type Fake struct {
	State      string
	OK         bool
	ReloadErr  error
	RestartErr error
	StopErr    error
	// OnRestart, when set, runs on Restart (after it is recorded) and its
	// error replaces RestartErr: a test plays the unit's ExecStart there.
	OnRestart func(unit string, noBlock bool) error

	mu    sync.Mutex
	calls []string
}

var _ Systemctl = (*Fake)(nil)

func (f *Fake) record(c string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
}

// Calls returns a copy of the recorded calls.
func (f *Fake) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// Running returns State, OK.
func (f *Fake) Running(context.Context) (string, bool) {
	f.record("is-system-running")
	return f.State, f.OK
}

// DaemonReload returns ReloadErr.
func (f *Fake) DaemonReload(context.Context) error {
	f.record("daemon-reload")
	return f.ReloadErr
}

// Restart returns RestartErr, or the error of OnRestart when it is set.
func (f *Fake) Restart(_ context.Context, unit string, noBlock bool) error {
	if noBlock {
		f.record("restart --no-block " + unit)
	} else {
		f.record("restart " + unit)
	}
	if f.OnRestart != nil {
		return f.OnRestart(unit, noBlock)
	}
	return f.RestartErr
}

// Stop returns StopErr.
func (f *Fake) Stop(_ context.Context, unit string) error {
	f.record("stop " + unit)
	return f.StopErr
}
