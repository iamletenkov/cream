package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"syscall"
	"time"

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
	// directly with the reason from the marker, "boot" without one; at boot
	// a matching configure mark skips the hook (§10.7).
	FromUnit bool
	// Force removes the configure mark before the run (--force): the hook
	// runs now, and a restart that never finishes is repeated at boot.
	Force bool
	// Again is a hook asking for one more run of its unit (--again): the
	// reason marker "followup" and a restart with --no-block, past the mark.
	Again bool
}

// ReasonFollowup is a run the hook asked for itself (cream configure
// --again): like install, upgrade and apply it bypasses the configure mark.
const ReasonFollowup = "followup"

// ConfigureUpToDate is ConfigureResult.Mode when the boot run was skipped:
// the configure mark matches the registry (§10.7).
const ConfigureUpToDate = "up-to-date"

// ConfigureSkipped is ConfigureResult.Status when the hook ran here and
// exited with code 20 (§10.8): not an error, the configure mark is neither
// written nor removed, and the hook runs again at the next boot.
const ConfigureSkipped = "skipped"

// ConfigureResult is the result of `cream configure` (golden: configure*.json).
// Mode is ApplyHook (the hook ran here), ApplyRestart, ApplyQueued or
// ApplyNone (the extension has no configure hook). Status is
// ConfigureSkipped after hook code 20 and empty otherwise.
type ConfigureResult struct {
	Ext            string `json:"ext"`
	Reason         string `json:"reason"`
	Mode           string `json:"mode"`
	Status         string `json:"status,omitempty"`
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
//     /run/cream/<ext>.reason (removed), "boot" without a valid marker; at
//     boot a configure mark that matches the registry skips the hook,
//     unless the manifest says configure.every_boot;
//   - Again: the reason marker "followup" and a queued restart of the unit
//     (nothing, with a warning, without systemd);
//   - systemd running: restart of cream-configure@<ext> with the reason
//     marker, synchronous with Wait (queued during boot, with a warning);
//   - no systemd (container, live machine without systemd): the hook
//     directly under lock/<ext>.
//
// The caller must not hold lock/<ext> (it may hold lock/global): both the
// direct run and the unit take it. A successful run in the live phase writes
// the configure mark; a failed hook is 79, removes the mark and appends a
// configure entry with status failed to history.jsonl; hook code 20 is not
// an error but status skipped (§10.8): the mark is left as it is, a
// configure entry with status skipped goes to history.jsonl, and at boot the
// hook runs again unless an older mark still matches. The registry is not changed (§6.4).
func (e *Engine) ConfigureReport(ctx context.Context, ext, reason string, o ConfigureOptions) (ConfigureResult, error) {
	res := ConfigureResult{Ext: ext, Reason: reason, Mode: ApplyNone}
	if err := checkConfigureFlags(o); err != nil {
		return res, err
	}
	if o.Again {
		reason = ReasonFollowup
		res.Reason = reason
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
	if o.Force {
		if err := e.Store.DeleteConfigured(ext); err != nil {
			return res, err
		}
	}
	if o.Again {
		st, up := e.Systemctl.Running(ctx)
		if !up {
			e.warn("configure.no_systemd", "%s: --again без systemd — повтор configure не поставлен", ext)
			return res, nil
		}
		res.Mode, err = e.restartConfigure(ctx, ext, reason, false, st)
		return res, err
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
	// The record is re-read under the lock: a cream set the unit waited for
	// may have raised secrets_rev, an upgrade replaced the version.
	if in, err = e.Find(ext); err != nil {
		return res, err
	}
	if !hasHook(in.Manifest, manifest.HookConfigure) {
		res.Mode = ApplyNone
		return res, nil
	}
	live := e.Phase() == PhaseLive
	if live && res.Reason == ReasonBoot && !in.Manifest.Hooks[manifest.HookConfigure].EveryBoot && e.markMatches(in) {
		res.Mode = ConfigureUpToDate
		_, _ = fmt.Fprintf(e.stderr(), "configure %s: пропуск, отметка совпала\n", ext)
		return res, nil
	}
	res.Mode = ApplyHook
	out, err := e.RunHook(WithLock(ctx, h, lock.Exclusive), in, manifest.HookConfigure,
		runner.EnvInput{Reason: res.Reason})
	res.RebootRequired = out == runner.Reboot
	if live {
		e.updateMark(ext, out, err)
	}
	switch {
	case out == runner.NotApplicable:
		// Code 20 of configure is "not yet" (no device, no driver), not a
		// failure: the unit succeeds, no mark is written, so the next boot
		// tries again unless an older mark still matches (§10.8).
		res.Status, err = ConfigureSkipped, nil
		_, _ = fmt.Fprintf(e.stderr(), "configure %s: пропущено (код 20), отметка не записана\n", ext)
		e.configureHistory(ext, in.Record.Version, state.StatusSkipped)
	case err != nil && out == runner.Failed:
		e.configureHistory(ext, in.Record.Version, state.StatusFailed)
	}
	return res, err
}

// configureHistory appends a configure entry of ext at version with status
// to history.jsonl; a write error is a state.history warning.
func (e *Engine) configureHistory(ext, version, status string) {
	if err := e.Store.Append(state.HistoryEntry{Op: "configure", Ext: ext, From: version,
		To: version, Status: status, Phase: e.Phase()}); err != nil {
		e.warn("state.history", "%s: журнал операций: %v", ext, err)
	}
}

// checkConfigureFlags refuses combinations of the configure flags: the unit's
// own call takes none of them, --again none but itself.
func checkConfigureFlags(o ConfigureOptions) error {
	switch {
	case o.FromUnit && (o.Wait || o.Force || o.Again):
		return cerr.New(cerr.ExitUsage, "usage.flag", "--from-unit несовместим с --wait, --force и --again")
	case o.Again && (o.Wait || o.Force):
		return cerr.New(cerr.ExitUsage, "usage.flag", "--again несовместим с --wait и --force")
	}
	return nil
}

// ConfigureForceAll is `cream configure --force --all`: ConfigureReport with
// reason apply for every installed extension with a configure hook, in
// registry order. A failure does not stop the others; the first error is
// returned with the results of every extension tried.
func (e *Engine) ConfigureForceAll(ctx context.Context, o ConfigureOptions) ([]ConfigureResult, error) {
	all, err := e.Installed()
	if err != nil {
		return nil, err
	}
	out := []ConfigureResult{}
	var first error
	for _, in := range all {
		if in.Manifest == nil || !hasHook(in.Manifest, manifest.HookConfigure) {
			continue
		}
		res, err := e.ConfigureReport(ctx, in.Record.Name, ReasonApply, o)
		out = append(out, res)
		if err != nil && first == nil {
			first = err
		}
	}
	return out, first
}

func validReason(r string) bool {
	switch r {
	case ReasonBoot, ReasonInstall, ReasonUpgrade, ReasonApply, ReasonFollowup:
		return true
	}
	return false
}

// maxReason bounds what is read from a reason marker.
const maxReason = 64

// takeReason reads and removes the reason marker of ext; a missing marker
// or anything but install, upgrade, apply, followup is "boot".
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
		return "", cerr.Wrap(err, cerr.ExitState, cerr.CodeIOWrite, "маркер причины %s", p)
	}
	return reason, nil
}

// ConfiguredInfo is `configured` of cream show and cream list (§10.7): when
// configure last succeeded (At, nil — never or after a failure), whether
// that mark still matches the registry and the settings, and whether the
// manifest runs configure on every boot anyway.
type ConfiguredInfo struct {
	At        *time.Time `json:"at"`
	Matches   bool       `json:"matches"`
	EveryBoot bool       `json:"every_boot"`
}

// ConfiguredStatus is ConfiguredInfo of ext; nil for an extension without a
// configure hook.
func (e *Engine) ConfiguredStatus(ext string) (*ConfiguredInfo, error) {
	in, err := e.Find(ext)
	if err != nil {
		return nil, err
	}
	return e.configuredInfo(in), nil
}

// configuredInfo is ConfiguredInfo of in, nil without a manifest or a
// configure hook. A mark or settings that cannot be read do not match.
func (e *Engine) configuredInfo(in Installed) *ConfiguredInfo {
	if in.Manifest == nil || !hasHook(in.Manifest, manifest.HookConfigure) {
		return nil
	}
	info := &ConfiguredInfo{EveryBoot: in.Manifest.Hooks[manifest.HookConfigure].EveryBoot}
	mark, ok, err := e.Store.GetConfigured(in.Record.Name)
	if err != nil || !ok {
		return info
	}
	at := mark.At.UTC()
	info.At = &at
	if cur, err := e.currentMark(in); err == nil {
		info.Matches = mark.Same(cur)
	}
	return info
}

// currentMark is the configure mark in would get now: version and tree
// from the registry, the digest of the effective non-secret settings (a
// secret enters only through secrets_rev), the sha256 of /etc/machine-id.
func (e *Engine) currentMark(in Installed) (state.Configured, error) {
	values, err := e.Domain(in).Values()
	if err != nil {
		return state.Configured{}, err
	}
	return state.Configured{
		Version:      in.Record.Version,
		ExtDigest:    in.Record.ExtDigest,
		ParamsDigest: state.ParamsDigest(values, nil),
		SecretsRev:   in.Record.SecretsRev,
		MachineID:    e.Store.MachineID(),
	}, nil
}

// markMatches reports whether the configure mark of in matches its current
// state; a missing mark or unreadable settings are a mismatch.
func (e *Engine) markMatches(in Installed) bool {
	mark, ok, err := e.Store.GetConfigured(in.Record.Name)
	if err != nil || !ok {
		return false
	}
	cur, err := e.currentMark(in)
	return err == nil && mark.Same(cur)
}

// updateMark follows a configure run of ext with outcome out: success
// writes the mark from the registry and settings as they are after the hook
// (a nested cream set of the hook is included), failure removes it, code 20
// leaves it. Neither fails the operation: an error is a configure.mark
// warning, and at worst the hook runs once more at boot.
func (e *Engine) updateMark(ext string, out runner.Outcome, runErr error) {
	switch {
	case out == runner.Failed:
		if err := e.Store.DeleteConfigured(ext); err != nil {
			e.warn("configure.mark", "%s: отметка configure: %v", ext, err)
		}
	case (out == runner.OK || out == runner.Reboot) && runErr == nil:
		in, err := e.Find(ext)
		var cur state.Configured
		if err == nil {
			cur, err = e.currentMark(in)
		}
		if err == nil {
			err = e.Store.PutConfigured(ext, cur)
		}
		if err != nil {
			e.warn("configure.mark", "%s: отметка configure: %v", ext, err)
		}
	}
}
