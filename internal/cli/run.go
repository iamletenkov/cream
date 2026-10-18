// Package cli is cream's command dispatcher (spec §11): stdlib flag with
// argument permutation, global flags, the --json envelope and the mapping of
// errors to exit codes. Commands live in cmd_<group>.go, one group per task.
package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/paths"
)

// App is everything the process gives cream; tests substitute every field.
type App struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Env    func(string) string
	Euid   func() int
	Now    func() time.Time
	// Context ends on SIGINT, SIGTERM or SIGHUP (main, signal.NotifyContext):
	// lock waits stop, hooks and actions get their process group killed.
	// nil — context.Background().
	Context context.Context
}

// Run executes one cream invocation and returns the process exit code.
// It is the only place where errors become exit codes and the envelope.
func Run(args []string, app App) int {
	return run(args, app, app.commands())
}

// invocation is the state of one run.
type invocation struct {
	app     App
	cmds    []Command
	warn    *cerr.Warnings
	globals Globals
	command string
	json    bool
	// usage is appended to the error message in text mode for usage errors.
	usage string
}

func run(args []string, app App, cmds []Command) int {
	inv := &invocation{app: app, cmds: cmds, warn: &cerr.Warnings{},
		globals: Globals{LockTimeout: DefaultLockTimeout}}
	result, err := inv.dispatch(args)
	return inv.finish(result, err)
}

func (inv *invocation) dispatch(args []string) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			_, _ = fmt.Fprintf(inv.app.Stderr, "cream: panic: %v\n%s", r, debug.Stack())
			result, err = nil, cerr.New(cerr.ExitInternal, cerr.CodeInternal, "внутренняя ошибка cream: %v", r)
		}
	}()
	// Envelope even if parsing fails before --json is reached.
	inv.json = jsonRequested(args)

	gfs := newFlagSet("cream")
	inv.globals.register(gfs)
	showVersion := gfs.Bool("version", false, "")
	if err := gfs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return inv.help(nil)
		}
		inv.usage = mainUsage(inv.cmds)
		return nil, flagError(err)
	}
	inv.json = inv.globals.JSON || inv.json
	rest := gfs.Args()
	if *showVersion {
		rest = append([]string{"version"}, rest...)
	}
	if len(rest) == 0 {
		inv.usage = mainUsage(inv.cmds)
		return nil, cerr.New(cerr.ExitUsage, "usage.no_command", "не указана команда")
	}
	name := rest[0]
	inv.command = name
	if name == "help" {
		return inv.help(rest[1:])
	}
	if isReservedCommand(name) {
		return nil, reservedError("команда " + name)
	}
	cmd, ok := findCommand(inv.cmds, name)
	if !ok {
		inv.usage = mainUsage(inv.cmds)
		return nil, cerr.New(cerr.ExitUsage, "usage.unknown_command", "неизвестная команда %q", name)
	}

	fs := newFlagSet(name)
	inv.globals.register(fs)
	if cmd.Flags != nil {
		cmd.Flags(fs)
	}
	inv.usage = commandUsage(cmd)
	permuted, err := permute(fs, rest[1:], cmd.PermuteUntil)
	if err != nil {
		return nil, cerr.Wrap(err, cerr.ExitUsage, "usage.flag", "неверный вызов %s", name)
	}
	if err := fs.Parse(permuted); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			inv.json = inv.globals.JSON || inv.json
			return helpResult{Usage: commandUsage(cmd)}, nil
		}
		return nil, flagError(err)
	}
	inv.json = inv.globals.JSON
	if f := usedReservedFlag(fs); f != "" {
		return nil, reservedError("флаг --" + f)
	}
	if inv.globals.LockTimeout < 0 {
		return nil, cerr.New(cerr.ExitUsage, "usage.flag", "--lock-timeout не может быть отрицательным")
	}
	layout := paths.Detect(inv.app.Env, inv.app.Euid(), inv.globals.Root, inv.globals.Sources, inv.globals.Cache)
	if !filepath.IsAbs(layout.Root) {
		return nil, cerr.New(cerr.ExitUsage, "usage.root", "--root (CREAM_ROOT) должен быть абсолютным путём: %q", layout.Root)
	}
	inv.usage = ""
	ctx := &Context{App: inv.app, Command: name, Globals: inv.globals, Layout: layout, Warn: inv.warn,
		Out: inv.app.Stdout, Ctx: inv.app.Context}
	if ctx.Ctx == nil {
		ctx.Ctx = context.Background()
	}
	if inv.json {
		// stdout belongs to the envelope only: a command writing to Out or to
		// App.Stdout by mistake still cannot break "exactly one object".
		ctx.Out = inv.app.Stderr
		ctx.App.Stdout = inv.app.Stderr
	}
	result, err = cmd.Run(ctx, fs.Args())
	if err != nil && cerr.ExitCode(err) == cerr.ExitUsage {
		inv.usage = commandUsage(cmd)
	}
	return result, err
}

