package engine

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/iamletenkov/cream/internal/catalog"
	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/resolve"
	"github.com/iamletenkov/cream/internal/source"
	"github.com/iamletenkov/cream/internal/state"
)

// Actions of an upgrade plan item: what upgrade decided for the extension.
const (
	ActionUpgrade  = "upgrade"    // a higher version in the source: upgrade From → To
	ActionRetry    = "retry"      // the same version, the record failed: the operation is repeated
	ActionUpToDate = "up-to-date" // nothing newer: no hook, no platform or risk check
	ActionHeld     = "held"       // cream hold: upgrade --all skips it
	ActionBlocked  = "blocked"    // cannot be upgraded: Reason and Error say why
	ActionSkipped  = "skipped"    // upgrade --all: installed not from a source (no_source)
)

// Reasons of a blocked (or skipped, held) item.
const (
	ReasonRisk       = "risk"       // targets.live: confirm without --accept-risk <name>
	ReasonPlatform   = "platform"   // platform.*, target.* of the new version (68)
	ReasonDependency = "dependency" // the graph (67) or a provider whose upgrade failed
	ReasonHeld       = "held"       // cream hold
	ReasonSource     = "source"     // the source or the extension in it is missing (69, 66)
	ReasonVersion    = "version"    // version.downgrade, version.same_digest (65)
	ReasonData       = "data"       // the manifest or the catalog is invalid (65)
	ReasonNoSource   = "no_source"  // installed with --from-dir: nothing to upgrade from
	ReasonError      = "error"      // anything else
)

// Statuses of an item after the run (planned — dry-run and the confirmation).
const (
	StatusPlanned   = "planned"
	StatusUpgraded  = "upgraded"
	StatusUnchanged = "unchanged"
	StatusFailed    = "failed"
	StatusBlocked   = "blocked"
	StatusHeld      = "held"
	StatusSkipped   = "skipped"
	StatusNotRun    = "not_run" // --fail-fast stopped before it
)

// AcceptRiskAll is --accept-risk all: every targets.live: confirm extension.
const AcceptRiskAll = "all"

// UpgradeRequest is `cream upgrade` (§6.5).
type UpgradeRequest struct {
	// Names are the extensions to upgrade; All — every installed one.
	Names []string
	All   bool
	// AcceptRisk are the names (or "all") whose targets.live: confirm is accepted.
	AcceptRisk []string
	// DryRun builds the plan and changes nothing.
	DryRun bool
	// FailFast stops at the first blocked item of the plan (nothing is done)
	// or the first failed upgrade (the rest are not_run): the 1.x behaviour.
	FailFast bool
	// ConfirmRisk asks the operator about a targets.live: confirm extension
	// not in AcceptRisk (a TTY without -y); nil — it is blocked:risk.
	ConfirmRisk func(name, note string) (bool, error)
	// Confirm shows the plan and asks to go on (a TTY without -y); nil — no
	// question. It is asked only when something is to be upgraded.
	Confirm func(plan []UpgradeItem) (bool, error)
}

