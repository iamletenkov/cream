package engine

import (
	"context"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/state"
)

// HoldResult is one extension of `cream hold` / `cream unhold`.
type HoldResult struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Held    bool   `json:"held"`
	Changed bool   `json:"changed"`
}

// History operations of hold and unhold.
const (
	holdOpHold   = "hold"
	holdOpUnhold = "unhold"
)

// Hold is `cream hold <ext>…` (held true) or `cream unhold <ext>…` (false):
// the flag lives in the registry record (field held), written under
// lock/global like every record. A held extension is skipped by
// `upgrade --all` and refused by an explicit upgrade or an install of another
// version (68 hold.held). Every name is checked before anything is written
// (66 not_found.extension); an unchanged one is changed:false.
func (e *Engine) Hold(ctx context.Context, names []string, held bool) ([]HoldResult, error) {
	if len(names) == 0 {
		return nil, cerr.New(cerr.ExitUsage, "usage.args", "нужны имена расширений")
	}
	for _, n := range names {
		if err := CheckExtName(n); err != nil {
			return nil, err
		}
	}
	g, err := e.Locks.Global(ctx)
	if err != nil {
		return nil, err
	}
	defer g.Release()
	recs := make([]state.Record, 0, len(names))
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			continue
		}
		seen[n] = true
		rec, ok, err := e.Store.Get(n)
		if err != nil {
			return nil, err
		}
		if !ok {
			ce := cerr.New(cerr.ExitNotFound, "not_found.extension", "расширение %s не установлено", n)
			ce.Details = map[string]any{"ext": n}
			return nil, ce
		}
		recs = append(recs, *rec)
	}
	op := holdOpHold
	if !held {
		op = holdOpUnhold
	}
	out := make([]HoldResult, 0, len(recs))
	for _, rec := range recs {
		res := HoldResult{Name: rec.Name, Version: rec.Version, Held: held, Changed: rec.Held != held}
		if res.Changed {
			rec.Held = held
			if err := e.Store.Put(rec); err != nil {
				return out, err
			}
			if err := e.Store.Append(state.HistoryEntry{Op: op, Ext: rec.Name, From: rec.Version, To: rec.Version,
				Status: rec.Status, Phase: rec.Phase}); err != nil {
				e.warn("state.history", "%s: журнал операций: %v", rec.Name, err)
			}
		}
		out = append(out, res)
	}
	return out, nil
}

// heldError is 68 hold.held: ext is held, its version does not change.
func heldError(ext, installed, found string) *cerr.Error {
	ce := cerr.New(cerr.ExitNotApplicable, "hold.held",
		"%s закреплено (cream hold) на %s — версия не меняется; снять: cream unhold %s", ext, installed, ext)
	ce.Details = map[string]any{"ext": ext, "installed": installed}
	if found != "" {
		ce.Details["found"] = found
	}
	return ce
}