// help answers `help`, `help <command>`, -h and --help.
func (inv *invocation) help(args []string) (any, error) {
	inv.command = "help"
	// Flags after "help" (--json is already known from the pre-scan) are not command names.
	args = slices.DeleteFunc(slices.Clone(args), func(a string) bool { return strings.HasPrefix(a, "-") })
	if len(args) == 0 {
		return helpResult{Usage: mainUsage(inv.cmds)}, nil
	}
	cmd, ok := findCommand(inv.cmds, args[0])
	if !ok {
		return nil, cerr.New(cerr.ExitUsage, "usage.unknown_command", "неизвестная команда %q", args[0])
	}
	return helpResult{Usage: commandUsage(cmd)}, nil
}

type helpResult struct {
	Usage string `json:"usage"`
}

func (h helpResult) WriteText(w io.Writer) error {
	_, err := io.WriteString(w, h.Usage)
	return err
}

// finish prints the outcome and returns the exit code.
func (inv *invocation) finish(result any, err error) int {
	code := cerr.ExitCode(err)
	if err == nil {
		if ec, ok := result.(ExitCoder); ok {
			code = ec.ExitCode()
		}
	}
	if inv.json {
		return inv.finishJSON(result, err, code)
	}
	return inv.finishText(result, err, code)
}

func (inv *invocation) finishJSON(result any, err error, code int) int {
	var buf bytes.Buffer
	env := newEnvelope(inv.command, err == nil && code == 0, result, inv.warn.List(), err)
	if mErr := writeJSON(&buf, env); mErr != nil {
		// The result itself is not serialisable: a bug in the command.
		err = cerr.Wrap(mErr, cerr.ExitInternal, cerr.CodeInternal, "результат %s не сериализуется", inv.command)
		code = cerr.ExitInternal
		buf.Reset()
		if mErr := writeJSON(&buf, newEnvelope(inv.command, false, nil, inv.warn.List(), err)); mErr != nil {
			return inv.stdoutFailed(mErr)
		}
	}
	if _, wErr := inv.app.Stdout.Write(buf.Bytes()); wErr != nil {
		return inv.stdoutFailed(wErr)
	}
	return code
}

func (inv *invocation) finishText(result any, err error, code int) int {
	if !inv.globals.Quiet {
		for _, w := range inv.warn.List() {
			_, _ = fmt.Fprintf(inv.app.Stderr, "cream: предупреждение: %s (%s)\n", w.Message, w.Code)
		}
	}
	if err != nil {
		_, _ = fmt.Fprintf(inv.app.Stderr, "cream: %s\n", err.Error())
		if inv.usage != "" {
			_, _ = fmt.Fprintf(inv.app.Stderr, "\n%s", inv.usage)
		}
		return code
	}
	if tw, ok := result.(TextWriter); ok {
		if wErr := tw.WriteText(inv.app.Stdout); wErr != nil {
			return inv.stdoutFailed(wErr)
		}
	}
	return code
}

// stdoutFailed reports a failed write to stdout (closed pipe, full disk): 74.
func (inv *invocation) stdoutFailed(err error) int {
	_, _ = fmt.Fprintf(inv.app.Stderr, "cream: запись в stdout: %v\n", err)
	return cerr.ExitState
}

func flagError(err error) error {
	return cerr.Wrap(err, cerr.ExitUsage, "usage.flag", "неверный флаг")
}

func reservedError(what string) error {
	return cerr.New(cerr.ExitUsage, "usage.reserved", "%s: зарезервировано в cream/v1, не реализовано в cream 1.0", what)
}

// jsonRequested reports a --json among the arguments before "--": used only to
// keep the envelope when parsing fails before the flag is reached.
func jsonRequested(args []string) bool {
	for _, a := range args {
		switch {
		case a == "--":
			return false
		case a == "--json" || a == "-json":
			return true
		case strings.HasPrefix(a, "--json=") || strings.HasPrefix(a, "-json="):
			_, v, _ := strings.Cut(a, "=")
			if b, err := strconv.ParseBool(v); err == nil && b {
				return true
			}
		}
	}
	return false
}
