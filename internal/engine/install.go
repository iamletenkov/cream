package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iamletenkov/cream/internal/catalog"
	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/config"
	"github.com/iamletenkov/cream/internal/keytype"
	"github.com/iamletenkov/cream/internal/lock"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/place"
	"github.com/iamletenkov/cream/internal/resolve"
	"github.com/iamletenkov/cream/internal/runner"
	"github.com/iamletenkov/cream/internal/semver"
	"github.com/iamletenkov/cream/internal/source"
	"github.com/iamletenkov/cream/internal/state"
	"github.com/iamletenkov/cream/internal/systemd"
)

// InstallRequest is `cream install` from the sources, live or build (§6.5,
// §11.2; build phase — spec 2026-10-21 §10.2).
type InstallRequest struct {
	// Refs are [source/]name[@version]; several only without parameters.
	Refs []string
	// Params are the install parameters KEY=VAL (argv, a single KEY=- from
	// Stdin, --params-file); parsed against the schema of the found manifest.
	Params config.AssignInput
	// Source is --source: the source of refs that name none.
	Source string
	// Reinstall reruns the install hook even when nothing changed.
	Reinstall bool
	// AcceptRisk allows targets.live: confirm.
	AcceptRisk bool
	// AllowSkip turns hook code 20 into status skipped without an error.
	AllowSkip bool
	// WithDeps adds the missing depends of the requested extensions from the
	// source each was found in (§9.4).
	WithDeps bool
}

// InstallResult is the result of install/upgrade for one extension: the
// registry record (§10) plus changed (§11.4; golden install-*.json).
type InstallResult struct {
	state.Record
	Changed bool `json:"changed"`
}

// installOpts are the flags installEntries needs.
type installOpts struct {
	reinstall, acceptRisk, allowSkip bool
}

// Operations of the registry history.
const (
	installOpInstall = "install"
	installOpUpgrade = "upgrade"
)

// Install installs exactly one reference (InstallAll for several).
func (e *Engine) Install(ctx context.Context, r InstallRequest) (*InstallResult, error) {
	if len(r.Refs) != 1 {
		return nil, cerr.New(cerr.ExitUsage, "usage.args", "install: нужна одна ссылка, передано %d", len(r.Refs))
	}
	res, err := e.InstallAll(ctx, r)
	if len(res) == 0 {
		return nil, err
	}
	return &res[0], err
}