// UpgradeError is the reason of a blocked or failed item (--json).
type UpgradeError struct {
	Exit    int            `json:"exit"`
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

// UpgradeItem is one extension of the plan.
type UpgradeItem struct {
	Name   string        `json:"name"`
	From   string        `json:"from"`
	To     string        `json:"to"`
	Action string        `json:"action"`
	Reason string        `json:"reason"`
	Status string        `json:"status"`
	Error  *UpgradeError `json:"error"`
	err    error
}

// Err is the error of a blocked or failed item, nil otherwise.
func (it UpgradeItem) Err() error { return it.err }

func (it *UpgradeItem) fail(status, reason string, err error) {
	it.Status, it.Reason, it.err = status, reason, err
	it.Error = &UpgradeError{Exit: cerr.ExitCode(err), Code: cerr.CodeOf(err), Message: err.Error(),
		Details: cerr.DetailsOf(err)}
}

func (it *UpgradeItem) block(reason string, err error) {
	it.Action = ActionBlocked
	it.fail(StatusBlocked, reason, err)
}

// UpgradeSummary counts the items: skipped are held, blocked, skipped and
// not_run; ExitCode is the code of the worst outcome (a failed upgrade over a
// blocked item; the first in plan order), 0 when there is none.
type UpgradeSummary struct {
	Upgraded  int `json:"upgraded"`
	Unchanged int `json:"unchanged"`
	Skipped   int `json:"skipped"`
	Failed    int `json:"failed"`
	ExitCode  int `json:"exit_code"`
}

// UpgradeReport is the result of `cream upgrade`: the plan in upgrade order
// (topological: providers first; then the items with nothing to do), the
// registry records of what was upgraded or is up to date (extensions, the
// 1.x form; empty with --dry-run) and the summary.
type UpgradeReport struct {
	DryRun     bool            `json:"dry_run"`
	Plan       []UpgradeItem   `json:"plan"`
	Extensions []InstallResult `json:"extensions"`
	Summary    UpgradeSummary  `json:"summary"`
}

// worst is the index of the item that decides the exit code, -1 for none.
func (r *UpgradeReport) worst() int {
	blocked := -1
	for i, it := range r.Plan {
		switch {
		case it.err == nil:
		case it.Status == StatusFailed:
			return i
		case blocked < 0:
			blocked = i
		}
	}
	return blocked
}

// Err is nil when every item is fine (upgraded, up to date, held or skipped
// by --all); the error of the only bad item when there is one; otherwise
// upgrade.partial with the exit code of the worst outcome.
func (r *UpgradeReport) Err() error {
	w := r.worst()
	if w < 0 {
		return nil
	}
	bad := 0
	for _, it := range r.Plan {
		if it.err != nil {
			bad++
		}
	}
	if bad == 1 {
		return r.Plan[w].err
	}
	s := r.Summary
	ce := cerr.New(s.ExitCode, "upgrade.partial",
		"upgrade: обновлено %d, без изменений %d, пропущено %d, ошибок %d; худший исход — %s: %s",
		s.Upgraded, s.Unchanged, s.Skipped, s.Failed, r.Plan[w].Name, r.Plan[w].err.Error())
	ce.Details = map[string]any{"upgraded": s.Upgraded, "unchanged": s.Unchanged, "skipped": s.Skipped,
		"failed": s.Failed, "ext": r.Plan[w].Name}
	return ce
}

func (r *UpgradeReport) summarize() {
	s := UpgradeSummary{}
	for _, it := range r.Plan {
		switch it.Status {
		case StatusUpgraded:
			s.Upgraded++
		case StatusUnchanged:
			s.Unchanged++
		case StatusFailed:
			s.Failed++
		case StatusPlanned:
		default:
			s.Skipped++
		}
	}
	if w := r.worst(); w >= 0 {
		s.ExitCode = cerr.ExitCode(r.Plan[w].err)
	}
	r.Summary = s
}

// upgradeCand is an item to upgrade (action upgrade or retry).
type upgradeCand struct {
	entry catalog.Entry
	prev  *Installed
	op    string
}

// Upgrade is `cream upgrade <ext>…` or `cream upgrade --all` (§6.5), like
// apt/dnf: under lock/global it first builds the plan of every extension —
// upgrade From → To, up-to-date, held, blocked:<reason> — and only then acts.
//
// The new version is looked up in the source the registry record names (Р8).
// An extension with nothing newer is up-to-date: no hook and no platform or
// risk check (a targets.live: confirm one does not block --all). One with an
// upgrade is checked against the host: a targets.live: confirm one needs
// --accept-risk <name> (or all, or a yes to ConfirmRisk), else it is
// blocked:risk; other platform refusals are blocked:platform. The graph of
// "installed ∪ upgrades" blocks (blocked:dependency) the extension a graph
// error names, until the rest resolves.
//
// Upgrades run in topological order (providers first). A failed one does not
// stop the others, except those that depend on it (blocked:dependency);
// FailFast stops instead. The report holds the plan, the records and the
// summary; Err gives the error of the worst outcome. The error of Upgrade
// itself is for what prevents any plan: usage, an extension not installed,
// the lock, sources.yaml, the registry, a declined Confirm (1 upgrade.declined).
func (e *Engine) Upgrade(ctx context.Context, r UpgradeRequest) (*UpgradeReport, error) {
	switch {
	case r.All && len(r.Names) > 0:
		return nil, cerr.New(cerr.ExitUsage, "usage.args", "upgrade: имена расширений и --all несовместимы")
	case !r.All && len(r.Names) == 0:
		return nil, cerr.New(cerr.ExitUsage, "usage.args", "upgrade: нужны имена расширений или --all")
	}
	for _, n := range r.Names {
		if err := CheckExtName(n); err != nil {
			return nil, err
		}
	}
	for _, n := range r.AcceptRisk {
		if n == AcceptRiskAll {
			continue
		}
		if err := CheckExtName(n); err != nil {
			return nil, err
		}
	}
	g, err := e.Locks.Global(ctx)
	if err != nil {
		return nil, err
	}
	defer g.Release()
	installed, err := e.Installed()
	if err != nil {
		return nil, err
	}
	targets, err := upgradeTargets(installed, r)
	if err != nil {
		return nil, err
	}
	dirs, err := e.installSourceDirs()
	if err != nil {
		return nil, err
	}
	host, err := resolve.DetectHost(e.L, e.Phase(), e.LookPath)
	if err != nil {
		return nil, err
	}

	rep := &UpgradeReport{DryRun: r.DryRun, Extensions: []InstallResult{}}
	cands := map[string]*upgradeCand{}
	byName := map[string]*UpgradeItem{}
	items := make([]*UpgradeItem, 0, len(targets))
	for i := range targets {
		it, cand, err := e.upgradeItem(&targets[i], r, dirs, host)
		if err != nil {
			return nil, err
		}
		items = append(items, it)
		byName[it.Name] = it
		if cand != nil {
			cands[it.Name] = cand
		}
	}

	layers := e.upgradeGraph(installed, cands, byName, host, dirs)
	order := make([]*UpgradeItem, 0, len(items))
	for _, l := range layers {
		order = append(order, byName[l.Name])
	}
	for _, it := range items {
		if !slices.Contains(order, it) {
			order = append(order, it)
		}
	}
	blocked := false
	for _, it := range order {
		blocked = blocked || it.Status == StatusBlocked
		if _, ok := cands[it.Name]; ok {
			it.Status = StatusPlanned
		}
	}
	finish := func() (*UpgradeReport, error) {
		rep.Plan = make([]UpgradeItem, 0, len(order))
		for _, it := range order {
			rep.Plan = append(rep.Plan, *it)
		}
		for _, it := range order {
			if it.Status == StatusUnchanged && !r.DryRun {
				in, _ := upgradeFind(installed, it.Name)
				rep.Extensions = append(rep.Extensions, InstallResult{Record: in.Record})
			}
		}
		rep.summarize()
		return rep, nil
	}
	if r.FailFast && blocked {
		for _, l := range layers {
			byName[l.Name].Status = StatusNotRun
		}
		return finish()
	}
	if r.DryRun {
		return finish()
	}
	if r.Confirm != nil && len(layers) > 0 {
		plan := make([]UpgradeItem, 0, len(order))
		for _, it := range order {
			plan = append(plan, *it)
		}
		ok, err := r.Confirm(plan)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, cerr.New(cerr.ExitCheck, "upgrade.declined", "upgrade: отменено — ничего не изменено")
		}
	}
	drop, err := e.buildGuard()
	if err != nil {
		return nil, err
	}
	defer drop()
	e.upgradeRun(ctx, r, layers, cands, byName, installed, rep)
	return finish()
}

