package engine

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/lock"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/place"
	"github.com/iamletenkov/cream/internal/resolve"
	"github.com/iamletenkov/cream/internal/runner"
	"github.com/iamletenkov/cream/internal/state"
	"github.com/iamletenkov/cream/internal/systemd"
)

// RemoveOptions are the flags of `cream remove` (§6.5, §11.2).
type RemoveOptions struct {
	// Purge also removes the settings domain /etc/cream/<ext> and the state
	// directory /var/lib/cream/ext/<ext>.
	Purge bool
	// Forget removes the registry record, the tree and cream's artifacts
	// without the remove hook; it also works when the tree cannot be read.
	Forget bool
	// Force removes despite installed dependents.
	Force bool
}

// RemoveResult is the result of `cream remove` (golden: remove-*.json).
type RemoveResult struct {
	Ext     string `json:"ext"`
	Version string `json:"version"`
	// Hook — the remove hook ran (false for --forget and for a library).
	Hook      bool `json:"hook"`
	Purged    bool `json:"purged"`
	Forgotten bool `json:"forgotten"`
	// Dependents are the installed extensions that still need ext and were
	// overridden by --force; never nil.
	Dependents     []string `json:"dependents"`
	RebootRequired bool     `json:"reboot_required"`
}

// History statuses of remove (history.jsonl); a failed hook is
// state.StatusFailed.
const (
	RemoveStatusRemoved   = "removed"
	RemoveStatusForgotten = "forgotten"
)

// Remove is `cream remove` (§6.5): under lock/global and lock/<ext> it
// refuses (67 deps.dependents) while installed extensions need ext, unless
// Force; runs the remove hook (an extension without one is 65
// hook.missing_remove, a library has nothing to undo; Forget skips the
// hook and also drops a broken record, Р20); stops and disables configure; removes the files of the registry (a modified
// one is kept as <dst>.cream-modified with a warning, ruling Р11 for missing
// ones), the tree, with Purge the domain and the state directory, and the
// registry record; writes history. A failed hook leaves everything as it was.
func (e *Engine) Remove(ctx context.Context, ext string, o RemoveOptions) (*RemoveResult, error) {
	if o.Purge && o.Forget {
		return nil, cerr.New(cerr.ExitUsage, "usage.flag", "--purge и --forget несовместимы")
	}
	if err := CheckExtName(ext); err != nil {
		return nil, err
	}
	g, err := e.Locks.Global(ctx)
	if err != nil {
		return nil, err
	}
	defer g.Release()
	h, err := e.Locks.Ext(ctx, ext, lock.Exclusive)
	if err != nil {
		return nil, err
	}
	defer h.Release()

	in, err := e.removeTarget(ext, o.Forget)
	if err != nil {
		return nil, err
	}
	res := &RemoveResult{Ext: ext, Version: in.Record.Version, Purged: o.Purge, Forgotten: o.Forget,
		Dependents: []string{}}

	deps, err := e.removeDependents(in)
	if err != nil {
		return nil, err
	}
	if len(deps) > 0 {
		if !o.Force {
			ce := cerr.New(cerr.ExitGraph, "deps.dependents",
				"%s нужно установленным расширениям: %s (--force — удалить всё равно)", ext, strings.Join(deps, ", "))
			ce.Details = map[string]any{"ext": ext, "dependents": deps}
			return nil, ce
		}
		res.Dependents = deps
		if e.Warn != nil {
			e.Warn.AddDetails("deps.dependents_forced", map[string]any{"ext": ext, "dependents": deps},
				"%s удалено с --force, хотя его ждут: %s", ext, strings.Join(deps, ", "))
		}
	}

	if !o.Forget {
		if err := e.removeHook(WithLock(ctx, h, lock.Exclusive), in, res); err != nil {
			return nil, err
		}
	}
	if err := e.removeArtifacts(ctx, in, o.Purge); err != nil {
		return nil, err
	}
	if err := e.Store.Delete(ext); err != nil {
		return nil, err
	}
	status := RemoveStatusRemoved
	if o.Forget {
		status = RemoveStatusForgotten
	}
	e.removeJournal(in, status)
	return res, nil
}

// removeTarget reads the registry record of ext and its tree. With forget a
// tree that cannot be read is not an error: the manifest stays nil.
func (e *Engine) removeTarget(ext string, forget bool) (Installed, error) {
	rec, ok, err := e.Store.Get(ext)
	if err != nil {
		// Р20: a broken record is removed by --forget alone; its files are unknown.
		if !forget || cerr.CodeOf(err) != "state.corrupt" {
			return Installed{}, err
		}
		if e.Warn != nil {
			e.Warn.AddDetails("state.corrupt", map[string]any{"ext": ext},
				"%s: запись реестра повреждена, снимается без неё (files не убираются): %v", ext, err)
		}
		rec, ok = &state.Record{Name: ext}, true
	}
	if !ok {
		ce := cerr.New(cerr.ExitNotFound, "not_found.extension", "расширение %s не установлено", ext)
		ce.Details = map[string]any{"ext": ext}
		return Installed{}, ce
	}
	m, err := e.loadTree(ext)
	if err != nil {
		if !forget {
			return Installed{}, err
		}
		e.warn("state.tree", "%s: дерево расширения не читается, удаляется без манифеста: %v", ext, err)
		m = nil
	}
	return Installed{Record: *rec, Manifest: m}, nil
}

