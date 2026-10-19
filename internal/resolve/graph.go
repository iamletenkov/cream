package resolve

import (
	"fmt"
	"slices"
	"strings"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/semver"
	"github.com/iamletenkov/cream/internal/state"
)

// Node is one extension of the graph: an installed one (Installed, Files and
// Source from the registry) or a requested one (Source and Digest from the
// catalog entry). Files are the dst paths it owns; nil means "those of the
// manifest".
type Node struct {
	Manifest  *manifest.Manifest
	Installed bool
	Files     []string
	Source    state.SourceRef
	Digest    string
}

// Missing is a dependency by name that the graph lacks (details of 67
// deps.missing); Version is the range, "" for any version.
type Missing struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// gnode is a member of the graph "installed ∪ requested".
type gnode struct {
	Node
	requested bool
	mode      string
}

func (g *gnode) name() string { return g.Manifest.Name }

func (g *gnode) ref() string { return g.Manifest.Name + "@" + g.Manifest.Version.String() }

func (g *gnode) provides(capability string) bool {
	return slices.Contains(g.Manifest.Provides, capability)
}

func (g *gnode) files() []string {
	if g.Files != nil {
		return g.Files
	}
	out := make([]string, 0, len(g.Manifest.Files))
	for _, f := range g.Manifest.Files {
		out = append(out, f.Dst)
	}
	return out
}

// Resolve checks requested against the host and the graph "installed ∪
// requested" (§9.4) and returns their layers in install order: a provider
// (depends target, requires provider) before its consumer, ties by name.
// A requested extension replaces the installed one of the same name. In the
// build phase a layer with targets.build: none has mode "none": it is not
// installed, so it neither provides for others nor has its graph checked.
// Only constraints that touch a requested extension are checked, so a
// defect among installed ones does not block an unrelated install.
// params are the install parameters per extension (for params_digest).
//
// Errors: 64 usage.phase, usage.duplicate; 68 platform.*, target.*; 67
// deps.missing, deps.version, deps.requires, deps.cycle, conflict.extension,
// conflict.capability, conflict.file. Each carries details.ext.
func Resolve(installed, requested []Node, params map[string]map[string]string, h Host, w *cerr.Warnings) ([]Layer, error) {
	if w == nil {
		w = &cerr.Warnings{}
	}
	if err := checkPhase(h.Phase); err != nil {
		return nil, err
	}
	req := make([]*gnode, 0, len(requested))
	seen := map[string]bool{}
	for _, n := range requested {
		name := n.Manifest.Name
		if seen[name] {
			return nil, graphErr(cerr.ExitUsage, "usage.duplicate", name, nil, "%s запрошено дважды", name)
		}
		seen[name] = true
		mode, err := checkPlatform(n.Manifest, h, w)
		if err != nil {
			return nil, err
		}
		req = append(req, &gnode{Node: n, requested: true, mode: mode})
	}
	before := map[string]*gnode{}
	for _, n := range installed {
		before[n.Manifest.Name] = &gnode{Node: n, mode: ModeLive}
	}
	set := make(map[string]*gnode, len(before)+len(req))
	for k, v := range before {
		set[k] = v
	}
	for _, g := range req {
		if g.mode != ModeNone {
			set[g.name()] = g
		}
	}
	if err := checkGraph(set, before); err != nil {
		return nil, err
	}
	order, err := topoSort(req, set)
	if err != nil {
		return nil, err
	}
	layers := make([]Layer, 0, len(order))
	for _, g := range order {
		layers = append(layers, newLayer(g, set, params[g.name()]))
	}
	return layers, nil
}

func graphErr(exit int, code, ext string, details map[string]any, format string, a ...any) *cerr.Error {
	e := cerr.New(exit, code, format, a...)
	if details == nil {
		details = map[string]any{}
	}
	details["ext"] = ext
	e.Details = details
	return e
}

func sortedNames(set map[string]*gnode) []string {
	names := make([]string, 0, len(set))
	for k := range set {
		names = append(names, k)
	}
	slices.Sort(names)
	return names
}

