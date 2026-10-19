package cli

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/iamletenkov/cream/internal/engine"
)

// actionCommands are the extension actions (§7): run and actions.
func (App) actionCommands() []Command {
	return []Command{
		{Name: "run",
			Usage: "run [--json] [--lock-timeout D] <ext> <action> [args…]  — запустить действие расширения\n" +
				"флаги cream — только до <ext>: всё после имени действия уходит ему как есть",
			PermuteUntil: 1, Run: runAction},
		{Name: "actions", Usage: "actions <ext> [--json]  — действия расширения", Run: runActions},
	}
}

// actionRunResult is the result of `cream run` (golden: run-*.json): the
// action's exit code is cream's exit code, ok = exit_code == 0 (§7.2).
type actionRunResult struct {
	engine.ActionResult
}

func (r actionRunResult) ExitCode() int { return r.ActionResult.ExitCode }

func runAction(ctx *Context, args []string) (any, error) {
	if len(args) < 2 {
		return nil, ctx.Usagef("run: нужны <ext> <action>")
	}
	res, err := newEngine(ctx).RunAction(ctx.Ctx, engine.ActionRequest{Ext: args[0], Action: args[1],
		Args: args[2:], Envelope: ctx.Globals.JSON, Euid: ctx.App.Euid()})
	if err != nil {
		return nil, err
	}
	return actionRunResult{res}, nil
}

// actionsResult is the result of `cream actions` (golden: actions-*.json).
type actionsResult struct {
	engine.ActionsResult
}

func (r actionsResult) WriteText(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, a := range r.Actions {
		_, _ = fmt.Fprintf(tw, "%s\t%s\tlock: %s\toutput: %s\n", a.Name, a.Summary, a.Lock, a.Output)
	}
	return tw.Flush()
}

func runActions(ctx *Context, args []string) (any, error) {
	if len(args) != 1 {
		return nil, ctx.Usagef("actions: нужно одно имя расширения")
	}
	res, err := newEngine(ctx).Actions(args[0])
	if err != nil {
		return nil, err
	}
	return actionsResult{res}, nil
}
