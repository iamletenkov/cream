package engine

import (
	"slices"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/config"
	"github.com/iamletenkov/cream/internal/keytype"
	"github.com/iamletenkov/cream/internal/manifest"
)

// Installed returns every registry record, sorted by name, with the manifest
// of its tree. A broken record is skipped with a state.corrupt warning
// (ruling Р20); a record whose tree cannot be read keeps a nil Manifest and
// gets a state.tree warning.
func (e *Engine) Installed() ([]Installed, error) {
	recs, err := e.Store.List(e.Warn)
	if err != nil {
		return nil, err
	}
	out := make([]Installed, 0, len(recs))
	for _, r := range recs {
		m, err := e.loadTree(r.Name)
		if err != nil {
			e.warn("state.tree", "%s: дерево расширения не читается: %v", r.Name, err)
		}
		out = append(out, Installed{Record: r, Manifest: m})
	}
	return out, nil
}

// Find returns the installed extension ext: 64 usage.ext_name for a name that
// cannot be an extension, 66 not_found.extension when it is not installed,
// the manifest error when its tree cannot be read.
func (e *Engine) Find(ext string) (Installed, error) {
	if err := CheckExtName(ext); err != nil {
		return Installed{}, err
	}
	rec, ok, err := e.Store.Get(ext)
	if err != nil {
		return Installed{}, err
	}
	if !ok {
		ce := cerr.New(cerr.ExitNotFound, "not_found.extension", "расширение %s не установлено", ext)
		ce.Details = map[string]any{"ext": ext}
		return Installed{}, ce
	}
	m, err := e.loadTree(ext)
	if err != nil {
		return Installed{}, err
	}
	return Installed{Record: *rec, Manifest: m}, nil
}

// CheckExtName rejects (64 usage.ext_name) a name that is not a valid
// extension name (§4.2): such a name never reaches a path.
func CheckExtName(ext string) error {
	if !manifest.ValidName(ext) {
		ce := cerr.New(cerr.ExitUsage, "usage.ext_name", "недопустимое имя расширения %q", ext)
		ce.Details = map[string]any{"ext": ext}
		return ce
	}
	return nil
}

// loadTree parses the manifest of the installed tree. It was validated at
// install time, so platform.dev_build is not repeated here.
func (e *Engine) loadTree(ext string) (*manifest.Manifest, error) {
	m, err := manifest.Load(e.L.ExtTree(ext), e.Cream, nil)
	if err != nil {
		return nil, err
	}
	if m.Name != ext {
		return nil, cerr.New(cerr.ExitState, "state.tree", "%s: в дереве манифест расширения %s", ext, m.Name)
	}
	return m, nil
}

// Domain returns the settings domain of an installed extension.
func (e *Engine) Domain(in Installed) *config.Domain {
	return config.Open(e.L, in.Record.Name, in.Manifest.Config)
}

// Apply modes of `cream list` and SetResult.Apply.
const (
	ApplyNone    = "none"    // nothing to apply or nothing applies it
	ApplySkipped = "skipped" // --no-apply
	ApplyHook    = "hook"    // hooks/apply ran
	ApplyRestart = "restart" // cream-configure@<ext> restarted synchronously
	ApplyQueued  = "queued"  // restart queued with --no-block
)

// ListEntry is one row of `cream list` (§8.3).
type ListEntry struct {
	Name           string   `json:"name"`
	Version        string   `json:"version"`
	Kind           string   `json:"kind"`
	Status         string   `json:"status"`
	Domain         bool     `json:"domain"`
	Apply          string   `json:"apply"`
	Secret         bool     `json:"secret"`
	Actions        []string `json:"actions"`
	RebootRequired bool     `json:"reboot_required"`
	// Held is cream hold: upgrade --all skips it.
	Held bool `json:"held"`
	// Configured is the configure mark (§10.7); nil without a configure hook.
	Configured *ConfiguredInfo `json:"configured"`
}

// List is `cream list`: the installed extensions with what their domain offers.
func (e *Engine) List() ([]ListEntry, error) {
	all, err := e.Installed()
	if err != nil {
		return nil, err
	}
	out := make([]ListEntry, 0, len(all))
	for _, in := range all {
		le := ListEntry{Name: in.Record.Name, Version: in.Record.Version, Kind: in.Record.Kind,
			Status: in.Record.Status, Apply: ApplyNone, Actions: []string{},
			RebootRequired: in.Record.RebootRequired, Held: in.Record.Held}
		if m := in.Manifest; m != nil {
			le.Domain = len(m.Config) > 0
			le.Apply = applyMode(m, e.L.OpenWrt())
			for _, sp := range m.Config {
				if sp.Secret != keytype.SecretNone {
					le.Secret = true
				}
			}
			for name := range m.Actions {
				le.Actions = append(le.Actions, name)
			}
			slices.Sort(le.Actions)
			le.Configured = e.configuredInfo(in)
		}
		out = append(out, le)
	}
	return out, nil
}

// applyMode is how cream set applies changes of m (§8.4): its apply hook,
// a restart of configure, or nothing. On OpenWrt there is no unit to
// restart: the configure hook runs directly (ADR-0005).
func applyMode(m *manifest.Manifest, openwrt bool) string {
	if _, ok := m.Hooks[manifest.HookApply]; ok {
		return ApplyHook
	}
	if _, ok := m.Hooks[manifest.HookConfigure]; ok {
		if openwrt {
			return ApplyHook
		}
		return ApplyRestart
	}
	return ApplyNone
}

// ShowEntry is one key of `cream show` (§8.3); a secret value is a mask.
type ShowEntry struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Origin  string `json:"origin"`
	Type    string `json:"type"`
	Secret  bool   `json:"secret"`
	Invalid bool   `json:"invalid"`
}

// Show is `cream show <ext>`.
func (e *Engine) Show(ext string) ([]ShowEntry, error) {
	in, err := e.Find(ext)
	if err != nil {
		return nil, err
	}
	rows, err := e.Domain(in).Show()
	if err != nil {
		return nil, err
	}
	out := make([]ShowEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, ShowEntry{Key: r.Key, Value: r.Value, Origin: r.Origin, Type: r.Type,
			Secret: r.Secret, Invalid: r.Invalid})
	}
	return out, nil
}

// Get is `cream get <ext> KEY`: a secret only with secret == true; the root
// check is the caller's (cli, ruling Р9).
func (e *Engine) Get(ext, key string, secret bool) (string, error) {
	in, err := e.Find(ext)
	if err != nil {
		return "", err
	}
	return e.Domain(in).Get(key, secret)
}

// ExportShell is `cream config export <ext> --shell`.
func (e *Engine) ExportShell(ext string) (string, error) {
	in, err := e.Find(ext)
	if err != nil {
		return "", err
	}
	return e.Domain(in).ExportShell()
}
