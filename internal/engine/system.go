package engine

import (
	"context"
	"fmt"

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
		return res, cerr.Wrap(err, cerr.ExitState, cerr.CodeIOWrite, "system init")
	}
	if err := e.systemInitTargets(ctx); err != nil {
		return res, err
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

// systemInitTargets brings configure.target of the registry in line with the
// targets systemd.Init recomputed (the cloud-user stage follows cloud-init):
// only records whose target changed are rewritten, under the global lock.
func (e *Engine) systemInitTargets(ctx context.Context) error {
	g, err := e.Locks.Global(ctx)
	if err != nil {
		return err
	}
	defer g.Release()
	recs, err := e.Store.List(e.Warn)
	if err != nil {
		return err
	}
	for _, rec := range recs {
		if rec.Configure == nil {
			continue
		}
		target, err := systemd.Target(e.L, rec.Configure.Stage)
		if err != nil {
			return fmt.Errorf("system init: %s: %w", rec.Name, err)
		}
		if target == rec.Configure.Target {
			continue
		}
		rec.Configure.Target = target
		if err := e.Store.Put(rec); err != nil {
			return err
		}
	}
	return nil
}
