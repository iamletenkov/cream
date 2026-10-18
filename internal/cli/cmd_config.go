package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/config"
	"github.com/iamletenkov/cream/internal/engine"
)

// configCommands are the settings commands (§8.3): list, show, get, set, config.
func (App) configCommands() []Command {
	var getSecret, cfgSecret, cfgShell bool
	var setApply, setNoApply, setWait, setReadonly bool
	var setParams string
	return []Command{
		{Name: "list", Usage: "list [--json]  — установленные расширения", Run: runList},
		{Name: "show", Usage: "show <ext> [--json]  — настройки расширения (секреты — маской)", Run: runShow},
		{Name: "get", Usage: "get [--secret] <ext> KEY  — значение ключа",
			Flags: func(fs *flag.FlagSet) {
				fs.BoolVar(&getSecret, "secret", false, "печатать значение секрета (только root)")
			},
			Run: func(ctx *Context, args []string) (any, error) { return runGet(ctx, "get", args, getSecret) }},
		{Name: "set",
			Usage: "set [--apply|--no-apply] [--wait] [--allow-readonly] [--params-file F] <ext> KEY=VALUE… [KEY=-]  — изменить настройки",
			Flags: func(fs *flag.FlagSet) {
				fs.BoolVar(&setApply, "apply", false, "применить изменения (по умолчанию)")
				fs.BoolVar(&setNoApply, "no-apply", false, "только записать, не применять")
				fs.BoolVar(&setWait, "wait", false, "ждать перезапуска configure")
				fs.BoolVar(&setReadonly, "allow-readonly", false, "разрешить запись readonly-ключей")
				fs.StringVar(&setParams, "params-file", "", "файл `F` с KEY=VALUE (0600, владелец root)")
			},
			Run: func(ctx *Context, args []string) (any, error) {
				if setApply && setNoApply {
					return nil, cerr.New(cerr.ExitUsage, "usage.flag", "--apply и --no-apply несовместимы")
				}
				return runSet(ctx, args, setParams, engine.SetOptions{NoApply: setNoApply, Wait: setWait,
					AllowReadonly: setReadonly})
			}},
		{Name: "config",
			Usage: "config get [--secret] <ext> KEY | config export --shell <ext>  — настройки для хуков и скриптов",
			Flags: func(fs *flag.FlagSet) {
				fs.BoolVar(&cfgSecret, "secret", false, "config get: печатать значение секрета (только root)")
				fs.BoolVar(&cfgShell, "shell", false, "config export: строки KEY='…' для POSIX shell")
			},
			Run: func(ctx *Context, args []string) (any, error) {
				if len(args) == 0 {
					return nil, ctx.Usagef("config: нужна подкоманда get или export")
				}
				switch args[0] {
				case "get":
					return runGet(ctx, "config get", args[1:], cfgSecret)
				case "export":
					return runExport(ctx, args[1:], cfgShell)
				}
				return nil, ctx.Usagef("config: неизвестная подкоманда %q (get, export)", args[0])
			}},
	}
}

// listResult is the result of `cream list` (golden: list.json).
type listResult struct {
	Extensions []engine.ListEntry `json:"extensions"`
}

func (r listResult) WriteText(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, e := range r.Extensions {
		reboot := ""
		if e.RebootRequired {
			reboot = "  нужна перезагрузка"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\tapply: %s%s\n", e.Name, e.Version, e.Status, e.Apply, reboot)
	}
	return tw.Flush()
}

func runList(ctx *Context, args []string) (any, error) {
	if len(args) > 0 {
		return nil, ctx.Usagef("list: лишние аргументы %q", args)
	}
	list, err := newEngine(ctx).List()
	if err != nil {
		return nil, err
	}
	return listResult{Extensions: list}, nil
}

// showResult is the result of `cream show` (golden: show.json).
type showResult struct {
	Ext  string             `json:"ext"`
	Keys []engine.ShowEntry `json:"keys"`
}

func (r showResult) WriteText(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, k := range r.Keys {
		note := k.Origin
		if k.Type != "" {
			note += ", " + k.Type
		}
		if k.Invalid {
			note += ", invalid"
		}
		_, _ = fmt.Fprintf(tw, "%s=%s\t(%s)\n", k.Key, k.Value, note)
	}
	return tw.Flush()
}

func runShow(ctx *Context, args []string) (any, error) {
	if len(args) != 1 {
		return nil, ctx.Usagef("show: нужно одно имя расширения")
	}
	keys, err := newEngine(ctx).Show(args[0])
	if err != nil {
		return nil, err
	}
	return showResult{Ext: args[0], Keys: keys}, nil
}

// getResult is the result of `cream get` and `cream config get` (golden: get.json).
type getResult struct {
	Ext   string `json:"ext"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (r getResult) WriteText(w io.Writer) error {
	_, err := io.WriteString(w, r.Value+"\n")
	return err
}

func runGet(ctx *Context, name string, args []string, secret bool) (any, error) {
	if secret {
		if err := requireRoot(ctx, name+" --secret"); err != nil {
			return nil, err
		}
	}
	if len(args) != 2 {
		return nil, ctx.Usagef("%s: нужны <ext> KEY", name)
	}
	v, err := newEngine(ctx).Get(args[0], args[1], secret)
	if err != nil {
		return nil, err
	}
	return getResult{Ext: args[0], Key: args[1], Value: v}, nil
}

// setResult is the result of `cream set` (golden: set*.json).
type setResult struct{ engine.SetResult }

func (r setResult) WriteText(w io.Writer) error {
	if !r.Changed {
		_, err := fmt.Fprintf(w, "%s: без изменений\n", r.Ext)
		return err
	}
	_, err := fmt.Fprintf(w, "%s: изменено %s; применение: %s\n", r.Ext, strings.Join(r.ChangedKeys, " "), r.Apply)
	return err
}

func runSet(ctx *Context, args []string, paramsFile string, o engine.SetOptions) (any, error) {
	if err := requireRoot(ctx, "set"); err != nil {
		return nil, err
	}
	if len(args) == 0 || (len(args) == 1 && paramsFile == "") {
		return nil, ctx.Usagef("set: нужны <ext> и KEY=VALUE (или --params-file)")
	}
	e := newEngine(ctx)
	in, err := e.Find(args[0])
	if err != nil {
		return nil, err
	}
	ch, err := config.ParseAssignments(config.AssignInput{Args: args[1:], Stdin: ctx.App.Stdin,
		ParamsFile: paramsFile, CheckPerm: ctx.Layout.NeedsRoot()}, in.Manifest.Config)
	if err != nil {
		return nil, err
	}
	res, err := e.Set(ctx.Ctx, args[0], ch, o)
	if err != nil {
		return nil, err
	}
	return setResult{res}, nil
}

// exportResult is the result of `cream config export` (golden: config-export.json).
type exportResult struct {
	Ext    string `json:"ext"`
	Format string `json:"format"`
	Text   string `json:"text"`
}

func (r exportResult) WriteText(w io.Writer) error {
	_, err := io.WriteString(w, r.Text)
	return err
}

func runExport(ctx *Context, args []string, shell bool) (any, error) {
	if len(args) != 1 {
		return nil, ctx.Usagef("config export: нужно одно имя расширения")
	}
	if !shell {
		return nil, cerr.New(cerr.ExitUsage, "usage.format", "config export: укажите формат --shell")
	}
	text, err := newEngine(ctx).ExportShell(args[0])
	if err != nil {
		return nil, err
	}
	return exportResult{Ext: args[0], Format: "shell", Text: text}, nil
}
