package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"runtime"
	"strings"
	"text/tabwriter"

	"github.com/iamletenkov/cream/internal/catalog"
	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/lock"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/source"
	"github.com/iamletenkov/cream/internal/version"
)

// catalogDeps is what the source and catalog commands take from outside the
// invocation: the cream build that manifests' `cream:` is checked against,
// the downloader of archive sources and the machine architecture. Tests
// substitute them (a release build, file:// only).
type catalogDeps struct {
	cream manifest.CreamBuild
	fetch source.Fetcher
	arch  string
}

func defaultCatalogDeps() catalogDeps {
	return catalogDeps{cream: manifest.ParseCreamBuild(version.Version), fetch: source.NewHTTPFetcher(nil),
		arch: runtime.GOARCH}
}

// sourceCommands is `cream source add|remove|list|show|sync` (§9.1, §9.2).
func (App) sourceCommands() []Command { return sourceCommandsWith(defaultCatalogDeps()) }

// sourceFlags are the flags of every `source` subcommand; a subcommand other
// than add refuses add's flags (64 usage.flag).
type sourceFlags struct {
	archive, sha256, path string
	replace, sync         bool
}

func sourceCommandsWith(d catalogDeps) []Command {
	var f sourceFlags
	return []Command{{
		Name: "source",
		Usage: "source add|remove|list|show|sync …  — источники каталогов (§9)\n" +
			"  source add <name> --archive <https://…|file:///…> --sha256 <hex> [--replace] [--sync]\n" +
			"  source add <name> --path DIR [--replace] [--sync]\n" +
			"  source remove <name>\n" +
			"  source list\n" +
			"  source show <name>\n" +
			"  source sync [<name>…]",
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&f.archive, "archive", "", "add: архив каталога `URL` (https:// или file:///)")
			fs.StringVar(&f.sha256, "sha256", "", "add: sha256 архива `HEX` (обязателен с --archive)")
			fs.StringVar(&f.path, "path", "", "add: каталог `DIR` (абсолютный путь)")
			fs.BoolVar(&f.replace, "replace", false, "add: заменить источник с другими полями")
			fs.BoolVar(&f.sync, "sync", false, "add: сразу синхронизировать; при сбое запись не сохраняется")
			reserveFlags(fs, "git", "ref", "commit")
		},
		Run: func(ctx *Context, args []string) (any, error) { return runSource(ctx, d, f, args) },
	}}
}

func runSource(ctx *Context, d catalogDeps, f sourceFlags, args []string) (any, error) {
	if len(args) == 0 {
		return nil, ctx.Usagef("source: нужна подкоманда add, remove, list, show или sync")
	}
	sub, rest := args[0], args[1:]
	if sub != "add" {
		set := []struct {
			name string
			on   bool
		}{{"archive", f.archive != ""}, {"sha256", f.sha256 != ""}, {"path", f.path != ""},
			{"replace", f.replace}, {"sync", f.sync}}
		for _, s := range set {
			if s.on {
				return nil, cerr.New(cerr.ExitUsage, "usage.flag", "source %s: флаг --%s только у source add", sub, s.name)
			}
		}
	}
	switch sub {
	case "add":
		return sourceAdd(ctx, d, f, rest)
	case "remove":
		return sourceRemove(ctx, rest)
	case "list":
		return sourceList(ctx, rest)
	case "show":
		return sourceShow(ctx, rest)
	case "sync":
		return sourceSync(ctx, d, rest)
	case "pin":
		return nil, reservedError("source pin")
	}
	return nil, ctx.Usagef("source: неизвестная подкоманда %q (add, remove, list, show, sync)", sub)
}

// sourcePrivate reports sources that are the caller's own, not the
// machine's: XDG user mode or both sources.yaml and the cache overridden
// (bisquite: --sources … --cache …). They need neither root nor the
// machine's lock/global: they are serialized by <cache>/.lock (Р24).
func sourcePrivate(ctx *Context) bool {
	l := ctx.Layout
	return l.User || (l.SourcesFile != "" && l.CacheDir != "")
}