// providers returns the members of set other than self that provide capability.
func providers(set map[string]*gnode, capability, self string) []*gnode {
	var out []*gnode
	for _, name := range sortedNames(set) {
		if g := set[name]; name != self && g.provides(capability) {
			out = append(out, g)
		}
	}
	return out
}

func checkGraph(set, before map[string]*gnode) error {
	names := sortedNames(set)
	for _, name := range names {
		g := set[name]
		if err := checkDepends(g, set); err != nil {
			return err
		}
		if err := checkRequires(g, set, before); err != nil {
			return err
		}
		if err := checkConflicts(g, set); err != nil {
			return err
		}
	}
	return checkFiles(set, names)
}

func checkDepends(g *gnode, set map[string]*gnode) error {
	var missing []Missing
	for _, d := range g.Manifest.Depends {
		t, ok := set[d.Name]
		if !ok || t == g {
			if g.requested {
				missing = append(missing, Missing{Name: d.Name, Version: rangeString(d.Version)})
			}
			continue
		}
		if !g.requested && !t.requested {
			continue
		}
		if d.Version != nil && !d.Version.Contains(t.Manifest.Version) {
			return graphErr(cerr.ExitGraph, "deps.version", g.name(),
				map[string]any{"dep": d.Name, "want": d.Version.String(), "found": t.Manifest.Version.String()},
				"%s требует %s %s, а будет %s", g.name(), d.Name, d.Version, t.Manifest.Version)
		}
	}
	if len(missing) > 0 {
		parts := make([]string, 0, len(missing))
		for _, m := range missing {
			parts = append(parts, strings.TrimSpace(m.Name+" "+m.Version))
		}
		return graphErr(cerr.ExitGraph, "deps.missing", g.name(), map[string]any{"missing": missing},
			"%s: нет зависимостей %s — поставьте их раньше", g.name(), strings.Join(parts, ", "))
	}
	return nil
}

func rangeString(r *semver.Range) string {
	if r == nil {
		return ""
	}
	return r.String()
}

// checkRequires: a requested extension needs a provider of each capability;
// an installed one fails only when the request takes its last provider away.
func checkRequires(g *gnode, set, before map[string]*gnode) error {
	for _, c := range g.Manifest.Requires {
		if len(providers(set, c, g.name())) > 0 {
			continue
		}
		if !g.requested && len(providers(before, c, g.name())) == 0 {
			continue
		}
		return graphErr(cerr.ExitGraph, "deps.requires", g.name(), map[string]any{"capability": c},
			"%s требует способность %s, а её никто не даёт", g.name(), c)
	}
	return nil
}

func checkConflicts(g *gnode, set map[string]*gnode) error {
	for _, other := range g.Manifest.Conflicts.Extensions {
		o, ok := set[other]
		if !ok || o == g || (!g.requested && !o.requested) {
			continue
		}
		return graphErr(cerr.ExitGraph, "conflict.extension", g.name(), map[string]any{"with": other},
			"%s несовместимо с %s", g.name(), other)
	}
	for _, c := range g.Manifest.Conflicts.Capabilities {
		for _, o := range providers(set, c, g.name()) {
			if !g.requested && !o.requested {
				continue
			}
			return graphErr(cerr.ExitGraph, "conflict.capability", g.name(), map[string]any{"with": o.name(), "capability": c},
				"%s несовместимо с другим поставщиком %s: %s", g.name(), c, o.name())
		}
	}
	return nil
}

func checkFiles(set map[string]*gnode, names []string) error {
	owner := map[string]*gnode{}
	for _, name := range names {
		g := set[name]
		for _, dst := range g.files() {
			o, ok := owner[dst]
			if !ok {
				owner[dst] = g
				continue
			}
			if o == g || (!g.requested && !o.requested) {
				continue
			}
			ext := g.name()
			if !g.requested {
				ext = o.name()
			}
			return graphErr(cerr.ExitGraph, "conflict.file", ext, map[string]any{"dst": dst, "owners": []string{o.name(), g.name()}},
				"%s: файл %s есть и у %s, и у %s", ext, dst, o.name(), g.name())
		}
	}
	return nil
}