// removeDependents are the installed extensions that need in (depends, or
// requires with in as the only provider). Records whose tree cannot be read
// are left out (Installed warns about them); a target without a manifest is
// matched by its name only.
func (e *Engine) removeDependents(in Installed) ([]string, error) {
	all, err := e.Installed()
	if err != nil {
		return nil, err
	}
	nodes := make([]resolve.Node, 0, len(all))
	for _, x := range all {
		if x.Record.Name == in.Record.Name {
			continue
		}
		if x.Manifest != nil {
			nodes = append(nodes, resolve.Node{Manifest: x.Manifest, Installed: true})
		}
	}
	target := in.Manifest
	if target == nil {
		target = &manifest.Manifest{Name: in.Record.Name}
	}
	nodes = append(nodes, resolve.Node{Manifest: target, Installed: true})
	return resolve.Dependents(nodes, in.Record.Name), nil
}

// removeHook runs the remove hook of in. A library has no hooks (§4.5); any
// other extension without the hook is 65 hook.missing_remove. A failed hook
// (or one that reports 20) is journaled and returned; nothing is removed.
func (e *Engine) removeHook(ctx context.Context, in Installed, res *RemoveResult) error {
	if !hasHook(in.Manifest, manifest.HookRemove) {
		if in.Manifest.Kind == manifest.KindLibrary {
			return nil
		}
		ce := cerr.New(cerr.ExitData, "hook.missing_remove",
			"%s: в манифесте нет хука remove; удалить без него — cream remove --forget %s", in.Record.Name, in.Record.Name)
		ce.Details = map[string]any{"ext": in.Record.Name}
		return ce
	}
	res.Hook = true
	out, err := e.RunHook(ctx, in, manifest.HookRemove, runner.EnvInput{})
	if err != nil {
		e.removeJournal(in, state.StatusFailed)
		return err
	}
	res.RebootRequired = out == runner.Reboot
	return nil
}

// removeArtifacts removes what cream put on the machine for in: the
// configure instance (stopped with a running systemd) and its drop-in
// (daemon-reload with a running systemd), the reason marker, the files of the registry, the tree with its
// .new/.prev leftovers and, with purge, the domain and the state directory.
func (e *Engine) removeArtifacts(ctx context.Context, in Installed, purge bool) error {
	ext := in.Record.Name
	_, up := e.Systemctl.Running(ctx)
	if up && (in.Record.Configure != nil || (in.Manifest != nil && hasHook(in.Manifest, manifest.HookConfigure))) {
		// A running configure instance would outlive its unit file.
		if err := e.Systemctl.Stop(ctx, systemd.UnitName(ext)); err != nil {
			e.warn("systemd.stop_failed", "%s: systemctl stop %s: %v", ext, systemd.UnitName(ext), err)
		}
	}
	if err := systemd.Disable(e.L, ext); err != nil {
		return cerr.Wrap(err, cerr.ExitState, cerr.CodeIOWrite, "%s: снятие configure", ext)
	}
	if err := place.Unplace(e.L, in.Record.Files, e.Warn); err != nil {
		return cerr.Wrap(err, cerr.ExitState, cerr.CodeIOWrite, "%s: удаление files", ext)
	}
	if up {
		if err := e.Systemctl.DaemonReload(ctx); err != nil {
			e.warn("systemd.reload_failed", "systemctl daemon-reload: %v", err)
		}
	}
	dirs := []string{e.L.ExtTree(ext), e.L.ExtNew(ext), e.L.ExtPrev(ext)}
	if purge {
		dirs = append(dirs, e.L.EtcDir(ext), e.L.StateDir(ext))
	}
	for _, d := range dirs {
		if err := os.RemoveAll(d); err != nil {
			return cerr.Wrap(err, cerr.ExitState, cerr.CodeIOWrite, "%s: удаление %s", ext, d)
		}
	}
	if err := os.Remove(e.L.RunReason(ext)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return cerr.Wrap(err, cerr.ExitState, cerr.CodeIOWrite, "%s: маркер причины", ext)
	}
	return nil
}

// removeJournal appends a remove entry to history.jsonl; a journal that
// cannot be written is a warning, not a failure of the operation.
func (e *Engine) removeJournal(in Installed, status string) {
	if err := e.Store.Append(state.HistoryEntry{Op: "remove", Ext: in.Record.Name, From: in.Record.Version,
		Status: status, Phase: e.Phase()}); err != nil {
		e.warn("state.history", "%s: журнал операций: %v", in.Record.Name, err)
	}
}