// sourceLock serializes a change of sources (add, remove, sync): the
// machine's — root and lock/global; private ones — the flock of their cache
// (source.LockCache, Р24). source.Sync and source.Forget do not lock: the
// caller serializes.
func sourceLock(ctx *Context, what string) (release func(), err error) {
	if sourcePrivate(ctx) {
		return source.LockCache(ctx.Ctx, ctx.Layout, ctx.Globals.LockTimeout)
	}
	if err := requireRoot(ctx, what); err != nil {
		return nil, err
	}
	h, err := lock.NewLocker(ctx.Layout, ctx.Globals.LockTimeout, ctx.App.Env).Global(ctx.Ctx)
	if err != nil {
		return nil, err
	}
	return h.Release, nil
}

// sourceLoad reads sources.yaml; on the real root as root the file must be
// root-owned and not writable by others (§13).
func sourceLoad(ctx *Context) (*source.File, error) {
	return source.LoadFile(ctx.Layout.Sources(), ctx.Layout.NeedsRoot() && ctx.App.Euid() == 0)
}

// sourceSyncer is the syncer of this invocation: the catalog is validated
// against d.cream, its warnings (catalog.cream_newer, Р22) go to the envelope.
func sourceSyncer(ctx *Context, d catalogDeps) *source.Syncer {
	return &source.Syncer{Layout: ctx.Layout, Fetch: d.fetch, Now: ctx.App.Now,
		Validate: func(dir string) (string, error) {
			r, err := catalog.Validate(dir, d.cream)
			for _, w := range r.Warnings {
				ctx.Warn.AddDetails(w.Code, w.Details, "%s", w.Message)
			}
			return r.CatalogVersion, err
		}}
}

// sourceView is one source in the results of source add/list/show: the
// sources.yaml entry, whether its cache exists and its sources.lock entry.
type sourceView struct {
	Name   string            `json:"name"`
	Type   string            `json:"type"`
	URL    string            `json:"url,omitempty"`
	SHA256 string            `json:"sha256,omitempty"`
	Path   string            `json:"path,omitempty"`
	Synced bool              `json:"synced"`
	Lock   *source.LockEntry `json:"lock"`
}

func newSourceView(d source.SourceDir) sourceView {
	s := d.Source
	return sourceView{Name: s.Name, Type: s.Type, URL: s.URL, SHA256: s.SHA256, Path: s.Path, Synced: d.Synced, Lock: d.Lock}
}

func (v sourceView) origin() string {
	if v.Type == source.TypePath {
		return v.Path
	}
	return v.URL
}

// sourceViews returns the sources of file with their cache state.
func sourceViews(ctx *Context, file *source.File) ([]sourceView, error) {
	lockEntries, err := source.ReadLock(ctx.Layout.SourcesLock())
	if err != nil {
		return nil, err
	}
	dirs, err := source.Dirs(ctx.Layout, file, lockEntries)
	if err != nil {
		return nil, err
	}
	out := make([]sourceView, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, newSourceView(d))
	}
	return out, nil
}

// sourceAddResult is the result of `cream source add` (golden: source-add-*.json).
type sourceAddResult struct {
	Source  sourceView `json:"source"`
	Changed bool       `json:"changed"`
}

func (r sourceAddResult) WriteText(w io.Writer) error {
	state := "без изменений"
	if r.Changed {
		state = "записан"
	}
	if r.Source.Synced {
		state += ", синхронизирован"
	}
	_, err := fmt.Fprintf(w, "источник %s (%s %s): %s\n", r.Source.Name, r.Source.Type, r.Source.origin(), state)
	return err
}

