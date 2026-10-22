package engine

import (
	"context"
	"fmt"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/initsys"
	"github.com/iamletenkov/cream/internal/state"
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
	res := SystemInitResult{Unit: initsys.For(e.L).Unit()}
	if err := initsys.For(e.L).Init(e.L); err != nil {
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
// targets systemd.Init recomputed. lock/global is taken only when some record
// has to change, and without waiting (system init runs from the package
// postinst under set -e, next to another dpkg or hook): busy, the registry is
// left as is with a system.registry_busy warning — the links are already
// right, and the next system init or upgrade writes the target.
func (e *Engine) systemInitTargets(ctx context.Context) error {
	recs, err := e.Store.List(e.Warn)
	if err != nil {
		return err
	}
	var changed []state.Record
	for _, rec := range recs {
		if rec.Configure == nil {
			continue
		}
		target, err := initsys.For(e.L).Target(e.L, rec.Configure.Stage)
		if err != nil {
			return fmt.Errorf("system init: %s: %w", rec.Name, err)
		}
		if target != rec.Configure.Target {
			changed = append(changed, rec)
		}
	}
	if len(changed) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	g, ok, err := e.Locks.TryGlobal()
	if err != nil {
		return err
	}
	if !ok {
		e.warn("system.registry_busy", "lock/global занята: configure.target в реестре не обновлён (%d записей), ссылки юнитов пересчитаны; повторите cream system init", len(changed))
		return nil
	}
	defer g.Release()
	return e.applyTargets(changed)
}

// applyTargets rewrites configure.target of recs under lock/global: each
// record is read again, and one removed or changed meanwhile is taken as it
// is now (a removed one is not written back).
func (e *Engine) applyTargets(recs []state.Record) error {
	for _, old := range recs {
		rec, ok, err := e.Store.Get(old.Name)
		if err != nil {
			return err
		}
		if !ok || rec.Configure == nil {
			continue
		}
		target, err := initsys.For(e.L).Target(e.L, rec.Configure.Stage)
		if err != nil {
			return fmt.Errorf("system init: %s: %w", rec.Name, err)
		}
		if target == rec.Configure.Target {
			continue
		}
		rec.Configure.Target = target
		if err := e.Store.Put(*rec); err != nil {
			return err
		}
	}
	return nil
}
