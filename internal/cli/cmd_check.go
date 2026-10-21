package cli

import (
	"flag"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/resolve"
)

// checkCommands is `cream check` (spec 2026-10-21 §7.3, §10.3): the
// pre-build check of an image's cream lines on the host — offline, without
// root, sources.yaml or a cache, writing nothing.
func (App) checkCommands() []Command { return checkCommandsWith(defaultCatalogDeps()) }

// checkFlags are the flags of cream check.
type checkFlags struct {
	catalogs  catalogsFlag
	steps     stepsFlag
	os, arch  string
	installed string
}

func checkCommandsWith(d catalogDeps) []Command {
	var f checkFlags
	return []Command{
		{Name: "check",
			Usage: "check --catalog NAME=ARCHIVE|DIR… [--os ID:VERSION] [--arch A] [--installed FILE] --step 'CMD'…" +
				"  — проверка строк cream образа до сборки: офлайн, без root, ничего не пишет",
			Flags: func(fs *flag.FlagSet) {
				fs.Var(&f.catalogs, "catalog", "источник `NAME=PATH`: архив tar.gz или каталог; можно повторять, порядок — приоритет")
				fs.Var(&f.steps, "step", "строка `CMD` cream без «cream» (install, upgrade, remove, source add); можно повторять")
				fs.StringVar(&f.os, "os", "", "ОС образа `ID:VERSION` (debian:12); без неё ОС и семейство не проверяются")
				fs.StringVar(&f.arch, "arch", "", "архитектура образа `A` (по умолчанию — этой машины)")
				fs.StringVar(&f.installed, "installed", "", "`FILE` — метка cream.extensions базы: [{name, version, source?, provides?}]; без флага база неизвестна")
			},
			Run: func(ctx *Context, args []string) (any, error) { return runCheck(ctx, d, f, args) }},
	}
}

// catalogsFlag is the repeatable --catalog NAME=PATH.
type catalogsFlag []checkCatalogArg

type checkCatalogArg struct{ name, path string }

// reCatalogName is a source name (§9.1).
var reCatalogName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

func (f *catalogsFlag) String() string {
	parts := make([]string, 0, len(*f))
	for _, c := range *f {
		parts = append(parts, c.name+"="+c.path)
	}
	return strings.Join(parts, ",")
}

func (f *catalogsFlag) Set(v string) error {
	name, p, ok := strings.Cut(v, "=")
	switch {
	case !ok || p == "":
		return fmt.Errorf("--catalog %q: ожидается NAME=PATH", v)
	case !reCatalogName.MatchString(name):
		return fmt.Errorf("--catalog %q: имя источника — [a-z0-9][a-z0-9._-]{0,63}", v)
	}
	for _, c := range *f {
		if c.name == name {
			return fmt.Errorf("--catalog: источник %s задан дважды", name)
		}
	}
	*f = append(*f, checkCatalogArg{name: name, path: p})
	return nil
}

// stepsFlag is the repeatable --step; a value is one command line.
type stepsFlag []string

func (f *stepsFlag) String() string { return strings.Join(*f, "; ") }

func (f *stepsFlag) Set(v string) error {
	*f = append(*f, v)
	return nil
}

// checkResult is the result of `cream check` (golden: check-*.json).
type checkResult struct {
	resolve.CheckResult
}

func (r checkResult) WriteText(w io.Writer) error {
	for _, s := range r.Steps {
		if err := writeCheckStep(w, s); err != nil {
			return err
		}
	}
	return nil
}

// writeCheckStep prints one step: «N<TAB>status<TAB>command», the layers
// and the removed, the error and the warnings of the step.
func writeCheckStep(w io.Writer, s resolve.CheckStep) error {
	line := fmt.Sprintf("%d\t%s\t%s", s.Step, s.Status, s.Command)
	var what []string
	for _, l := range s.Layers {
		what = append(what, fmt.Sprintf("%s %s (%s)", l.Name, l.Version, l.Op))
	}
	for _, name := range s.Removed {
		what = append(what, name+" (remove)")
	}
	if len(what) > 0 {
		line += " — " + strings.Join(what, ", ")
	}
	if _, err := fmt.Fprintln(w, line); err != nil {
		return err
	}
	if s.Error != nil {
		if _, err := fmt.Fprintf(w, "\tошибка: %s (%s, код %d)\n", s.Error.Message, s.Error.Code, s.Error.Exit); err != nil {
			return err
		}
	}
	for _, x := range s.Warnings {
		if _, err := fmt.Fprintf(w, "\tпредупреждение: %s (%s)\n", x.Message, x.Code); err != nil {
			return err
		}
	}
	return nil
}

// runCheck opens the catalogs, reads --installed and checks the steps. Every
// step is reported: on any failed step the exit and error.code are those of
// the first one, details carry the whole answer (steps, installed), and
// without --json the step report goes to stderr.
func runCheck(ctx *Context, d catalogDeps, f checkFlags, args []string) (any, error) {
	if len(args) > 0 {
		return nil, ctx.Usagef("check: лишние аргументы %q (строки — через --step)", args)
	}
	if len(f.catalogs) == 0 {
		return nil, ctx.Usagef("check: нужен хотя бы один --catalog NAME=PATH")
	}
	if len(f.steps) == 0 {
		return nil, ctx.Usagef("check: нужен хотя бы один --step")
	}
	arch := f.arch
	if arch == "" {
		arch = d.arch
	}
	host, err := resolve.CheckHost(f.os, arch)
	if err != nil {
		return nil, err
	}
	var installed []resolve.Node
	if f.installed != "" {
		if installed, err = resolve.ReadInstalled(f.installed); err != nil {
			return nil, err
		}
	}
	cats := make([]*resolve.CheckCatalog, 0, len(f.catalogs))
	defer func() {
		for _, c := range cats {
			_ = c.Close()
		}
	}()
	for _, a := range f.catalogs {
		c, err := resolve.OpenCheckCatalog(a.name, a.path)
		if err != nil {
			return nil, err
		}
		cats = append(cats, c)
	}
	res := resolve.Check(resolve.CheckRequest{Catalogs: cats, Host: host, Installed: installed, Steps: f.steps,
		Cream: d.cream, BaseUnknown: f.installed == ""})
	exit, first := res.FirstError()
	if first == nil {
		return checkResult{res}, nil
	}
	failed, firstStep := 0, 0
	for _, s := range res.Steps {
		if s.Error != nil {
			if failed++; failed == 1 {
				firstStep = s.Step
			}
		}
	}
	if !ctx.Globals.JSON {
		_ = checkResult{res}.WriteText(ctx.Stderr())
	}
	msg := fmt.Sprintf("шаг %d: %s", firstStep, first.Message)
	if failed > 1 {
		msg = fmt.Sprintf("%s (и ещё ошибок: %d)", msg, failed-1)
	}
	e := cerr.New(exit, first.Code, "%s", msg)
	e.Details = map[string]any{"step": firstStep, "phase": res.Phase, "steps": res.Steps, "installed": res.Installed}
	return nil, e
}