func sourceAdd(ctx *Context, d catalogDeps, f sourceFlags, args []string) (any, error) {
	if len(args) != 1 {
		return nil, ctx.Usagef("source add: нужно одно имя источника")
	}
	s := source.Source{Name: args[0]}
	switch {
	case f.archive != "" && f.path != "":
		return nil, ctx.Usagef("source add: --archive и --path несовместимы")
	case f.archive != "":
		s.Type, s.URL, s.SHA256 = source.TypeArchive, f.archive, f.sha256
	case f.path != "":
		if f.sha256 != "" {
			return nil, ctx.Usagef("source add: --sha256 только с --archive")
		}
		s.Type, s.Path = source.TypePath, f.path
	default:
		return nil, ctx.Usagef("source add: нужен тип источника: --archive URL --sha256 HEX или --path DIR")
	}
	release, err := sourceLock(ctx, "source add")
	if err != nil {
		return nil, err
	}
	defer release()
	file, err := sourceLoad(ctx)
	if err != nil {
		return nil, err
	}
	changed, err := file.Add(s, f.replace)
	if err != nil {
		return nil, err
	}
	s, _ = file.Get(s.Name)
	if f.sync {
		// Синхронизация до записи: при её сбое sources.yaml не меняется (§9.1).
		if _, err := sourceSyncer(ctx, d).Sync(ctx.Ctx, s); err != nil {
			return nil, err
		}
	}
	if changed {
		if err := file.Save(ctx.Layout.Sources()); err != nil {
			return nil, err
		}
	}
	views, err := sourceViews(ctx, &source.File{Sources: []source.Source{s}})
	if err != nil {
		return nil, err
	}
	return sourceAddResult{Source: views[0], Changed: changed}, nil
}

// sourceRemoveResult is the result of `cream source remove` (golden: source-remove.json).
type sourceRemoveResult struct {
	Name    string `json:"name"`
	Removed bool   `json:"removed"`
}

func (r sourceRemoveResult) WriteText(w io.Writer) error {
	_, err := fmt.Fprintf(w, "источник %s удалён вместе с кешем\n", r.Name)
	return err
}

// sourceRemove drops the entry from sources.yaml, then its cache and
// sources.lock entry.
func sourceRemove(ctx *Context, args []string) (any, error) {
	if len(args) != 1 {
		return nil, ctx.Usagef("source remove: нужно одно имя источника")
	}
	release, err := sourceLock(ctx, "source remove")
	if err != nil {
		return nil, err
	}
	defer release()
	file, err := sourceLoad(ctx)
	if err != nil {
		return nil, err
	}
	if err := file.Remove(args[0]); err != nil {
		return nil, err
	}
	if err := file.Save(ctx.Layout.Sources()); err != nil {
		return nil, err
	}
	if err := source.Forget(ctx.Layout, args[0]); err != nil {
		return nil, err
	}
	return sourceRemoveResult{Name: args[0], Removed: true}, nil
}

// sourceListResult is the result of `cream source list` (golden: source-list.json).
type sourceListResult struct {
	Sources []sourceView `json:"sources"`
}

func (r sourceListResult) WriteText(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, s := range r.Sources {
		state := "не синхронизирован"
		if s.Synced {
			state = "синхронизирован"
		}
		if s.Lock != nil {
			state += " " + s.Lock.SyncedAt.Format("2006-01-02T15:04:05Z07:00")
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s.Name, s.Type, s.origin(), state)
	}
	return tw.Flush()
}

func sourceList(ctx *Context, args []string) (any, error) {
	if len(args) != 0 {
		return nil, ctx.Usagef("source list: лишние аргументы")
	}
	file, err := sourceLoad(ctx)
	if err != nil {
		return nil, err
	}
	views, err := sourceViews(ctx, file)
	if err != nil {
		return nil, err
	}
	return sourceListResult{Sources: views}, nil
}

// sourceShowResult is the result of `cream source show` (golden: source-show.json).
type sourceShowResult struct {
	Source sourceView `json:"source"`
}