// InstallAll is `cream install <ref>… [KEY=VAL…]` on a live machine (§6.5):
// under lock/global it looks every reference up in the source caches,
// checks the host and the graph "installed ∪ requested" (§9.4; a missing
// dependency lists candidates from all sources) and installs in dependency
// order. A repeat of the same version and digest is decided by the
// registry: installed with no install-scope key changed — no hook,
// changed:false, other parameters applied like set; a changed install-scope
// key, status failed or skipped, --reinstall — the install hook again;
// failed-upgrade — the upgrade again. A higher version is an upgrade, a
// lower one 65 version.downgrade, the same version with another digest
// 65 version.same_digest. The results come in install order; on an error
// they hold the extensions done before it.
//
// In the build phase (Phase()) the same path applies the build rules
// (§10.2): targets.build, hooks under policy-rc.d, configure only enabled,
// systemd untouched. WithDeps adds missing depends first.
func (e *Engine) InstallAll(ctx context.Context, r InstallRequest) ([]InstallResult, error) {
	if len(r.Refs) == 0 {
		return nil, cerr.New(cerr.ExitUsage, "usage.args", "install: нужна ссылка на расширение")
	}
	if len(r.Refs) > 1 && (len(r.Params.Args) > 0 || r.Params.ParamsFile != "") {
		return nil, cerr.New(cerr.ExitUsage, "usage.args", "install: несколько ссылок — только без параметров")
	}
	refs := make([]catalog.Ref, 0, len(r.Refs))
	for _, s := range r.Refs {
		ref, err := catalog.ParseRef(s)
		if err != nil {
			return nil, err
		}
		if r.Source != "" {
			if ref.Source != "" && ref.Source != r.Source {
				ce := cerr.New(cerr.ExitUsage, "usage.source", "ссылка %s и --source %s называют разные источники", s, r.Source)
				ce.Details = map[string]any{"ref": s, "source": r.Source}
				return nil, ce
			}
			ref.Source = r.Source
		}
		refs = append(refs, ref)
	}

	g, err := e.Locks.Global(ctx)
	if err != nil {
		return nil, err
	}
	defer g.Release()
	dirs, err := e.installSourceDirs()
	if err != nil {
		return nil, err
	}
	entries := make([]catalog.Entry, 0, len(refs))
	for _, ref := range refs {
		en, err := catalog.Lookup(dirs, ref, e.Cream, e.Warn)
		if err != nil {
			return nil, err
		}
		entries = append(entries, en)
	}
	var changes map[string][]config.Change
	if len(entries) == 1 && (len(r.Params.Args) > 0 || r.Params.ParamsFile != "") {
		m := entries[0].Manifest
		ch, err := config.ParseAssignments(r.Params, m.Config)
		if err != nil {
			return nil, err
		}
		changes = map[string][]config.Change{m.Name: ch}
	}
	all, err := e.Installed()
	if err != nil {
		return nil, err
	}
	if r.WithDeps {
		if entries, err = e.installWithDeps(dirs, entries, all); err != nil {
			return nil, err
		}
	}
	drop, err := e.buildGuard()
	if err != nil {
		return nil, err
	}
	defer drop()
	return e.installEntries(ctx, entries, changes, all, dirs,
		installOpts{reinstall: r.Reinstall, acceptRisk: r.AcceptRisk, allowSkip: r.AllowSkip})
}

// installWithDeps adds to entries the depends (by name, transitively) that
// neither the registry nor the request has, each from the source of the
// extension that needs it (--with-deps, §9.4). A dependency that source
// lacks stays missing: the graph refuses with 67 deps.missing and its
// candidates. The range is not checked here: a wrong version is 67
// deps.version of the graph. requires are not added.
func (e *Engine) installWithDeps(dirs []source.SourceDir, entries []catalog.Entry, all []Installed) ([]catalog.Entry, error) {
	have := make(map[string]bool, len(all)+len(entries))
	for _, in := range all {
		if in.Manifest != nil {
			have[in.Record.Name] = true
		}
	}
	for _, en := range entries {
		have[en.Manifest.Name] = true
	}
	for i := 0; i < len(entries); i++ {
		en := entries[i]
		for _, d := range en.Manifest.Depends {
			if have[d.Name] {
				continue
			}
			dep, err := catalog.Lookup(dirs, catalog.Ref{Source: en.Source.Source.Name, Name: d.Name}, e.Cream, e.Warn)
			switch {
			case cerr.CodeOf(err) == "not_found.extension":
				continue
			case err != nil:
				return nil, err
			}
			have[d.Name] = true
			entries = append(entries, dep)
		}
	}
	return entries, nil
}

// installSourceDirs reads sources.yaml and sources.lock and returns the
// source caches in priority order.
func (e *Engine) installSourceDirs() ([]source.SourceDir, error) {
	f, err := source.LoadFile(e.L.Sources(), e.L.NeedsRoot())
	if err != nil {
		return nil, err
	}
	lk, err := source.ReadLock(e.L.SourcesLock())
	if err != nil {
		return nil, err
	}
	return source.Dirs(e.L, f, lk)
}

