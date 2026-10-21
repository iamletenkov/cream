package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/engine"
)

// systemCommands are configure and system init (§6.4, §11.2).
func (App) systemCommands() []Command {
	var wait, fromUnit, force, all, again bool
	return []Command{
		{Name: "configure",
			Usage: "configure [--wait] [--force] <ext> | configure --force --all | configure --again <ext> | configure --from-unit <ext>  — запустить configure\n" +
				"повторить хук configure сейчас (CREAM_REASON=apply): перезапуск cream-configure@<ext>.service,\n" +
				"без systemd — хук напрямую; на загрузке хук идёт сам на своей стадии (early, system, cloud-user, graphical),\n" +
				"если отметка выполнения /var/lib/cream/configured/<ext>.json не совпала с реестром или её нет.\n" +
				"журнал: journalctl -u cream-configure@<ext>.service -b; на OpenWrt — init-скрипты procd\n" +
				"/etc/init.d/cream-configure-early (S19) и /etc/init.d/cream-configure (S95), журнал — logread",
			Flags: func(fs *flag.FlagSet) {
				fs.BoolVar(&wait, "wait", false, "ждать исхода хука: status ok или skipped (хук вернул 20), 79 — хук упал; без флага перезапуск ставится в очередь")
				fs.BoolVar(&force, "force", false, "сбросить отметку выполнения и запустить хук сейчас")
				fs.BoolVar(&all, "all", false, "с --force: все расширения с хуком configure")
				fs.BoolVar(&again, "again", false, "повторить configure (причина followup), перезапуск юнита без ожидания; из хука — отложенно (systemd-run --on-active)")
				fs.BoolVar(&fromUnit, "from-unit", false, "вызов из самого юнита cream-configure@<ext>.service; руками не вызывать")
			},
			Run: func(ctx *Context, args []string) (any, error) {
				o := engine.ConfigureOptions{Wait: wait, FromUnit: fromUnit, Force: force, Again: again}
				if all {
					return runConfigureAll(ctx, args, o)
				}
				return runConfigure(ctx, args, o)
			}},
		{Name: "system", Usage: "system init  — каталоги, шаблон юнита, цели configure (OpenWrt: init-скрипты procd)", Run: runSystem},
	}
}

// configureResult is the result of `cream configure` (golden: configure.json).
type configureResult struct{ engine.ConfigureResult }

func (r configureResult) WriteText(w io.Writer) error {
	if r.Status == "" {
		_, err := fmt.Fprintf(w, "%s: configure (%s): %s\n", r.Ext, r.Reason, r.Mode)
		return err
	}
	_, err := fmt.Fprintf(w, "%s: configure (%s): %s, %s\n", r.Ext, r.Reason, r.Mode, r.Status)
	return err
}

func runConfigure(ctx *Context, args []string, o engine.ConfigureOptions) (any, error) {
	if err := requireRoot(ctx, "configure"); err != nil {
		return nil, err
	}
	if len(args) != 1 {
		return nil, ctx.Usagef("configure: нужно одно имя расширения")
	}
	// A manual configure applies the current settings: reason apply.
	res, err := newEngine(ctx).ConfigureReport(ctx.Ctx, args[0], engine.ReasonApply, o)
	if err != nil {
		return nil, err
	}
	return configureResult{res}, nil
}

// configureAllResult is the result of `cream configure --force --all`
// (golden: configure-force-all.json).
type configureAllResult struct {
	Extensions []engine.ConfigureResult `json:"extensions"`
}

func (r configureAllResult) WriteText(w io.Writer) error {
	for _, c := range r.Extensions {
		if err := (configureResult{c}).WriteText(w); err != nil {
			return err
		}
	}
	return nil
}

func runConfigureAll(ctx *Context, args []string, o engine.ConfigureOptions) (any, error) {
	if !o.Force || o.FromUnit || o.Again {
		return nil, cerr.New(cerr.ExitUsage, "usage.flag", "configure: --all — только вместе с --force")
	}
	if err := requireRoot(ctx, "configure"); err != nil {
		return nil, err
	}
	if len(args) != 0 {
		return nil, ctx.Usagef("configure --all: имя расширения не нужно")
	}
	res, err := newEngine(ctx).ConfigureForceAll(ctx.Ctx, o)
	if err != nil {
		// The text mode still tells which extensions were configured.
		_ = configureAllResult{Extensions: res}.WriteText(ctx.Stderr())
		return nil, err
	}
	return configureAllResult{Extensions: res}, nil
}

// systemInitResult is the result of `cream system init` (golden: system-init.json).
type systemInitResult struct{ engine.SystemInitResult }

func (r systemInitResult) WriteText(w io.Writer) error {
	_, err := io.WriteString(w, "cream: машина подготовлена\n")
	return err
}

func runSystem(ctx *Context, args []string) (any, error) {
	if len(args) != 1 || args[0] != "init" {
		return nil, ctx.Usagef("system: единственная подкоманда — init")
	}
	if err := requireRoot(ctx, "system init"); err != nil {
		return nil, err
	}
	res, err := newEngine(ctx).SystemInit(ctx.Ctx)
	if err != nil {
		return nil, err
	}
	return systemInitResult{res}, nil
}