// upgradeTargets are the installed extensions upgrade looks at: all (by
// name) or the named ones in the given order (66 for one not installed).
func upgradeTargets(installed []Installed, r UpgradeRequest) ([]Installed, error) {
	if r.All {
		return installed, nil
	}
	var out []Installed
	seen := map[string]bool{}
	for _, n := range r.Names {
		if seen[n] {
			continue
		}
		seen[n] = true
		in, ok := upgradeFind(installed, n)
		if !ok {
			ce := cerr.New(cerr.ExitNotFound, "not_found.extension", "расширение %s не установлено", n)
			ce.Details = map[string]any{"ext": n}
			return nil, ce
		}
		out = append(out, in)
	}
	return out, nil
}

// upgradeItem decides one extension: the item and, for an upgrade or a
// retry that passed the platform and risk checks, the candidate. The error is
// only that of ConfirmRisk.
func (e *Engine) upgradeItem(in *Installed, r UpgradeRequest, dirs []source.SourceDir,
	host resolve.Host) (*UpgradeItem, *upgradeCand, error) {
	rec := in.Record
	name, src := rec.Name, rec.Source.Name
	it := &UpgradeItem{Name: name, From: rec.Version, To: rec.Version}
	if rec.Held {
		if r.All {
			it.Action, it.Reason, it.Status = ActionHeld, ReasonHeld, StatusHeld
		} else {
			it.block(ReasonHeld, heldError(name, rec.Version, ""))
		}
		return it, nil, nil
	}
	d, ok := upgradeSource(dirs, src)
	if !ok {
		if r.All && src == "" {
			e.warn("upgrade.no_source", "%s: поставлено не из источника — пропущено", name)
			it.Action, it.Reason, it.Status = ActionSkipped, ReasonNoSource, StatusSkipped
			return it, nil, nil
		}
		ce := cerr.New(cerr.ExitSource, "source.missing",
			"%s: источника %q из реестра нет в sources.yaml — обновлять неоткуда", name, src)
		ce.Details = map[string]any{"ext": name, "source": src}
		it.block(ReasonSource, ce)
		return it, nil, nil
	}
	en, err := catalog.Lookup([]source.SourceDir{d}, catalog.Ref{Source: src, Name: name}, e.Cream, e.Warn)
	if err != nil {
		var ce *cerr.Error
		if errors.As(err, &ce) && ce.Code == "not_found.extension" {
			ce.Message += " (источник " + src + ")"
		}
		it.block(upgradeReason(err), err)
		return it, nil, nil
	}
	m := en.Manifest
	it.To = m.Version.String()
	op, err := installDecide(in, en, false)
	if err != nil {
		it.block(upgradeReason(err), err)
		return it, nil, nil
	}
	switch {
	case op == installOpUpgrade:
		it.Action = ActionUpgrade
	case rec.Status == state.StatusFailed || rec.Status == state.StatusFailedUpgrade:
		it.Action = ActionRetry
	default:
		it.Action, it.Status = ActionUpToDate, StatusUnchanged
		return it, nil, nil
	}
	h := host
	h.AcceptRisk = slices.Contains(r.AcceptRisk, AcceptRiskAll) || slices.Contains(r.AcceptRisk, name)
	if err := resolve.CheckPlatform(m, h, e.Warn); err != nil {
		if cerr.CodeOf(err) != "target.confirm" {
			it.block(ReasonPlatform, err)
			return it, nil, nil
		}
		yes := false
		if r.ConfirmRisk != nil {
			if yes, err = r.ConfirmRisk(name, m.Targets.LiveNote); err != nil {
				return nil, nil, err
			}
		}
		if !yes {
			ce := cerr.New(cerr.ExitNotApplicable, "target.confirm",
				"%s: targets.live: confirm (%s) — обновление только с --accept-risk %s", name, m.Targets.LiveNote, name)
			ce.Details = map[string]any{"ext": name, "note": m.Targets.LiveNote}
			it.block(ReasonRisk, ce)
			return it, nil, nil
		}
	}
	return it, &upgradeCand{entry: en, prev: in, op: op}, nil
}

