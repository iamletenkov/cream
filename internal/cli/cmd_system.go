package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/iamletenkov/cream/internal/engine"
)

// systemCommands are configure and system init (§6.4, §11.2).
func (App) systemCommands() []Command {
	var wait, fromUnit bool
	return []Command{
		{Name: "configure", Usage: "configure [--wait] <ext> | configure --from-unit <ext>  — запустить configure",
			Flags: func(fs *flag.FlagSet) {
				fs.BoolVar(&wait, "wait", false, "ждать завершения перезапуска configure")
				fs.BoolVar(&fromUnit, "from-unit", false, "вызов из юнита cream-configure@ (только юнит)")
			},
			Run: func(ctx *Context, args []string) (any, error) {
				return runConfigure(ctx, args, engine.ConfigureOptions{Wait: wait, FromUnit: fromUnit})
			}},
		{Name: "system", Usage: "system init  — каталоги, шаблон юнита, цели configure", Run: runSystem},
	}
}

// configureResult is the result of `cream configure` (golden: configure.json).
type configureResult struct{ engine.ConfigureResult }

func (r configureResult) WriteText(w io.Writer) error {
	_, err := fmt.Fprintf(w, "%s: configure (%s): %s\n", r.Ext, r.Reason, r.Mode)
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
	res, err := newEngine(ctx).ConfigureReport(context.Background(), args[0], engine.ReasonApply, o)
	if err != nil {
		return nil, err
	}
	return configureResult{res}, nil
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
	res, err := newEngine(ctx).SystemInit(context.Background())
	if err != nil {
		return nil, err
	}
	return systemInitResult{res}, nil
}
