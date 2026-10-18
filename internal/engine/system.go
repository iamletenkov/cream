package engine

import (
	"context"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/systemd"
)

// SystemInitResult is the result of `cream system init` (golden: system-init.json).
type SystemInitResult struct {
	Unit         string `json:"unit"`
	DaemonReload bool   `json:"daemon_reload"`
}

// SystemInit prepares the machine (§5, §11.2): directories with their modes,
// the unit template, configure targets recomputed (systemd.Init); with a
// running systemd a daemon-reload follows — its failure is a
// systemd.reload_failed warning, not an error.
func (e *Engine) SystemInit(ctx context.Context) (SystemInitResult, error) {
	res := SystemInitResult{Unit: "cream-configure@.service"}
	if err := systemd.Init(e.L); err != nil {
		return res, cerr.Wrap(err, cerr.ExitState, "state.write", "system init")
	}
	if _, up := e.Systemctl.Running(ctx); !up {
		return res, nil
	}
	if err := e.Systemctl.DaemonReload(ctx); err != nil {
		e.warn("systemd.reload_failed", "systemctl daemon-reload: %v", err)
		return res, nil
	}
	res.DaemonReload = true
	return res, nil
}