func (r sourceShowResult) WriteText(w io.Writer) error {
	s := r.Source
	lines := []string{"name: " + s.Name, "type: " + s.Type}
	if s.URL != "" {
		lines = append(lines, "url: "+s.URL, "sha256: "+s.SHA256)
	}
	if s.Path != "" {
		lines = append(lines, "path: "+s.Path)
	}
	lines = append(lines, fmt.Sprintf("synced: %t", s.Synced))
	if s.Lock != nil {
		lines = append(lines, "resolved: "+s.Lock.Resolved, "catalog_version: "+s.Lock.CatalogVersion,
			"synced_at: "+s.Lock.SyncedAt.Format("2006-01-02T15:04:05Z07:00"))
	}
	_, err := io.WriteString(w, strings.Join(lines, "\n")+"\n")
	return err
}

func sourceShow(ctx *Context, args []string) (any, error) {
	if len(args) != 1 {
		return nil, ctx.Usagef("source show: нужно одно имя источника")
	}
	file, err := sourceLoad(ctx)
	if err != nil {
		return nil, err
	}
	s, ok := file.Get(args[0])
	if !ok {
		return nil, sourceNotFound(args[0])
	}
	views, err := sourceViews(ctx, &source.File{Sources: []source.Source{s}})
	if err != nil {
		return nil, err
	}
	return sourceShowResult{Source: views[0]}, nil
}

func sourceNotFound(name string) error {
	e := cerr.New(cerr.ExitNotFound, "not_found.source", "источника %s нет в sources.yaml", name)
	e.Details = map[string]any{"source": name}
	return e
}

// sourceSyncResult is the result of `cream source sync` (golden: source-sync.json).
type sourceSyncResult struct {
	Synced []source.LockEntry `json:"synced"`
}

func (r sourceSyncResult) WriteText(w io.Writer) error {
	for _, e := range r.Synced {
		if _, err := fmt.Fprintf(w, "%s: %s (каталог %s)\n", e.Name, e.Resolved, e.CatalogVersion); err != nil {
			return err
		}
	}
	return nil
}

// sourceSync syncs the named sources (all without names) in the order of
// sources.yaml. A failed source does not stop the others: they are synced,
// and the error of the first failure is returned with the names of the
// synced and failed sources in its details.
func sourceSync(ctx *Context, d catalogDeps, args []string) (any, error) {
	release, err := sourceLock(ctx, "source sync")
	if err != nil {
		return nil, err
	}
	defer release()
	file, err := sourceLoad(ctx)
	if err != nil {
		return nil, err
	}
	todo := file.Sources
	if len(args) > 0 {
		todo = make([]source.Source, 0, len(args))
		for _, name := range args {
			s, ok := file.Get(name)
			if !ok {
				return nil, sourceNotFound(name)
			}
			todo = append(todo, s)
		}
	}
	syncer := sourceSyncer(ctx, d)
	res := sourceSyncResult{Synced: []source.LockEntry{}}
	var first error
	failed := []string{}
	for _, s := range todo {
		e, err := syncer.Sync(ctx.Ctx, s)
		if err != nil {
			_, _ = fmt.Fprintf(ctx.Stderr(), "cream: источник %s: %v\n", s.Name, err)
			failed = append(failed, s.Name)
			if first == nil {
				first = err
			}
			continue
		}
		res.Synced = append(res.Synced, e)
	}
	if first != nil {
		return nil, sourceSyncFailed(first, res.Synced, failed)
	}
	return res, nil
}

// sourceSyncFailed keeps the exit and code of the first failure and adds the
// names of the synced and the failed sources to its details.
func sourceSyncFailed(first error, synced []source.LockEntry, failed []string) error {
	var ce *cerr.Error
	if !errors.As(first, &ce) {
		ce = cerr.Wrap(first, cerr.ExitInternal, cerr.CodeInternal, "синхронизация источников")
	}
	out := *ce
	out.Details = make(map[string]any, len(ce.Details)+2)
	for k, v := range ce.Details {
		out.Details[k] = v
	}
	names := make([]string, 0, len(synced))
	for _, e := range synced {
		names = append(names, e.Name)
	}
	out.Details["synced"], out.Details["failed"] = names, failed
	return &out
}