// installEntries checks entries against the host and the graph and
// installs them in order (the common part of install and upgrade). all is
// Installed(), read under lock/global.
func (e *Engine) installEntries(ctx context.Context, entries []catalog.Entry, changes map[string][]config.Change,
	all []Installed, dirs []source.SourceDir, o installOpts) ([]InstallResult, error) {
	prev := make(map[string]*Installed, len(all))
	for i := range all {
		prev[all[i].Record.Name] = &all[i]
	}
	nodes := installNodes(all)
	byName := make(map[string]catalog.Entry, len(entries))
	ops := make(map[string]string, len(entries))
	req := make([]resolve.Node, 0, len(entries))
	params := map[string]map[string]string{}
	for _, en := range entries {
		name := en.Manifest.Name
		op, err := installDecide(prev[name], en)
		if err != nil {
			return nil, err
		}
		if p := prev[name]; op == installOpUpgrade && p.Record.Held {
			return nil, heldError(name, p.Record.Version, en.Manifest.Version.String())
		}
		byName[name], ops[name] = en, op
		req = append(req, resolve.Node{Manifest: en.Manifest, Source: installSourceRef(en.Source), Digest: en.Digest})
		if ch := changes[name]; len(ch) > 0 {
			p := make(map[string]string, len(ch))
			for _, c := range ch {
				p[c.Key] = c.Value
			}
			params[name] = p
		}
	}
	phase := e.Phase()
	host, err := resolve.DetectHost(e.L, phase, e.LookPath)
	if err != nil {
		return nil, err
	}
	host.AcceptRisk = o.acceptRisk
	layers, err := resolve.Resolve(nodes, req, params, host, e.Warn)
	if err != nil {
		return nil, e.installCandidates(err, dirs)
	}
	recs := make([]state.Record, 0, len(all))
	for _, in := range all {
		recs = append(recs, in.Record)
	}
	out := make([]InstallResult, 0, len(layers))
	for _, layer := range layers {
		if layer.Mode == resolve.ModeNone {
			e.warn("install.build_none", "%s: targets.build: none — в фазе build не ставится", layer.Name)
			continue
		}
		job := installJob{entry: byName[layer.Name], layer: layer, changes: changes[layer.Name],
			prev: prev[layer.Name], op: ops[layer.Name], phase: phase, registry: recs}
		res, err := e.installOne(ctx, job, o)
		if res != nil {
			out = append(out, *res)
			recs = installReplace(recs, res.Record)
		}
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

// installReplace puts rec into the registry snapshot recs (by name).
func installReplace(recs []state.Record, rec state.Record) []state.Record {
	for i := range recs {
		if recs[i].Name == rec.Name {
			recs[i] = rec
			return recs
		}
	}
	return append(recs, rec)
}

// installDecide is the operation for entry en against the installed prev:
// install (first, or a repeat of the same version and digest) or upgrade
// (a higher version, or the repeat of a failed upgrade); 65 for a lower
// version and for the same version with another digest.
func installDecide(prev *Installed, en catalog.Entry) (string, error) {
	if prev == nil {
		return installOpInstall, nil
	}
	name, rec := en.Manifest.Name, prev.Record
	old, err := semver.Parse(rec.Version)
	if err != nil {
		ce := cerr.Wrap(err, cerr.ExitState, "state.corrupt", "%s: версия в реестре %q", name, rec.Version)
		ce.Details = map[string]any{"ext": name}
		return "", ce
	}
	details := map[string]any{"ext": name, "installed": rec.Version, "found": en.Manifest.Version.String()}
	switch c := semver.Compare(en.Manifest.Version, old); {
	case c < 0:
		ce := cerr.New(cerr.ExitData, "version.downgrade",
			"%s: установлена %s, в источнике %s — понижение версии не поддерживается (remove, затем install нужного пина)",
			name, rec.Version, en.Manifest.Version)
		ce.Details = details
		return "", ce
	case c > 0:
		return installOpUpgrade, nil
	case rec.ExtDigest != en.Digest:
		ce := cerr.New(cerr.ExitData, "version.same_digest",
			"%s %s: отпечаток дерева в источнике отличается от установленного — пересборка без подъёма версии", name, rec.Version)
		details["installed_digest"], details["found_digest"] = rec.ExtDigest, en.Digest
		ce.Details = details
		return "", ce
	}
	return installOpInstall, nil
}

// installSourceRef is the registry form of the source an entry came from.
func installSourceRef(d source.SourceDir) state.SourceRef {
	ref := state.SourceRef{Name: d.Source.Name, Type: d.Source.Type, Origin: d.Source.URL}
	if d.Source.Type == source.TypePath {
		ref.Origin = d.Source.Path
	}
	if d.Lock != nil {
		ref.Origin, ref.Resolved, ref.CatalogVersion = d.Lock.Origin, d.Lock.Resolved, d.Lock.CatalogVersion
	}
	return ref
}

// installCandidate is one extension that could satisfy a missing
// dependency (details.candidates of 67 deps.missing, §9.4).
type installCandidate struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Source  string `json:"source"`
	// Matches: the version is in the range of the dependency.
	Matches bool `json:"matches"`
}

// installCandidates adds details.candidates to 67 deps.missing: every
// source that has a missing extension, with its version. Other errors pass.
func (e *Engine) installCandidates(err error, dirs []source.SourceDir) error {
	var ce *cerr.Error
	if !errors.As(err, &ce) || ce.Code != "deps.missing" {
		return err
	}
	missing, _ := ce.Details["missing"].([]resolve.Missing)
	cands := []installCandidate{}
	for _, m := range missing {
		for _, d := range dirs {
			if !d.Synced {
				continue
			}
			en, lerr := catalog.Lookup([]source.SourceDir{d}, catalog.Ref{Name: m.Name}, e.Cream, &cerr.Warnings{})
			if lerr != nil {
				continue
			}
			c := installCandidate{Name: m.Name, Version: en.Manifest.Version.String(), Source: d.Source.Name, Matches: true}
			if m.Version != "" {
				rg, perr := semver.ParseRange(m.Version)
				c.Matches = perr == nil && rg.Contains(en.Manifest.Version)
			}
			cands = append(cands, c)
		}
	}
	ce.Details["candidates"] = cands
	if len(cands) > 0 {
		parts := make([]string, 0, len(cands))
		for _, c := range cands {
			parts = append(parts, fmt.Sprintf("%s/%s@%s", c.Source, c.Name, c.Version))
		}
		ce.Message += "; есть в источниках: " + strings.Join(parts, ", ")
	}
	return ce
}

// installJob is one extension to install or upgrade.
type installJob struct {
	entry    catalog.Entry
	layer    resolve.Layer
	changes  []config.Change
	prev     *Installed
	op       string
	phase    string
	registry []state.Record
}

// installOne installs or upgrades one extension (§6.5): parameters, the
// tree through .<ext>.new (an upgrade keeps the old one in .<ext>.prev),
// files, the hook, configure, the registry and history; on a live machine
// then daemon-reload and a synchronous configure.
func (e *Engine) installOne(ctx context.Context, j installJob, o installOpts) (*InstallResult, error) {
	m := j.entry.Manifest
	if err := refuseScopeBuild(m); err != nil {
		return nil, err
	}
	name := m.Name
	h, err := e.Locks.Ext(ctx, name, lock.Exclusive)
	if err != nil {
		return nil, err
	}
	defer h.Release()
	dom := config.Open(e.L, name, m.Config)

	if j.op == installOpInstall && j.prev != nil && j.prev.Record.Status == state.StatusInstalled && !o.reinstall {
		scoped, err := installScopeChanged(dom, m, j.changes)
		if err != nil {
			return nil, err
		}
		if !scoped {
			h.Release()
			res := &InstallResult{Record: j.prev.Record}
			if rest := installNonScope(m, j.changes); len(rest) > 0 {
				if _, err := e.Set(ctx, name, rest, SetOptions{}); err != nil {
					return res, err
				}
			}
			return res, nil
		}
	}

	// Parameters are checked before the tree and written after files (§6.5,
	// steps 3–4): a refusal of files (67) leaves the domain untouched.
	if len(j.changes) > 0 {
		if _, err := dom.Set(j.changes, config.SetOpts{AllowInstallScope: true, Only: []string{}}); err != nil {
			return nil, err
		}
	}
	upgrade := j.op == installOpUpgrade
	if err := e.installStage(name, j.entry.Dir); err != nil {
		return nil, err
	}
	files, err := place.Place(e.L, name, e.L.ExtNew(name), m.Files, installOwner(j.registry))
	if err != nil {
		e.installDropStage(name)
		if len(files) > 0 {
			if uerr := place.Unplace(e.L, files, e.Warn); uerr != nil {
				e.warn("install.unplace", "%s: откат files: %v", name, uerr)
			}
		}
		return nil, err
	}
	if err := e.installSwap(name, upgrade); err != nil {
		return nil, err
	}
	if j.prev != nil {
		e.installUnplaceStale(j.prev.Record.Files, files)
	}

	rec := state.Record{Name: name, Version: m.Version.String(), Kind: string(m.Kind),
		Source: installSourceRef(j.entry.Source), ExtDigest: j.entry.Digest, ParamsDigest: j.layer.ParamsDigest,
		Phase: j.phase, CreamVersion: e.installCreamVersion(), Files: files}
	from := ""
	if j.prev != nil {
		from = j.prev.Record.Version
		rec.Phase, rec.InstalledAt, rec.Configure = j.prev.Record.Phase, j.prev.Record.InstalledAt, j.prev.Record.Configure
		rec.Held = j.prev.Record.Held
	}
	hook, env := manifest.HookInstall, runner.EnvInput{Phase: j.phase}
	if upgrade {
		env.PrevVersion = from
		if hasHook(m, manifest.HookUpgrade) {
			hook = manifest.HookUpgrade
		}
	}
	outcome := runner.OK
	var herr error
	if len(j.changes) > 0 {
		only := make([]string, 0, len(j.changes))
		for _, c := range j.changes {
			only = append(only, c.Key)
		}
		// A write that fails after the tree and files are in place is a failed
		// operation in the registry, like a failed hook: the retry repeats it.
		_, herr = dom.Set(j.changes, config.SetOpts{AllowInstallScope: true, Only: only})
	}
	if herr == nil && hasHook(m, hook) {
		outcome, herr = e.RunHook(WithLock(ctx, h, lock.Exclusive), Installed{Record: rec, Manifest: m}, hook, env)
	}
	switch {
	case outcome == runner.NotApplicable:
		rec.Status = state.StatusSkipped
		if o.allowSkip {
			herr = nil
		}
	case herr != nil && upgrade:
		// The old version stays in the registry and in .prev (§6.5).
		failed := j.prev.Record
		failed.Status, failed.PrevVersion, failed.Files = state.StatusFailedUpgrade, from, files
		rec = failed
	case herr != nil:
		rec.Status = state.StatusFailed
	default:
		rec.Status = state.StatusInstalled
		rec.RebootRequired = outcome == runner.Reboot
		if err := e.installEnable(&rec, m); err != nil {
			return nil, err
		}
		e.installDropPrev(name)
	}
	if err := e.Store.Put(rec); err != nil {
		return nil, err
	}
	if err := e.Store.Append(state.HistoryEntry{Op: j.op, Ext: name, From: from, To: m.Version.String(),
		Status: rec.Status, Phase: j.phase}); err != nil {
		e.warn("state.history", "%s: журнал операций: %v", name, err)
	}
	if put, ok, gerr := e.Store.Get(name); gerr == nil && ok {
		rec = *put
	}
	res := &InstallResult{Record: rec, Changed: true}
	if herr != nil || rec.Status != state.StatusInstalled {
		return res, herr
	}
	h.Release()
	return res, e.installLive(ctx, rec, m, j.op, j.phase)
}

// installLive finishes an install on a live machine (§6.4): daemon-reload
// when units may have changed (files, the configure drop-in) and a
// synchronous configure with reason install or upgrade (queued with a
// warning while systemd boots; run directly without systemd).
func (e *Engine) installLive(ctx context.Context, rec state.Record, m *manifest.Manifest, op, phase string) error {
	if phase != PhaseLive {
		return nil
	}
	if _, up := e.Systemctl.Running(ctx); up && (len(rec.Files) > 0 || rec.Configure != nil) {
		if err := e.Systemctl.DaemonReload(ctx); err != nil {
			e.warn("systemd.reload_failed", "systemctl daemon-reload: %v", err)
		}
	}
	if !hasHook(m, manifest.HookConfigure) {
		return nil
	}
	reason := ReasonInstall
	if op == installOpUpgrade {
		reason = ReasonUpgrade
	}
	return e.Configure(ctx, rec.Name, reason, ConfigureOptions{Wait: true})
}

// installEnable enables cream-configure@<ext> for a configure hook (§6.4)
// or removes a configure the new version no longer has.
func (e *Engine) installEnable(rec *state.Record, m *manifest.Manifest) error {
	hk, ok := m.Hooks[manifest.HookConfigure]
	if !ok {
		if rec.Configure != nil {
			if err := systemd.Disable(e.L, rec.Name); err != nil {
				return installSystemdErr(err, rec.Name)
			}
		}
		rec.Configure = nil
		return nil
	}
	target, err := systemd.Enable(e.L, rec.Name, systemd.StageSpec{Stage: string(hk.Stage),
		After: hk.After, Before: hk.Before, Wants: hk.Wants})
	if err != nil {
		return installSystemdErr(err, rec.Name)
	}
	rec.Configure = &state.ConfigureRec{Stage: string(hk.Stage), Target: target}
	return nil
}

func installSystemdErr(err error, ext string) error {
	var ce *cerr.Error
	if errors.As(err, &ce) {
		return err
	}
	return cerr.Wrap(err, cerr.ExitState, cerr.CodeIOWrite, "%s: configure", ext)
}

// installCreamVersion is cream_version of the registry.
func (e *Engine) installCreamVersion() string {
	if e.Cream.Dev {
		return "dev"
	}
	return e.Cream.Version.String()
}

// installOwner is the ownership of files by the registry snapshot recs:
// every record of every status, the extension's own included (Р21).
func installOwner(recs []state.Record) place.Owner {
	owners := map[string]string{}
	for _, r := range recs {
		for _, f := range r.Files {
			owners[f.Dst] = r.Name
		}
	}
	return func(dst string) (string, bool) {
		ext, ok := owners[dst]
		return ext, ok
	}
}

// installUnplaceStale removes files of the previous installation the new
// one no longer lays out.
func (e *Engine) installUnplaceStale(old, placed []state.FileRec) {
	var stale []state.FileRec
	for _, f := range old {
		if !slices.ContainsFunc(placed, func(p state.FileRec) bool { return p.Dst == f.Dst }) {
			stale = append(stale, f)
		}
	}
	if len(stale) == 0 {
		return
	}
	if err := place.Unplace(e.L, stale, e.Warn); err != nil {
		e.warn("install.unplace", "прежние files не убраны: %v", err)
	}
}

// installScopeChanged reports whether changes give a scope: install key a
// value other than the effective one (§8.5).
func installScopeChanged(dom *config.Domain, m *manifest.Manifest, changes []config.Change) (bool, error) {
	var vals map[string]string
	for _, c := range changes {
		sp, ok := m.Config.Lookup(c.Key)
		if !ok || sp.Scope != keytype.ScopeInstall {
			continue
		}
		if sp.Secret != keytype.SecretNone {
			cur, err := dom.Get(c.Key, true)
			if err != nil || cur != c.Value {
				return true, nil
			}
			continue
		}
		if vals == nil {
			var err error
			if vals, err = dom.Values(); err != nil {
				return false, err
			}
		}
		if cur, ok := vals[c.Key]; !ok || cur != c.Value {
			return true, nil
		}
	}
	return false, nil
}

// installNonScope drops scope: install keys (unchanged here) from changes.
func installNonScope(m *manifest.Manifest, changes []config.Change) []config.Change {
	out := make([]config.Change, 0, len(changes))
	for _, c := range changes {
		if sp, ok := m.Config.Lookup(c.Key); ok && sp.Scope == keytype.ScopeInstall {
			continue
		}
		out = append(out, c)
	}
	return out
}