// upgradeReason is the reason of a blocked item by its error.
func upgradeReason(err error) string {
	code := cerr.CodeOf(err)
	switch cerr.ExitCode(err) {
	case cerr.ExitSource, cerr.ExitNotFound:
		return ReasonSource
	case cerr.ExitData:
		if strings.HasPrefix(code, "version.") {
			return ReasonVersion
		}
		return ReasonData
	case cerr.ExitNotApplicable:
		return ReasonPlatform
	case cerr.ExitGraph:
		return ReasonDependency
	}
	return ReasonError
}

// upgradeGraph resolves the candidates against "installed ∪ upgrades" and
// returns their layers in install order. A graph error blocks
// (blocked:dependency) the candidates it names and the rest is resolved
// again, so one inconsistent upgrade does not hold the others back.
func (e *Engine) upgradeGraph(installed []Installed, cands map[string]*upgradeCand, byName map[string]*UpgradeItem,
	host resolve.Host, dirs []source.SourceDir) []resolve.Layer {
	nodes := installNodes(installed)
	host.AcceptRisk = true // checked per candidate in upgradeItem
	for len(cands) > 0 {
		names := make([]string, 0, len(cands))
		for n := range cands {
			names = append(names, n)
		}
		slices.Sort(names)
		req := make([]resolve.Node, 0, len(cands))
		for _, n := range names {
			en := cands[n].entry
			req = append(req, resolve.Node{Manifest: en.Manifest, Source: installSourceRef(en.Source), Digest: en.Digest})
		}
		layers, err := resolve.Resolve(nodes, req, nil, host, &cerr.Warnings{})
		if err == nil {
			return layers
		}
		blamed := upgradeBlame(err, names, cands)
		if len(blamed) == len(names) && len(names) > 1 {
			// Nothing in the error names a candidate: the ones whose removal
			// alone lets the rest resolve are to blame.
			var single []string
			for i, n := range names {
				rest := slices.Delete(slices.Clone(req), i, i+1)
				if _, rerr := resolve.Resolve(nodes, rest, nil, host, &cerr.Warnings{}); rerr == nil {
					single = append(single, n)
				}
			}
			if len(single) > 0 {
				blamed = single
			}
		}
		err = e.installCandidates(err, dirs)
		for _, n := range blamed {
			byName[n].block(ReasonDependency, err)
			delete(cands, n)
		}
	}
	return nil
}

