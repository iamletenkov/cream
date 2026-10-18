package cli

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"strings"
)

// Command is one subcommand.
//
// Run returns the command's result (the "result" of the --json envelope) or an
// error; it never prints the error itself and never decides the exit code —
// Run of the package does. In text mode a result implementing TextWriter is
// printed to stdout; a result implementing ExitCoder sets the exit code
// (cream run: the action's own code, ok = code == 0).
type Command struct {
	Name string
	// Usage is the synopsis after "cream ", first line; more lines may follow.
	Usage string
	// Flags registers the command's own flags; nil if it has none.
	Flags func(*flag.FlagSet)
	Run   func(*Context, []string) (result any, err error)
	// PermuteUntil > 0 stops flag permutation after that many positionals:
	// for `run <ext> <action> args…` it is 1, cream flags only before <ext>
	// (§7.2, ruling Р13).
	PermuteUntil int
}

// TextWriter is a result that knows its human-readable form.
type TextWriter interface {
	WriteText(w io.Writer) error
}

// ExitCoder is a result that carries a non-error exit code.
type ExitCoder interface {
	ExitCode() int
}

// commands is the full command table, in help order. Each group lives in its
// own cmd_<group>.go and is owned by the task that implements it.
func (a App) commands() []Command {
	groups := [][]Command{
		a.metaCommands(),
		a.sourceCommands(),
		a.catalogCommands(),
		a.installCommands(),
		a.removeCommands(),
		a.configCommands(),
		a.actionCommands(),
		a.systemCommands(),
	}
	var out []Command
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// isReservedCommand reports commands deferred past cream 1.0 (ruling Р1):
// they are known to the dispatcher and answer 64 usage.reserved.
func isReservedCommand(name string) bool {
	switch name {
	case "search", "info", "verify", "assume", "lineage", "check", "status", "path":
		return true
	}
	return false
}

func findCommand(cmds []Command, name string) (Command, bool) {
	for _, c := range cmds {
		if c.Name == name {
			return c, true
		}
	}
	return Command{}, false
}

// newFlagSet returns a flag set that reports nothing by itself: the
// dispatcher turns parse errors into usage errors.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return fs
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// mainUsage is the top-level help.
func mainUsage(cmds []Command) string {
	var b strings.Builder
	b.WriteString("использование: cream <команда> [аргументы…] [флаги]\n\nкоманды:\n")
	for _, c := range cmds {
		fmt.Fprintf(&b, "  %s\n", firstLine(c.Usage))
	}
	b.WriteString("  help [<команда>]\n\nглобальные флаги:\n")
	b.WriteString(flagDefaults(nil))
	return b.String()
}

// commandUsage is the help of one command: synopsis, its flags, global flags.
func commandUsage(c Command) string {
	var b strings.Builder
	fmt.Fprintf(&b, "использование: cream %s\n", c.Usage)
	if c.Flags != nil {
		b.WriteString("\nфлаги:\n")
		b.WriteString(flagDefaults(c.Flags))
	}
	b.WriteString("\nглобальные флаги:\n")
	b.WriteString(flagDefaults(nil))
	return b.String()
}

// flagDefaults renders the defaults of register (or of the globals when nil).
func flagDefaults(register func(*flag.FlagSet)) string {
	fs := newFlagSet("")
	if register == nil {
		g := Globals{LockTimeout: DefaultLockTimeout}
		g.register(fs)
	} else {
		register(fs)
	}
	var buf bytes.Buffer
	fs.SetOutput(&buf)
	fs.PrintDefaults()
	return buf.String()
}
