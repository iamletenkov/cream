package engine

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"syscall"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/lock"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/runner"
	"github.com/iamletenkov/cream/internal/state"
)

// ConfigureOptions are the flags of `cream configure` (§6.4, §11.2).
type ConfigureOptions struct {
	// Wait makes the restart of cream-configure@<ext> synchronous (--wait);
	// during boot it falls back to --no-block with a warning.
	Wait bool
	// FromUnit is the call of the unit itself (--from-unit): the hook runs
	// directly with the reason from the marker, "boot" without one.
	FromUnit bool
}

// ConfigureResult is the result of `cream configure` (golden: configure*.json).
// Mode is ApplyHook (the hook ran here), ApplyRestart, ApplyQueued or
// ApplyNone (the extension has no configure hook).
type ConfigureResult struct {
	Ext            string `json:"ext"`
	Reason         string `json:"reason"`
	Mode           string `json:"mode"`
	RebootRequired bool   `json:"reboot_required"`
}

// Configure runs configure of ext for reason (install, upgrade, apply; with
// FromUnit the reason comes from the marker). It is ConfigureReport without
// the report.
func (e *Engine) Configure(ctx context.Context, ext, reason string, o ConfigureOptions) error {
	_, err := e.ConfigureReport(ctx, ext, reason, o)
	return err
}

// ConfigureReport runs configure of ext:
//   - FromUnit: the hook directly under lock/<ext>, CREAM_REASON from
//     /run/cream/<ext>.reason (removed), "boot" without a valid marker;
//   - systemd running: restart of cream-configure@<ext> with the reason
//     marker, synchronous with Wait (queued during boot, with a warning);
//   - no systemd (container, live machine without systemd): the hook
//     directly under lock/<ext>.
//
// The caller must not hold lock/<ext> (it may hold lock/global): both the
// direct run and the unit take it. A failed hook is 79 and a configure entry
// with status failed in history.jsonl; the registry is not changed (§6.4).
func (e *Engine) ConfigureReport(ctx context.Context, ext, reason string, o ConfigureOptions) (ConfigureResult, error) {
	res := ConfigureResult{Ext: ext, Reason: reason, Mode: ApplyNone}
	if o.FromUnit && o.Wait {
		return res, cerr.New(cerr.ExitUsage, "usage.flag", "--from-unit и --wait несовместимы")
	}
	if !o.FromUnit && !validReason(reason) {
		return res, cerr.New(cerr.ExitInternal, cerr.CodeInternal, "configure %s: недопустимая причина %q", ext, reason)
	}
	in, err := e.Find(ext)
	if err != nil {
		return res, err
	}
	if !hasHook(in.Manifest, manifest.HookConfigure) {
		return res, nil
	}
	if !o.FromUnit {
		if st, up := e.Systemctl.Running(ctx); up {
			res.Mode, err = e.restartConfigure(ctx, ext, reason, o.Wait, st)
			return res, err
		}
	}

	h, err := e.Locks.Ext(ctx, ext, lock.Exclusive)
	if err != nil {
		return res, err
	}
	defer h.Release()
	if o.FromUnit {
		if res.Reason, err = e.takeReason(ext); err != nil {
			return res, err
		}
	}
	res.Mode = ApplyHook
	out, err := e.RunHook(WithLock(ctx, h, lock.Exclusive), in, manifest.HookConfigure,
		runner.EnvInput{Reason: res.Reason})
	res.RebootRequired = out == runner.Reboot
	if err != nil && out == runner.Failed {
		if herr := e.Store.Append(state.HistoryEntry{Op: "configure", Ext: ext, From: in.Record.Version,
			To: in.Record.Version, Status: state.StatusFailed, Phase: e.Phase()}); herr != nil {
			e.warn("state.history", "%s: журнал операций: %v", ext, herr)
		}
	}
	return res, err
}

func validReason(r string) bool {
	switch r {
	case ReasonBoot, ReasonInstall, ReasonUpgrade, ReasonApply:
		return true
	}
	return false
}

// maxReason bounds what is read from a reason marker.
const maxReason = 64

// takeReason reads and removes the reason marker of ext; a missing marker
// or anything but install, upgrade, apply is "boot".
func (e *Engine) takeReason(ext string) (string, error) {
	p := e.L.RunReason(ext)
	f, err := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return ReasonBoot, nil
	}
	reason := ReasonBoot
	if err == nil {
		data, rerr := io.ReadAll(io.LimitReader(f, maxReason))
		_ = f.Close()
		if r := strings.TrimSpace(string(data)); rerr == nil && validReason(r) {
			reason = r
		}
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", cerr.Wrap(err, cerr.ExitState, "state.write", "маркер причины %s", p)
	}
	return reason, nil
}