// upgradeBlame are the candidates a graph error names: details.ext, else
// any candidate among the other details (dep, with, owners, cycle), else
// those whose installed version provides details.capability and the new one
// does not (an installed consumer lost its provider), else all of them.
func upgradeBlame(err error, names []string, cands map[string]*upgradeCand) []string {
	d := cerr.DetailsOf(err)
	if ext, _ := d["ext"].(string); slices.Contains(names, ext) {
		return []string{ext}
	}
	var out []string
	add := func(s string) {
		if slices.Contains(names, s) && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	for _, k := range []string{"dep", "with", "owners", "cycle"} {
		switch v := d[k].(type) {
		case string:
			add(v)
		case []string:
			for _, s := range v {
				add(s)
			}
		}
	}
	if c, _ := d["capability"].(string); c != "" && len(out) == 0 {
		for _, n := range names {
			old, cur := cands[n].prev.Manifest, cands[n].entry.Manifest
			if old != nil && slices.Contains(old.Provides, c) && !slices.Contains(cur.Provides, c) {
				add(n)
			}
		}
	}
	if len(out) == 0 {
		return names
	}
	return out
}

// installNodes are the graph nodes of the installed extensions (a record
// without a readable tree is left out).
func installNodes(all []Installed) []resolve.Node {
	nodes := make([]resolve.Node, 0, len(all))
	for i := range all {
		in := &all[i]
		if in.Manifest == nil {
			continue
		}
		files := make([]string, 0, len(in.Record.Files))
		for _, f := range in.Record.Files {
			files = append(files, f.Dst)
		}
		nodes = append(nodes, resolve.Node{Manifest: in.Manifest, Installed: true, Files: files,
			Source: in.Record.Source, Digest: in.Record.ExtDigest})
	}
	return nodes
}

// upgradeRun upgrades the layers in order. A layer whose dependency failed
// (or was blocked here) is blocked:dependency; a failure stops the rest only
// with FailFast or a canceled context.
func (e *Engine) upgradeRun(ctx context.Context, r UpgradeRequest, layers []resolve.Layer, cands map[string]*upgradeCand,
	byName map[string]*UpgradeItem, installed []Installed, rep *UpgradeReport) {
	recs := make([]state.Record, 0, len(installed))
	for _, in := range installed {
		recs = append(recs, in.Record)
	}
	phase := e.Phase()
	bad := map[string]bool{}
	stop := false
	for _, layer := range layers {
		it, c := byName[layer.Name], cands[layer.Name]
		if stop {
			it.Status = StatusNotRun
			continue
		}
		if dep := upgradeFailedDep(layer, bad); dep != "" {
			ce := cerr.New(cerr.ExitGraph, "deps.failed",
				"%s не обновлено: зависит от %s, обновление которого не удалось", layer.Name, dep)
			ce.Details = map[string]any{"ext": layer.Name, "dep": dep}
			it.fail(StatusBlocked, ReasonDependency, ce)
			bad[layer.Name] = true
			continue
		}
		if layer.Mode == resolve.ModeNone {
			e.warn("install.build_none", "%s: targets.build: none — в фазе build не ставится", layer.Name)
			it.Status = StatusSkipped
			continue
		}
		job := installJob{entry: c.entry, layer: layer, prev: c.prev, op: c.op, phase: phase, registry: recs}
		res, err := e.installOne(ctx, job, installOpts{acceptRisk: true})
		if res != nil {
			rep.Extensions = append(rep.Extensions, *res)
			recs = installReplace(recs, res.Record)
		}
		if err != nil {
			it.fail(StatusFailed, "", err)
			// A failure after the new version is in place (configure) does
			// not hold its dependents back.
			bad[layer.Name] = res == nil || res.Status != state.StatusInstalled
			stop = r.FailFast || ctx.Err() != nil
			continue
		}
		it.Status = StatusUpgraded
	}
}

// upgradeFailedDep is the first dependency of layer (Deps, "name@version")
// among bad, "" for none.
func upgradeFailedDep(layer resolve.Layer, bad map[string]bool) string {
	for _, d := range layer.Deps {
		name, _, _ := strings.Cut(d, "@")
		if bad[name] {
			return name
		}
	}
	return ""
}

func upgradeFind(all []Installed, name string) (Installed, bool) {
	for _, in := range all {
		if in.Record.Name == name {
			return in, true
		}
	}
	return Installed{}, false
}

func upgradeSource(dirs []source.SourceDir, name string) (source.SourceDir, bool) {
	if name == "" {
		return source.SourceDir{}, false
	}
	for _, d := range dirs {
		if d.Source.Name == name {
			return d, true
		}
	}
	return source.SourceDir{}, false
}
