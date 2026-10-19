package resolve

import (
	"slices"
	"strings"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/semver"
	"github.com/iamletenkov/cream/internal/state"
)

// Layer modes: exec — the install hook runs in the guest (build phase);
// none — targets.build: none, not installed at build; live — a live install.
const (
	ModeExec = "exec"
	ModeNone = "none"
	ModeLive = "live"
)

// revLen is the length of the revision in a lineage token (§12.4).
const revLen = 12

// Layer is one requested extension in the answer of cream resolve --json
// (§12.4); the same object is record.json of the build phase (Р6).
// SourceResolved is the registry/lock form ("sha256:<hex>", "git:<hex>").
// Deps are "name@version" of its depends targets and requires providers.
// Pinned: the source revision is immutable (archive sha256 or git commit,
// not a path tree). SecretParams are the names of secret parameters: their
// values enter neither the layer nor params_digest.
type Layer struct {
	Name           string   `json:"name"`
	Version        string   `json:"version"`
	Source         string   `json:"source"`
	SourceResolved string   `json:"source_resolved"`
	CatalogVersion string   `json:"catalog_version"`
	ExtDigest      string   `json:"ext_digest"`
	ParamsDigest   string   `json:"params_digest"`
	Deps           []string `json:"deps"`
	Mode           string   `json:"mode"`
	Token          string   `json:"token"`
	Pinned         bool     `json:"pinned"`
	SecretParams   []string `json:"secret_params"`
}

// checkPlatform runs steps 2–3 of §9.4 for m and returns the layer mode.
func checkPlatform(m *manifest.Manifest, h Host, w *cerr.Warnings) (string, error) {
	na := func(code string, details map[string]any, format string, a ...any) (string, error) {
		return "", graphErr(cerr.ExitNotApplicable, code, m.Name, details, format, a...)
	}
	p := m.Platform
	if (h.Family != "" || h.Detected) && h.Family != p.Family {
		return na("platform.family", map[string]any{"want": p.Family, "found": h.Family},
			"%s: семейство %s, а здесь %s", m.Name, p.Family, h.Family)
	}
	target := canonArch(h.TargetArch)
	if target != "" && !slices.Contains(p.Arch, target) {
		return na("platform.arch", map[string]any{"want": p.Arch, "found": target},
			"%s: архитектуры %s, а цель %s", m.Name, strings.Join(p.Arch, ", "), target)
	}
	if err := checkOS(m, h, w); err != nil {
		return "", err
	}
	if h.HasCommand != nil {
		var missing []string
		for _, c := range p.Commands {
			if !h.HasCommand(c) {
				missing = append(missing, c)
			}
		}
		if len(missing) > 0 {
			return na("platform.commands", map[string]any{"missing": missing},
				"%s: нет команд в PATH: %s", m.Name, strings.Join(missing, ", "))
		}
	}
	if h.Phase == PhaseLive {
		switch m.Targets.Live {
		case manifest.LiveNo:
			return na("target.live", map[string]any{"note": m.Targets.LiveNote},
				"%s не ставится на живую машину: %s", m.Name, m.Targets.LiveNote)
		case manifest.LiveConfirm:
			if !h.AcceptRisk {
				return na("target.confirm", map[string]any{"note": m.Targets.LiveNote},
					"%s на живой машине — только с --accept-risk: %s", m.Name, m.Targets.LiveNote)
			}
		}
		return ModeLive, nil
	}
	if m.Targets.Build == manifest.BuildNone {
		return ModeNone, nil
	}
	if host := canonArch(h.HostArch); target != "" && host != "" && target != host {
		return na("platform.arch_barrier", map[string]any{"target_arch": target, "host_arch": host},
			"слой %s (exec) собирается только на хосте архитектуры цели: цель %s, хост %s", m.Name, target, host)
	}
	return ModeExec, nil
}

func checkOS(m *manifest.Manifest, h Host, w *cerr.Warnings) error {
	reqs := m.Platform.OS
	if len(reqs) == 0 {
		return nil
	}
	if h.OSID == "" {
		w.Add("platform.os_unknown", "%s: ОС не определена (нет os-release), platform.os не проверена", m.Name)
		return nil
	}
	want := make([]string, 0, len(reqs))
	for _, r := range reqs {
		want = append(want, strings.TrimSpace(r.ID+" "+rangeString(r.Version)))
		if r.ID != h.OSID {
			continue
		}
		if r.Version == nil {
			return nil
		}
		if v, err := semver.ParseLoose(h.OSVersion); err == nil && r.Version.Contains(v) {
			return nil
		}
	}
	found := strings.TrimSpace(h.OSID + " " + h.OSVersion)
	return graphErr(cerr.ExitNotApplicable, "platform.os", m.Name, map[string]any{"want": want, "found": found},
		"%s: ОС %s, а здесь %s", m.Name, strings.Join(want, " или "), found)
}

func newLayer(g *gnode, set map[string]*gnode, params map[string]string) Layer {
	m := g.Manifest
	secret := func(k string) bool {
		sp, ok := m.Config.Lookup(k)
		return ok && sp.Secret != ""
	}
	secrets := []string{}
	for k := range params {
		if secret(k) {
			secrets = append(secrets, k)
		}
	}
	slices.Sort(secrets)
	src := g.Source
	return Layer{
		Name:           m.Name,
		Version:        m.Version.String(),
		Source:         src.Name,
		SourceResolved: src.Resolved,
		CatalogVersion: src.CatalogVersion,
		ExtDigest:      g.Digest,
		ParamsDigest:   state.ParamsDigest(params, secret),
		Deps:           layerDeps(g, set),
		Mode:           g.mode,
		Token:          token(src, m),
		Pinned:         src.Type == "archive" || src.Type == "git",
		SecretParams:   secrets,
	}
}

func layerDeps(g *gnode, set map[string]*gnode) []string {
	out := []string{}
	add := func(t *gnode) {
		if r := t.ref(); t != g && !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	for _, d := range g.Manifest.Depends {
		if t, ok := set[d.Name]; ok {
			add(t)
		}
	}
	for _, c := range g.Manifest.Requires {
		for _, t := range providers(set, c, g.name()) {
			add(t)
		}
	}
	slices.Sort(out)
	return out
}

// token is <source>/<name>@<version>#<rev12> (§12.4); rev12 — the first 12
// hex digits of the source revision after its "sha256:"/"git:" prefix. A
// layer without a source (--from-dir) is <name>@<version>.
func token(src state.SourceRef, m *manifest.Manifest) string {
	t := m.Name + "@" + m.Version.String()
	if src.Name != "" {
		t = src.Name + "/" + t
	}
	rev := src.Resolved
	if i := strings.IndexByte(rev, ':'); i >= 0 {
		rev = rev[i+1:]
	}
	if len(rev) > revLen {
		rev = rev[:revLen]
	}
	if rev != "" {
		t += "#" + rev
	}
	return t
}
