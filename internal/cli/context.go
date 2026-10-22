package cli

import (
	"context"
	"flag"
	"io"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/engine"
	"github.com/iamletenkov/cream/internal/paths"
)

// DefaultLockTimeout is the --lock-timeout default (spec §6.3).
const DefaultLockTimeout = 120 * time.Second

// Globals are the global flags (spec §11.2). They are accepted before the
// command name and, thanks to permutation, anywhere among its arguments.
type Globals struct {
	JSON        bool
	Root        string
	Sources     string
	Cache       string
	LockTimeout time.Duration
	Quiet       bool
	Verbose     bool
}

// register binds the global flags to g on fs. Current values of g become the
// defaults, so registering again on a command's flag set keeps what was
// parsed before the command name.
func (g *Globals) register(fs *flag.FlagSet) {
	fs.BoolVar(&g.JSON, "json", g.JSON, "вывод — один JSON-объект в stdout (§11.4)")
	fs.StringVar(&g.Root, "root", g.Root, "перенести все пути под `DIR` (CREAM_ROOT)")
	fs.StringVar(&g.Sources, "sources", g.Sources, "файл источников `FILE` (CREAM_SOURCES)")
	fs.StringVar(&g.Cache, "cache", g.Cache, "каталог кеша `DIR` (CREAM_CACHE)")
	fs.DurationVar(&g.LockTimeout, "lock-timeout", g.LockTimeout, "ожидание блокировки")
	fs.BoolVar(&g.Quiet, "q", g.Quiet, "меньше сообщений в stderr")
	fs.BoolVar(&g.Verbose, "v", g.Verbose, "больше сообщений в stderr")
}

// Context is what a command gets from the dispatcher.
type Context struct {
	App     App
	Command string
	Globals Globals
	Layout  paths.Layout
	// Warn collects warnings for the envelope (and stderr in text mode).
	Warn *cerr.Warnings
	// Out is for human-readable output: stdout in text mode, stderr with --json
	// (stdout then carries only the envelope).
	Out io.Writer
	// Ctx is the invocation's context (App.Context): every engine call and
	// lock wait gets it, so a signal stops them.
	Ctx context.Context
	// Phase receives the phase install, upgrade and remove decided
	// (phaseEngine): the --json envelope names it (§10.1). Nil — not kept.
	Phase *engine.PhaseInfo
}

// Stderr is the diagnostics stream.
func (c *Context) Stderr() io.Writer { return c.App.Stderr }

// Usagef returns a 64 usage.args error for wrong positional arguments.
func (c *Context) Usagef(format string, a ...any) error {
	return cerr.New(cerr.ExitUsage, "usage.args", format, a...)
}
