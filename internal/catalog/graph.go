package catalog

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/manifest"
)

// Коды проверки графа каталога (cream 1.1.0, `cream validate --catalog`).
const (
	codeDepends      = "catalog.depends"       // 65: depends на имя, которого нет, или вне диапазона
	codeDependsCycle = "catalog.depends_cycle" // 65: цикл depends внутри каталога
	warnRequires     = "catalog.requires"      // requires без поставщика в каталоге
	warnConflicts    = "catalog.conflicts"     // conflicts.extensions на имя не из каталога
)

// ValidateCatalog is Validate plus the static graph of the catalog (cream
// 1.1.0, `cream validate --catalog`): a depends on a name the catalog lacks or
// outside the range of its version, and a depends cycle are errors (65); a
// requires no extension of the catalog provides (it may come from cream assume
// or a bare INSTALL) and a conflicts.extensions on a foreign name are
// warnings. An extension skipped as needing a newer cream (Р22) counts as
// present. Sync and catalog pack keep Validate: the graph is a check for
// catalog authors, not a reason to refuse a published catalog.
func ValidateCatalog(root string, cream manifest.CreamBuild) (Report, error) {
	r, exts := validate(root, cream)
	checkGraph(&r, exts)
	return r, r.Err()
}

func checkGraph(r *Report, exts []loaded) {
	idx := map[string]loaded{}
	for _, x := range exts {
		idx[x.m.Name] = x
	}
	skipped := map[string]bool{}
	for _, p := range r.NotApplicable {
		skipped[p.Ext] = true
	}
	where := func(x loaded) string { return path.Join(x.rel, manifest.ManifestFile) }
	for _, x := range exts {
		m := x.m
		for _, d := range m.Depends {
			t, ok := idx[d.Name]
			switch {
			case !ok && skipped[d.Name]:
			case !ok:
				r.fail(m.Name, where(x), codeDepends, "%s: depends на %s — такого расширения в каталоге нет", m.Name, d.Name)
			case d.Version != nil && !d.Version.Contains(t.m.Version):
				r.fail(m.Name, where(x), codeDepends, "%s: depends на %s %s, а в каталоге %s", m.Name, d.Name,
					d.Version.String(), t.m.Version.String())
			}
		}
		for _, c := range m.Requires {
			if !slices.ContainsFunc(exts, func(o loaded) bool { return o.m.Name != m.Name && slices.Contains(o.m.Provides, c) }) {
				r.warnExt(warnRequires, m.Name, "%s: requires %s — в каталоге её никто не даёт (нужен cream assume или поставщик из другого источника)", m.Name, c)
			}
		}
		for _, other := range m.Conflicts.Extensions {
			if _, ok := idx[other]; !ok && !skipped[other] {
				r.warnExt(warnConflicts, m.Name, "%s: conflicts.extensions %s — такого расширения в каталоге нет", m.Name, other)
			}
		}
	}
	for _, cyc := range dependsCycles(exts, idx) {
		x := idx[cyc[0]]
		r.fail(x.m.Name, where(x), codeDependsCycle, "цикл depends: %s", strings.Join(cyc, " → "))
	}
}

// dependsCycles finds each cycle of depends once, starting from its member
// that comes first in exts; the cycle is closed by its first name.
func dependsCycles(exts []loaded, idx map[string]loaded) [][]string {
	const (
		white = iota
		grey
		black
	)
	color := map[string]int{}
	var out [][]string
	var stack []string
	var visit func(name string)
	visit = func(name string) {
		color[name] = grey
		stack = append(stack, name)
		for _, d := range idx[name].m.Depends {
			if _, ok := idx[d.Name]; !ok {
				continue
			}
			switch color[d.Name] {
			case white:
				visit(d.Name)
			case grey:
				i := slices.Index(stack, d.Name)
				cyc := append(slices.Clone(stack[i:]), d.Name)
				out = append(out, cyc)
			}
		}
		stack = stack[:len(stack)-1]
		color[name] = black
	}
	for _, x := range exts {
		if color[x.m.Name] == white {
			visit(x.m.Name)
		}
	}
	return out
}

// warnExt records a warning about one extension (details.ext).
func (r *Report) warnExt(code, ext, format string, a ...any) {
	r.Warnings = append(r.Warnings, cerr.Warning{Code: code, Message: fmt.Sprintf(format, a...),
		Details: map[string]any{"ext": ext}})
}
