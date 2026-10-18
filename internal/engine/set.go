package engine

import (
	"context"
	"os"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/config"
	"github.com/iamletenkov/cream/internal/fsx"
	"github.com/iamletenkov/cream/internal/lock"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/runner"
	"github.com/iamletenkov/cream/internal/systemd"
)

// SetOptions are the flags of `cream set` (§8.3, §8.4).
type SetOptions struct {
	// NoApply writes without applying (--no-apply).
	NoApply bool
	// Wait makes the configure restart synchronous (--wait); during boot it
	// falls back to --no-block with a warning.
	Wait bool
	// AllowReadonly lets readonly keys be written (--allow-readonly).
	AllowReadonly bool
}

// SetResult is the result of `cream set` (golden: set*.json). Apply is one
// of the Apply* constants.
type SetResult struct {
	Ext            string   `json:"ext"`
	Changed        bool     `json:"changed"`
	ChangedKeys    []string `json:"changed_keys"`
	Apply          string   `json:"apply"`
	RebootRequired bool     `json:"reboot_required"`
}

// Set writes changes to the domain of ext under lock/<ext> (exclusive) and
// applies what changed (§8.4): hooks/apply with CREAM_CHANGED_KEYS, holding
// the lock (passed to the hook as CREAM_LOCK_FD); otherwise, with a
// configure hook and a running systemd, a restart of cream-configure@<ext>
// with the reason marker "apply" — after releasing the lock, which the
// unit's configure takes itself; otherwise nothing. A failed apply is 79
// after the write, and the result still describes the write.
func (e *Engine) Set(ctx context.Context, ext string, ch []config.Change, o SetOptions) (SetResult, error) {
	res := SetResult{Ext: ext, ChangedKeys: []string{}, Apply: ApplyNone}
	in, err := e.Find(ext)
	if err != nil {
		return res, err
	}
	h, err := e.Locks.Ext(ctx, ext, lock.Exclusive)
	if err != nil {
		return res, err
	}
	defer h.Release()

	changed, err := e.Domain(in).Set(ch, config.SetOpts{AllowReadonly: o.AllowReadonly})
	if err != nil {
		return res, err
	}
	if len(changed) == 0 {
		return res, nil
	}
	res.Changed, res.ChangedKeys = true, changed
	switch {
	case o.NoApply:
		res.Apply = ApplySkipped
		return res, nil
	case hasHook(in.Manifest, manifest.HookApply):
		res.Apply = ApplyHook
		out, err := e.RunHook(WithLock(ctx, h, lock.Exclusive), in, manifest.HookApply,
			runner.EnvInput{ChangedKeys: changed})
		res.RebootRequired = out == runner.Reboot
		return res, err
	case hasHook(in.Manifest, manifest.HookConfigure):
		h.Release()
		st, up := e.Systemctl.Running(ctx)
		if !up {
			return res, nil
		}
		res.Apply, err = e.restartConfigure(ctx, ext, ReasonApply, o.Wait, st)
		return res, err
	}
	return res, nil
}

func hasHook(m *manifest.Manifest, h manifest.HookName) bool {
	_, ok := m.Hooks[h]
	return ok
}

// restartConfigure restarts cream-configure@<ext> on a running systemd in
// state st (Systemctl.Running), after writing the reason marker /run/cream/<ext>.reason: synchronously with
// wait, except during boot (starting, initializing), where the restart is
// queued with --no-block and a configure.no_block warning, so that systemd
// jobs do not wait on each other. The caller must not hold lock/<ext>.
func (e *Engine) restartConfigure(ctx context.Context, ext, reason string, wait bool, st string) (string, error) {
	noBlock := !wait
	if wait && (st == "starting" || st == "initializing") {
		noBlock = true
		e.warn("configure.no_block",
			"%s: systemd в состоянии %s — configure поставлен в очередь (--no-block) вместо ожидания", ext, st)
	}
	if err := e.writeReason(ext, reason); err != nil {
		return ApplyNone, err
	}
	unit := systemd.UnitName(ext)
	if err := e.Systemctl.Restart(ctx, unit, noBlock); err != nil {
		ce := cerr.Wrap(err, cerr.ExitHook, "hook.failed", "%s: перезапуск %s", ext, unit)
		ce.Details = map[string]any{"hook": string(manifest.HookConfigure), "unit": unit}
		return ApplyNone, ce
	}
	if noBlock {
		return ApplyQueued, nil
	}
	return ApplyRestart, nil
}

// writeReason writes the reason marker the unit's configure --from-unit reads.
func (e *Engine) writeReason(ext, reason string) error {
	if err := os.MkdirAll(e.L.RunDir(), 0o755); err != nil {
		return cerr.Wrap(err, cerr.ExitState, "state.write", "%s", e.L.RunDir())
	}
	if err := fsx.WriteAtomic(e.L.RunReason(ext), []byte(reason+"\n"), 0o644); err != nil {
		return cerr.Wrap(err, cerr.ExitState, "state.write", "%s", e.L.RunReason(ext))
	}
	return nil
}
