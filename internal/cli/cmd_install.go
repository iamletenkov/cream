package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/config"
	"github.com/iamletenkov/cream/internal/engine"
)

// installCommands are install and upgrade on a live machine (Task 13) and
// install in the build phase inside the guest (Task 16, §12.2):
// install --phase build --from-dir D --record F --params-file P
// [--remove-params-file], without references and KEY=VALUE. The result of one extension is its
// registry record plus changed (§11.4, golden install-*.json,
// upgrade-*.json); several extensions (several refs, upgrade --all) give
// {"extensions": [...]} in install order.
func (App) installCommands() []Command {
	var src, paramsFile, phase, fromDir, record string
	var reinstall, acceptRisk, allowSkip, upAll, removeParams bool
	return []Command{
		{Name: "install",
			Usage: "install [--source S] [--reinstall] [--accept-risk] [--allow-skip] [--params-file F] <ref>… [KEY=VALUE…] [KEY=-]  — установка\n" +
				"  install --phase build --from-dir D --record F [--params-file P] [--remove-params-file] [--allow-skip]  — фаза build в госте",
			Flags: func(fs *flag.FlagSet) {
				fs.StringVar(&src, "source", "", "искать ссылки без источника в источнике `S`")
				fs.BoolVar(&reinstall, "reinstall", false, "вызвать хук install, даже если ничего не изменилось")
				fs.BoolVar(&acceptRisk, "accept-risk", false, "ставить расширение с targets.live: confirm")
				fs.BoolVar(&allowSkip, "allow-skip", false, "код 20 хука — status: skipped без ошибки")
				fs.StringVar(&paramsFile, "params-file", "", "файл `F` с KEY=VALUE (0600, владелец root)")
				fs.StringVar(&phase, "phase", engine.PhaseLive, "фаза `P`: live или build (build — только с --from-dir)")
				fs.StringVar(&fromDir, "from-dir", "", "фаза build: дерево расширения `D` (incoming/<id>/<имя>)")
				fs.StringVar(&record, "record", "", "фаза build: record.json `F` — слой cream resolve --json")
				fs.BoolVar(&removeParams, "remove-params-file", false, "фаза build: удалить --params-file после чтения")
				reserveFlags(fs, "with-deps", "allow-downgrade", "require-pinned")
			},
			Run: func(ctx *Context, args []string) (any, error) {
				if phase != engine.PhaseLive || fromDir != "" || record != "" || removeParams {
					return runInstallBuild(ctx, args, installBuildFlags{phase: phase, source: src,
						reinstall: reinstall, acceptRisk: acceptRisk},
						engine.BuildRequest{FromDir: fromDir, Record: record, ParamsFile: paramsFile,
							RemoveParamsFile: removeParams, AllowSkip: allowSkip})
				}
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
	res, err := newEngine(ctx).InstallAll(ctx.Ctx, r)
	if err != nil {
		return nil, err
	}
	return installOutput(res, len(r.Refs) == 1), nil
}

func runUpgrade(ctx *Context, args []string, all bool) (any, error) {
	if err := requireRoot(ctx, "upgrade"); err != nil {
		return nil, err
	}
	res, err := newEngine(ctx).Upgrade(ctx.Ctx, args, all)
	if err != nil {
		return nil, err
	}
	return installOutput(res, !all && len(args) == 1), nil
}

// installBuildFlags are the install flags the build phase refuses or checks.
type installBuildFlags struct {
	phase                 string
	source                string
	reinstall, acceptRisk bool
}

// runInstallBuild is `cream install --phase build --from-dir …` (§12.2 п.4):
// the extension comes from --from-dir, its parameters only from
// --params-file (never argv: secrets), the source from record.json.
func runInstallBuild(ctx *Context, args []string, f installBuildFlags, r engine.BuildRequest) (any, error) {
	switch {
	case f.phase != engine.PhaseBuild && f.phase != engine.PhaseLive:
		return nil, cerr.New(cerr.ExitUsage, "usage.phase", "--phase %q: ожидается build или live", f.phase)
	case f.phase != engine.PhaseBuild:
		return nil, ctx.Usagef("install: --from-dir, --record и --remove-params-file — только с --phase build")
	case r.FromDir == "" || r.Record == "":
		return nil, ctx.Usagef("install --phase build: нужны --from-dir и --record")
	case len(args) > 0:
		return nil, ctx.Usagef("install --phase build: расширение — из --from-dir, параметры — только в --params-file")
	case f.source != "" || f.reinstall || f.acceptRisk:
		return nil, ctx.Usagef("install --phase build: --source, --reinstall и --accept-risk не применяются")
	}
	if err := requireRoot(ctx, "install"); err != nil {
		return nil, err
	}
	r.CheckPerm = ctx.Layout.NeedsRoot()
	res, err := newEngine(ctx).InstallFromDir(ctx.Ctx, r)
	if err != nil {
		return nil, err
	}
	return installResult{*res}, nil
}
