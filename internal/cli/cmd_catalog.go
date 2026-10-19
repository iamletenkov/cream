package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/iamletenkov/cream/internal/catalog"
	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/resolve"
	"github.com/iamletenkov/cream/internal/source"
	"github.com/iamletenkov/cream/internal/state"
)

// catalogCommands are `cream validate`, `cream catalog pack` and
// `cream resolve` (§11.2, §12.4): offline, from the cache only (R6).
func (App) catalogCommands() []Command { return catalogCommandsWith(defaultCatalogDeps()) }

func catalogCommandsWith(d catalogDeps) []Command {
	var validateCatalog, packOut string
	var h resolveFlags
	return []Command{
		{Name: "validate", Usage: "validate [--catalog DIR] [DIR…]  — проверить каталог и каталоги расширений (§4.1)",
			Flags: func(fs *flag.FlagSet) {
				fs.StringVar(&validateCatalog, "catalog", "", "каталог `DIR` с cream-catalog.yaml")
			},
			Run: func(ctx *Context, args []string) (any, error) { return runValidate(ctx, d, validateCatalog, args) }},
		{Name: "catalog", Usage: "catalog pack DIR --out F  — детерминированный архив каталога (SOURCE_DATE_EPOCH)",
			Flags: func(fs *flag.FlagSet) {
				fs.StringVar(&packOut, "out", "", "pack: файл архива `F`")
			},
			Run: func(ctx *Context, args []string) (any, error) { return runCatalog(ctx, d, packOut, args) }},
		{Name: "resolve",
			Usage: "resolve [--phase build|live] [--target-arch A] [--host-arch A] [--family F] [--installed FILE] <ref> [K=V…]…  — офлайн-разрешение слоёв (§12.4)",
			Flags: func(fs *flag.FlagSet) {
				fs.StringVar(&h.phase, "phase", resolve.PhaseLive, "фаза `P`: build или live")
				fs.StringVar(&h.targetArch, "target-arch", "", "архитектура цели `A` (по умолчанию — этой машины)")
				fs.StringVar(&h.hostArch, "host-arch", "", "архитектура хоста сборки `A` (по умолчанию — этой машины)")
				fs.StringVar(&h.family, "family", "", "семейство пакетов цели `F` (deb); без него не проверяется в build")
				fs.StringVar(&h.installed, "installed", "", "build: `FILE` — JSON-массив расширений базы FROM [{name, version, provides?}] (cream 1.1.0)")
				reserveFlags(fs, "require-pinned")
			},
			Run: func(ctx *Context, args []string) (any, error) { return runResolve(ctx, d, h, args) }},
	}
}

// validateReport is one checked target of `cream validate`: a catalog
// (--catalog) or an extension directory. Warnings go to the envelope.
type validateReport struct {
	Target         string                  `json:"target"`
	Kind           string                  `json:"kind"`
	Catalog        string                  `json:"catalog,omitempty"`
	CatalogVersion string                  `json:"catalog_version,omitempty"`
	Extensions     []catalog.ExtensionInfo `json:"extensions"`
	NotApplicable  []catalog.Problem       `json:"not_applicable"`
	Errors         []catalog.Problem       `json:"errors"`
}

// validateResult is the result of `cream validate` (golden: validate-*.json).
type validateResult struct {
	Reports []validateReport `json:"reports"`
}

func (r validateResult) WriteText(w io.Writer) error {
	for _, rep := range r.Reports {
		var line string
		if rep.Kind == "catalog" {
			line = fmt.Sprintf("%s: каталог %s %s, расширений %d", rep.Target, rep.Catalog, rep.CatalogVersion,
				len(rep.Extensions))
			for i, x := range rep.Extensions {
				sep := " "
				if i == 0 {
					sep = ": "
				}
				line += sep + x.Name + " " + x.Version
			}
			if n := len(rep.NotApplicable); n > 0 {
				line += fmt.Sprintf(", неприменимых этим cream %d", n)
			}
		} else {
			line = rep.Target + ": расширение"
			for _, x := range rep.Extensions {
				line += " " + x.Name + " " + x.Version
			}
		}
		if _, err := fmt.Fprintln(w, line+" — без ошибок"); err != nil {
			return err
		}
	}
	return nil
}

