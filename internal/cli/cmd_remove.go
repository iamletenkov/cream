package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/iamletenkov/cream/internal/engine"
)

// removeCommands is `cream remove` (§6.5, §11.2).
func (App) removeCommands() []Command {
	var o engine.RemoveOptions
	return []Command{
		{Name: "remove", Usage: "remove [--purge|--forget] [--force] <ext>  — удалить расширение",
			Flags: func(fs *flag.FlagSet) {
				fs.BoolVar(&o.Purge, "purge", false, "удалить и /etc/cream/<ext>, /var/lib/cream/ext/<ext>")
				fs.BoolVar(&o.Forget, "forget", false, "без хука remove: только реестр, дерево и артефакты cream")
				fs.BoolVar(&o.Force, "force", false, "удалить, хотя от расширения зависят установленные")
			},
			Run: func(ctx *Context, args []string) (any, error) { return runRemove(ctx, args, o) }},
	}
}

// removeResult is the result of `cream remove` (golden: remove-*.json).
type removeResult struct{ *engine.RemoveResult }

func (r removeResult) WriteText(w io.Writer) error {
	var notes []string
	if r.Forgotten {
		notes = append(notes, "без хука")
	}
	if r.Purged {
		notes = append(notes, "с настройками и состоянием")
	}
	if len(r.Dependents) > 0 {
		notes = append(notes, "зависимые: "+strings.Join(r.Dependents, " "))
	}
	if r.RebootRequired {
		notes = append(notes, "нужна перезагрузка")
	}
	line := fmt.Sprintf("%s %s удалено", r.Ext, r.Version)
	if len(notes) > 0 {
		line += " (" + strings.Join(notes, "; ") + ")"
	}
	_, err := fmt.Fprintln(w, line)
	return err
}

func runRemove(ctx *Context, args []string, o engine.RemoveOptions) (any, error) {
	if err := requireRoot(ctx, "remove"); err != nil {
		return nil, err
	}
	if len(args) != 1 {
		return nil, ctx.Usagef("remove: нужно одно имя расширения")
	}
	res, err := newEngine(ctx).Remove(ctx.Ctx, args[0], o)
	if err != nil {
		return nil, err
	}
	return removeResult{res}, nil
}
