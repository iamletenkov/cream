package engine

import (
	"context"
	"errors"

	"github.com/iamletenkov/cream/internal/catalog"
	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/source"
)

// Upgrade is `cream upgrade <ext>…` or `cream upgrade --all` (§6.5): every
// named installed extension is looked up in the source its registry record
// names (Р8; a source no longer in sources.yaml is 69 source.missing) and
// installed like `cream install` without parameters: a higher version is
// upgraded (hook upgrade, otherwise install with CREAM_PREV_VERSION), the
// same version changes nothing (a failed one is repeated), a lower one is
// 65 version.downgrade. With --all an extension without a source
// (installed --from-dir) is skipped with an upgrade.no_source warning.
func (e *Engine) Upgrade(ctx context.Context, names []string, all bool) ([]InstallResult, error) {
	switch {
	case all && len(names) > 0:
		return nil, cerr.New(cerr.ExitUsage, "usage.args", "upgrade: имена расширений и --all несовместимы")
	case !all && len(names) == 0:
		return nil, cerr.New(cerr.ExitUsage, "usage.args", "upgrade: нужны имена расширений или --all")
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
	installed, err := e.Installed()
	if err != nil {
		return nil, err
	}
	var targets []Installed
	if all {
		targets = installed
	} else {
		for _, n := range names {
			in, ok := upgradeFind(installed, n)
			if !ok {
				ce := cerr.New(cerr.ExitNotFound, "not_found.extension", "расширение %s не установлено", n)
				ce.Details = map[string]any{"ext": n}
				return nil, ce
			}
			targets = append(targets, in)
		}
	}
	dirs, err := e.installSourceDirs()
	if err != nil {
		return nil, err
	}
	entries := make([]catalog.Entry, 0, len(targets))
	for _, in := range targets {
		name, src := in.Record.Name, in.Record.Source.Name
		d, ok := upgradeSource(dirs, src)
		if !ok {
			if all && src == "" {
				e.warn("upgrade.no_source", "%s: поставлено не из источника — пропущено", name)
				continue
			}
			ce := cerr.New(cerr.ExitSource, "source.missing",
				"%s: источника %q из реестра нет в sources.yaml — обновлять неоткуда", name, src)
			ce.Details = map[string]any{"ext": name, "source": src}
			return nil, ce
		}
		en, err := catalog.Lookup([]source.SourceDir{d}, catalog.Ref{Source: src, Name: name}, e.Cream, e.Warn)
		if err != nil {
			var ce *cerr.Error
			if errors.As(err, &ce) && ce.Code == "not_found.extension" {
				ce.Message += " (источник " + src + ")"
			}
			return nil, err
		}
		entries = append(entries, en)
	}
	if len(entries) == 0 {
		return []InstallResult{}, nil
	}
	return e.installEntries(ctx, entries, nil, installed, dirs, installOpts{})
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