// runValidate checks every target and reports all their problems: on any
// error the exit and code are those of the first problem, details.reports
// has every report, and without --json each problem is a line on stderr.
func runValidate(ctx *Context, d catalogDeps, cat string, dirs []string) (any, error) {
	if cat == "" && len(dirs) == 0 {
		return nil, ctx.Usagef("validate: нужен --catalog DIR и/или каталоги расширений")
	}
	type target struct{ path, kind string }
	var targets []target
	if cat != "" {
		targets = append(targets, target{cat, "catalog"})
	}
	for _, dir := range dirs {
		targets = append(targets, target{dir, "extension"})
	}
	res := validateResult{Reports: make([]validateReport, 0, len(targets))}
	var problems []catalog.Problem
	var targetOf []string
	for _, t := range targets {
		var r catalog.Report
		if t.kind == "catalog" {
			r, _ = catalog.Validate(t.path, d.cream)
		} else {
			r, _ = catalog.ValidateDir(t.path, d.cream)
		}
		for _, w := range r.Warnings {
			ctx.Warn.AddDetails(w.Code, w.Details, "%s", w.Message)
		}
		res.Reports = append(res.Reports, validateReport{Target: t.path, Kind: t.kind, Catalog: r.Catalog,
			CatalogVersion: r.CatalogVersion, Extensions: r.Extensions, NotApplicable: r.NotApplicable, Errors: r.Errors})
		for _, p := range r.Errors {
			problems = append(problems, p)
			targetOf = append(targetOf, t.path)
		}
	}
	if len(problems) == 0 {
		return res, nil
	}
	if !ctx.Globals.JSON {
		for i, p := range problems {
			_, _ = fmt.Fprintf(ctx.Stderr(), "cream: %s: %s: %s (%s)\n", targetOf[i], p.Path, p.Message, p.Code)
		}
	}
	p := problems[0]
	msg := targetOf[0] + ": " + p.Message
	if len(problems) > 1 {
		msg = fmt.Sprintf("%s (и ещё ошибок: %d)", msg, len(problems)-1)
	}
	e := cerr.New(p.Exit, p.Code, "%s", msg)
	e.Details = map[string]any{"reports": res.Reports}
	return nil, e
}

// packResult is the result of `cream catalog pack`.
type packResult struct {
	Out            string `json:"out"`
	SHA256         string `json:"sha256"`
	Size           int64  `json:"size"`
	Catalog        string `json:"catalog"`
	CatalogVersion string `json:"catalog_version"`
	Epoch          int64  `json:"source_date_epoch"`
}

func (r packResult) WriteText(w io.Writer) error {
	_, err := fmt.Fprintf(w, "%s  %s (каталог %s %s, %d байт)\n", r.SHA256, r.Out, r.Catalog, r.CatalogVersion, r.Size)
	return err
}

func runCatalog(ctx *Context, d catalogDeps, out string, args []string) (any, error) {
	if len(args) == 0 {
		return nil, ctx.Usagef("catalog: нужна подкоманда pack")
	}
	if args[0] != "pack" {
		return nil, ctx.Usagef("catalog: неизвестная подкоманда %q (pack)", args[0])
	}
	if len(args) != 2 || out == "" {
		return nil, ctx.Usagef("catalog pack: нужен один каталог и --out F")
	}
	return catalogPack(ctx, d, args[1], out)
}

// catalogPack validates the catalog, then writes its deterministic archive
// to out atomically (a temporary file next to it, then rename): entries'
// mtime is SOURCE_DATE_EPOCH (0 when unset).
func catalogPack(ctx *Context, d catalogDeps, dir, out string) (any, error) {
	epoch, err := packEpoch(ctx.App.Env("SOURCE_DATE_EPOCH"))
	if err != nil {
		return nil, err
	}
	r, err := catalog.Validate(dir, d.cream)
	for _, w := range r.Warnings {
		ctx.Warn.AddDetails(w.Code, w.Details, "%s", w.Message)
	}
	if err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(out), "."+filepath.Base(out)+".*")
	if err != nil {
		return nil, cerr.Wrap(err, cerr.ExitState, cerr.CodeIOWrite, "архив %s", out)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	h := sha256.New()
	cw := &packCounter{}
	err = source.Pack(dir, io.MultiWriter(tmp, h, cw), time.Unix(epoch, 0))
	if err != nil {
		_ = tmp.Close()
		return nil, cerr.Wrap(err, cerr.ExitState, cerr.CodeIOWrite, "упаковка %s", dir)
	}
	for _, step := range []func() error{tmp.Sync, func() error { return tmp.Chmod(0o644) }, tmp.Close,
		func() error { return os.Rename(tmp.Name(), out) }} {
		if err := step(); err != nil {
			_ = tmp.Close()
			return nil, cerr.Wrap(err, cerr.ExitState, cerr.CodeIOWrite, "архив %s", out)
		}
	}
	return packResult{Out: out, SHA256: hex.EncodeToString(h.Sum(nil)), Size: cw.n, Catalog: r.Catalog,
		CatalogVersion: r.CatalogVersion, Epoch: epoch}, nil
}

type packCounter struct{ n int64 }