// edges returns the requested members g must follow (providers first).
func edges(g *gnode, set map[string]*gnode) []string {
	if g.mode == ModeNone {
		return nil
	}
	var out []string
	add := func(t *gnode) {
		if t != g && t.requested && !slices.Contains(out, t.name()) {
			out = append(out, t.name())
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

// topoSort orders req by Kahn's algorithm, the smallest name first among the
// ready ones; a cycle is 67 deps.cycle with the cycle in details.
func topoSort(req []*gnode, set map[string]*gnode) ([]*gnode, error) {
	byName := map[string]*gnode{}
	for _, g := range req {
		byName[g.name()] = g
	}
	deps := map[string][]string{}
	pending := map[string]int{}
	users := map[string][]string{}
	for _, g := range req {
		deps[g.name()] = edges(g, set)
		pending[g.name()] = len(deps[g.name()])
		for _, p := range deps[g.name()] {
			users[p] = append(users[p], g.name())
		}
	}
	var ready []string
	for name, n := range pending {
		if n == 0 {
			ready = append(ready, name)
		}
	}
	out := make([]*gnode, 0, len(req))
	for len(ready) > 0 {
		slices.Sort(ready)
		name := ready[0]
		ready = ready[1:]
		out = append(out, byName[name])
		for _, u := range users[name] {
			if pending[u]--; pending[u] == 0 {
				ready = append(ready, u)
			}
		}
	}
	if len(out) == len(req) {
		return out, nil
	}
	cycle := findCycle(deps, pending)
	return nil, graphErr(cerr.ExitGraph, "deps.cycle", cycle[0], map[string]any{"cycle": cycle},
		"цикл зависимостей: %s", strings.Join(cycle, " → "))
}

// findCycle returns a cycle among the members left with pending edges, as
// a path that starts and ends with the same name.
func findCycle(deps map[string][]string, pending map[string]int) []string {
	var left []string
	for name, n := range pending {
		if n > 0 {
			left = append(left, name)
		}
	}
	slices.Sort(left)
	const (
		white = iota
		gray
		black
	)
	color := map[string]int{}
	var stack []string
	var found []string
	var visit func(string) bool
	visit = func(n string) bool {
		color[n] = gray
		stack = append(stack, n)
		for _, p := range deps[n] {
			if pending[p] == 0 {
				continue
			}
			switch color[p] {
			case gray:
				i := slices.Index(stack, p)
				found = append(slices.Clone(stack[i:]), p)
				return true
			case white:
				if visit(p) {
					return true
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[n] = black
		return false
	}
	for _, n := range left {
		if color[n] == white && visit(n) {
			return found
		}
	}
	panic(fmt.Sprintf("resolve: сортировка остановилась без цикла: %v", left))
}

// Dependents returns the installed extensions that need name (sorted, never
// nil): those that depend on it, and those that require a capability of it
// that no other installed extension provides. For remove (67 deps.dependents).
func Dependents(installed []Node, name string) []string {
	set := map[string]*gnode{}
	for _, n := range installed {
		set[n.Manifest.Name] = &gnode{Node: n}
	}
	out := []string{}
	target, ok := set[name]
	if !ok {
		return out
	}
	for _, other := range sortedNames(set) {
		g := set[other]
		if other == name {
			continue
		}
		if slices.ContainsFunc(g.Manifest.Depends, func(d manifest.Dep) bool { return d.Name == name }) {
			out = append(out, other)
			continue
		}
		for _, c := range g.Manifest.Requires {
			if !target.provides(c) {
				continue
			}
			rest := slices.DeleteFunc(providers(set, c, other), func(p *gnode) bool { return p == target })
			if len(rest) == 0 {
				out = append(out, other)
				break
			}
		}
	}
	return out
}
