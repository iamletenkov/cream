package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/iamletenkov/cream/internal/config"
	"github.com/iamletenkov/cream/internal/engine"
)

// installCommands are install and upgrade on a live machine (Task 13; the
// build phase flags are Task 16). The result of one extension is its
// registry record plus changed (§11.4, golden install-*.json,
// upgrade-*.json); several extensions (several refs, upgrade --all) give
// {"extensions": [...]} in install order.
func (App) installCommands() []Command {
	var src, paramsFile string
	var reinstall, acceptRisk, allowSkip, upAll bool
	return []Command{
		{Name: "install",
			Usage: "install [--source S] [--reinstall] [--accept-risk] [--allow-skip] [--params-file F] <ref>… [KEY=VALUE…] [KEY=-]  — установка",
			Flags: func(fs *flag.FlagSet) {
				fs.StringVar(&src, "source", "", "искать ссылки без источника в источнике `S`")
				fs.BoolVar(&reinstall, "reinstall", false, "вызвать хук install, даже если ничего не изменилось")
				fs.BoolVar(&acceptRisk, "accept-risk", false, "ставить расширение с targets.live: confirm")
				fs.BoolVar(&allowSkip, "allow-skip", false, "код 20 хука — status: skipped без ошибки")
				fs.StringVar(&paramsFile, "params-file", "", "файл `F` с KEY=VALUE (0600, владелец root)")
				reserveFlags(fs, "with-deps", "allow-downgrade")
			},
			Run: func(ctx *Context, args []string) (any, error) {
				return runInstall(ctx, args, engine.InstallRequest{Source: src, Reinstall: reinstall,
					AcceptRisk: acceptRisk, AllowSkip: allowSkip,
					Params: config.AssignInput{ParamsFile: paramsFile}})
			}},
		{Name: "upgrade", Usage: "upgrade <ext>… | upgrade --all  — обновление до версии в кеше источника",
			Flags: func(fs *flag.FlagSet) {
				fs.BoolVar(&upAll, "all", false, "обновить все установленные")
			},
			Run: func(ctx *Context, args []string) (any, error) { return runUpgrade(ctx, args, upAll) }},
	}
}

// installResult is one extension (golden install-*.json).
type installResult struct{ engine.InstallResult }

func (r installResult) WriteText(w io.Writer) error {
	return writeInstallLine(w, r.InstallResult)
}

// installListResult is several extensions.
type installListResult struct {
	Extensions []engine.InstallResult `json:"extensions"`
}

func (r installListResult) WriteText(w io.Writer) error {
	for _, x := range r.Extensions {
		if err := writeInstallLine(w, x); err != nil {
			return err
		}
	}
	return nil
}

func writeInstallLine(w io.Writer, r engine.InstallResult) error {
	note := "без изменений"
	if r.Changed {
		note = "изменено"
	}
	if r.RebootRequired {
		note += ", нужна перезагрузка"
	}
	_, err := fmt.Fprintf(w, "%s %s: %s (%s)\n", r.Name, r.Version, r.Status, note)
	return err
}

func installOutput(res []engine.InstallResult, single bool) any {
	if single && len(res) == 1 {
		return installResult{res[0]}
	}
	if res == nil {
		res = []engine.InstallResult{}
	}
	return installListResult{Extensions: res}
}

// runInstall splits args into references and KEY=VALUE parameters.
func runInstall(ctx *Context, args []string, r engine.InstallRequest) (any, error) {
	if err := requireRoot(ctx, "install"); err != nil {
		return nil, err
	}
	for _, a := range args {
		if strings.Contains(a, "=") {
			r.Params.Args = append(r.Params.Args, a)
		} else {
			r.Refs = append(r.Refs, a)
		}
	}
	if len(r.Refs) == 0 {
		return nil, ctx.Usagef("install: нужна ссылка на расширение")
	}
	r.Params.Stdin = ctx.App.Stdin
	r.Params.CheckPerm = ctx.Layout.NeedsRoot()
	res, err := newEngine(ctx).InstallAll(context.Background(), r)
	if err != nil {
		return nil, err
	}
	return installOutput(res, len(r.Refs) == 1), nil
}

func runUpgrade(ctx *Context, args []string, all bool) (any, error) {
	if err := requireRoot(ctx, "upgrade"); err != nil {
		return nil, err
	}
	res, err := newEngine(ctx).Upgrade(context.Background(), args, all)
	if err != nil {
		return nil, err
	}
	return installOutput(res, !all && len(args) == 1), nil
}