func (c *packCounter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

// packEpoch parses SOURCE_DATE_EPOCH: seconds since 1970, 0 when unset.
func packEpoch(raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < 0 {
		return 0, cerr.New(cerr.ExitUsage, "usage.source_date_epoch",
			"SOURCE_DATE_EPOCH %q: нужно целое число секунд ≥ 0", raw)
	}
	return v, nil
}

// resolveFlags are the host flags of `cream resolve`.
type resolveFlags struct {
	phase, targetArch, hostArch, family string
	// installed is the --installed file: the extensions of the base image
	// of a build layer (lineage of FROM), installed nodes of the graph.
	installed string
}

// resolveRequest is one <ref> [K=V…] group of the arguments.
type resolveRequest struct {
	ref    catalog.Ref
	params []string
}

// resolveLayer is one layer of `cream resolve --json`: the lineage layer of
// §12.4 plus the extension directory in the source cache.
type resolveLayer struct {
	resolve.Layer
	Dir string `json:"dir"`
}

// resolveResult is the result of `cream resolve` (golden: resolve-*.json):
// the layers in install order.
type resolveResult struct {
	Phase  string         `json:"phase"`
	Layers []resolveLayer `json:"layers"`
}

func (r resolveResult) WriteText(w io.Writer) error {
	for _, l := range r.Layers {
		if _, err := fmt.Fprintf(w, "%s\t%s\n", l.Token, l.Mode); err != nil {
			return err
		}
	}
	return nil
}

// runResolve looks every reference up in the source caches (no network),
// checks its parameters against config.keys and resolves the requested
// layers against the host: build — the host from the flags (an empty
// family is not checked, the guest's OS is unknown) with --installed, if
// given, as installed; live — this machine (os-release under --root) with
// the registry as installed.
// Secret parameters enter neither params_digest nor the output: only
// their names, in secret_params.
func runResolve(ctx *Context, d catalogDeps, f resolveFlags, args []string) (any, error) {
	reqs, err := resolveParseArgs(args)
	if err != nil {
		return nil, err
	}
	host, err := resolveHost(ctx, d, f)
	if err != nil {
		return nil, err
	}
	if f.installed != "" && host.Phase != resolve.PhaseBuild {
		return nil, cerr.New(cerr.ExitUsage, "usage.args",
			"resolve: --installed — только с --phase build (в live установленное берётся из реестра)")
	}
	file, err := sourceLoad(ctx)
	if err != nil {
		return nil, err
	}
	lockEntries, err := source.ReadLock(ctx.Layout.SourcesLock())
	if err != nil {
		return nil, err
	}
	dirs, err := source.Dirs(ctx.Layout, file, lockEntries)
	if err != nil {
		return nil, err
	}
	requested := make([]resolve.Node, 0, len(reqs))
	params := map[string]map[string]string{}
	dirOf := map[string]string{}
	for _, rq := range reqs {
		e, err := catalog.Lookup(dirs, rq.ref, d.cream, ctx.Warn)
		if err != nil {
			return nil, err
		}
		name := e.Manifest.Name
		p, err := resolveParams(e, rq.params)
		if err != nil {
			return nil, err
		}
		params[name], dirOf[name] = p, e.Dir
		requested = append(requested, resolve.Node{Manifest: e.Manifest, Source: resolveSourceRef(e.Source),
			Digest: e.Digest})
	}
	var installed []resolve.Node
	switch {
	case host.Phase == resolve.PhaseLive:
		if installed, err = resolveInstalled(ctx, d); err != nil {
			return nil, err
		}
	case f.installed != "":
		if installed, err = resolve.ReadInstalled(f.installed); err != nil {
			return nil, err
		}
	}
	layers, err := resolve.Resolve(installed, requested, params, host, ctx.Warn)
	if err != nil {
		return nil, err
	}
	res := resolveResult{Phase: host.Phase, Layers: make([]resolveLayer, 0, len(layers))}
	for _, l := range layers {
		res.Layers = append(res.Layers, resolveLayer{Layer: l, Dir: dirOf[l.Name]})
	}
	return res, nil
}

// resolveParseArgs groups the arguments: a reference, then its K=V.
func resolveParseArgs(args []string) ([]resolveRequest, error) {
	var reqs []resolveRequest
	for _, a := range args {
		// K=V или голое имя ключа (секрет без значения, Р28): имена ключей —
		// [A-Z…], имена расширений — [a-z…], так что спутать их нельзя.
		if strings.Contains(a, "=") || manifest.ValidKey(a) {
			if len(reqs) == 0 {
				name, _, _ := strings.Cut(a, "=")
				return nil, cerr.New(cerr.ExitUsage, "usage.args", "resolve: параметр %s до первой ссылки", name)
			}
			reqs[len(reqs)-1].params = append(reqs[len(reqs)-1].params, a)
			continue
		}
		ref, err := catalog.ParseRef(a)
		if err != nil {
			return nil, err
		}
		reqs = append(reqs, resolveRequest{ref: ref})
	}
	if len(reqs) == 0 {
		return nil, cerr.New(cerr.ExitUsage, "usage.args", "resolve: нужна хотя бы одна ссылка [источник/]имя[@версия]")
	}
	return reqs, nil
}

// resolveParams checks K=V of one extension: a key of its config.keys
// (65 config.unknown_key), given once (64), a valid value (65
// config.invalid). A secret is named, never valued (ruling Р28): KEY=- or
// a bare KEY, stdin is not read — params_digest takes "<secret>" anyway;
// any literal is 64 secret.in_argv. A bare non-secret key is 64 usage.args.
func resolveParams(e catalog.Entry, args []string) (map[string]string, error) {
	out := make(map[string]string, len(args))
	ext := e.Manifest.Name
	for _, a := range args {
		k, v, valued := strings.Cut(a, "=")
		details := map[string]any{"ext": ext, "key": k}
		fail := func(ce *cerr.Error) (map[string]string, error) {
			ce.Details = details
			return nil, ce
		}
		sp, ok := e.Manifest.Config.Lookup(k)
		if !ok {
			return fail(cerr.New(cerr.ExitData, "config.unknown_key", "%s: ключа %s нет в config.keys", ext, k))
		}
		if _, dup := out[k]; dup {
			return fail(cerr.New(cerr.ExitUsage, "usage.duplicate_key", "%s: ключ %s задан дважды", ext, k))
		}
		if sp.Secret != "" {
			if valued && v != "-" {
				return fail(cerr.New(cerr.ExitUsage, "secret.in_argv",
					"%s: %s — секрет: значение в аргументах запрещено, укажите %s=- или %s", ext, k, k, k))
			}
			out[k] = ""
			continue
		}
		if !valued {
			return fail(cerr.New(cerr.ExitUsage, "usage.args", "%s: ключу %s нужно значение %s=V", ext, k, k))
		}
		if err := sp.CheckValue(v); err != nil {
			return fail(cerr.New(cerr.ExitData, "config.invalid", "%s: %s: %v", ext, k, err))
		}
		out[k] = v
	}
	return out, nil
}

// resolveHost is the host of the phase: build — from the flags, the arches
// defaulting to this machine's; live — this machine detected (os-release
// under --root, commands in PATH), the flags overriding what they name.
// Any other phase is 64 usage.phase.
func resolveHost(ctx *Context, d catalogDeps, f resolveFlags) (resolve.Host, error) {
	var h resolve.Host
	if f.phase == resolve.PhaseBuild {
		h = resolve.Host{Phase: f.phase, TargetArch: d.arch, HostArch: d.arch}
	} else {
		var err error
		h, err = resolve.Probe{Layout: ctx.Layout, LookPath: exec.LookPath, Arch: d.arch}.Detect(f.phase)
		if err != nil {
			return resolve.Host{}, err
		}
	}
	if f.targetArch != "" {
		h.TargetArch = f.targetArch
	}
	if f.hostArch != "" {
		h.HostArch = f.hostArch
	}
	if f.family != "" {
		h.Family = f.family
	}
	return h, nil
}

// resolveSourceRef is the registry form of the source an entry came from.
func resolveSourceRef(d source.SourceDir) state.SourceRef {
	ref := state.SourceRef{Name: d.Source.Name, Type: d.Source.Type, Origin: d.Source.URL}
	if d.Source.Type == source.TypePath {
		ref.Origin = d.Source.Path
	}
	if d.Lock != nil {
		ref.Resolved, ref.CatalogVersion = d.Lock.Resolved, d.Lock.CatalogVersion
	}
	return ref
}

// resolveInstalled is the registry of this machine as graph nodes; a record
// whose tree cannot be read is left out (Installed warns about it).
func resolveInstalled(ctx *Context, d catalogDeps) ([]resolve.Node, error) {
	eng := newEngine(ctx)
	eng.Cream = d.cream
	inst, err := eng.Installed()
	if err != nil {
		var ce *cerr.Error
		if errors.As(err, &ce) {
			return nil, err
		}
		return nil, cerr.Wrap(err, cerr.ExitState, cerr.CodeIORead, "реестр")
	}
	out := make([]resolve.Node, 0, len(inst))
	for _, in := range inst {
		if in.Manifest == nil {
			continue
		}
		files := make([]string, 0, len(in.Record.Files))
		for _, f := range in.Record.Files {
			files = append(files, f.Dst)
		}
		out = append(out, resolve.Node{Manifest: in.Manifest, Installed: true, Files: files,
			Source: in.Record.Source, Digest: in.Record.ExtDigest})
	}
	return out, nil
}
