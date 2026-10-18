package engine

import (
	"context"
	"os"
	"path/filepath"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/config"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/runner"
)

// RunHook runs hook of an installed extension (spec §6.1–§6.3) and waits.
//
// env carries what only the caller knows (Phase, PrevVersion, Reason,
// ChangedKeys); RunHook fills Ext, ExtVersion, the directories, Root and the
// non-secret domain values, and defaults Phase to e.Phase(). The lock/<ext>
// the caller holds is passed to the hook when ctx comes from WithLock.
// Output of the hook goes to e.Stderr with known secret values masked.
//
// Outcomes: OK — nil error; Reboot — nil error, /run/reboot-required is
// created in the live phase and an existing registry record gets
// reboot_required; NotApplicable — 68 hook.not_applicable (a caller with
// --allow-skip ignores it); Failed — 79 hook.failed, details.hook_exit and
// details.timed_out. A hook the manifest does not declare is a bug of the
// caller (70).
func (e *Engine) RunHook(ctx context.Context, in Installed, hook manifest.HookName, env runner.EnvInput) (runner.Outcome, error) {
	name := in.Record.Name
	if in.Manifest == nil {
		return runner.Failed, cerr.New(cerr.ExitInternal, cerr.CodeInternal, "%s: хук %s без манифеста", name, hook)
	}
	if name == "" {
		name = in.Manifest.Name
	}
	decl, ok := in.Manifest.Hooks[hook]
	if !ok {
		return runner.Failed, cerr.New(cerr.ExitInternal, cerr.CodeInternal, "%s: хук %s не объявлен в манифесте", name, hook)
	}
	dom := config.Open(e.L, name, in.Manifest.Config)
	values, err := dom.Values()
	if err != nil {
		return runner.Failed, err
	}
	secrets, err := dom.Secrets()
	if err != nil {
		return runner.Failed, err
	}
	if err := os.MkdirAll(e.L.StateDir(name), 0o755); err != nil {
		return runner.Failed, cerr.Wrap(err, cerr.ExitState, "state.write", "%s: каталог состояния", name)
	}

	if env.Phase == "" {
		env.Phase = e.Phase()
	}
	env.Ext = name
	env.ExtVersion = in.Record.Version
	if env.ExtVersion == "" {
		env.ExtVersion = in.Manifest.Version.String()
	}
	env.ExtDir = e.L.ExtTree(name)
	env.EtcDir = e.L.EtcDir(name)
	env.StateDir = e.L.StateDir(name)
	env.Root = e.root()
	env.ConfigFile = e.L.ConfigFile(name)
	env.SecretsFile = e.L.SecretsFile(name)
	env.Values = values
	env.LockFD, env.LockMode = 0, ""
	spec := runner.Spec{
		Path:    filepath.Join(e.L.ExtTree(name), "hooks", string(hook)),
		Dir:     e.L.ExtTree(name),
		Stdout:  e.stderr(),
		Stderr:  e.stderr(),
		Timeout: decl.Timeout,
		Secrets: secrets,
	}
	if hl, ok := lockFrom(ctx); ok {
		spec.ExtraFiles = []*os.File{hl.h.File()}
		env.LockFD = runner.FirstExtraFD
		env.LockMode = hl.m.String()
	}
	spec.Env = runner.BuildEnv(env)

	res, err := e.Run.Run(ctx, spec)
	if err != nil {
		if ctx.Err() != nil {
			return runner.Failed, cerr.Wrap(err, cerr.ExitHook, "hook.failed", "%s: хук %s прерван", name, hook)
		}
		ce := cerr.Wrap(err, cerr.ExitHook, "hook.failed", "%s: хук %s не запустился", name, hook)
		ce.Details = map[string]any{"hook": string(hook), "hook_exit": res.ExitCode, "timed_out": false}
		return runner.Failed, ce
	}
	if res.TimedOut {
		ce := cerr.New(cerr.ExitHook, "hook.failed", "%s: хук %s не завершился за %s", name, hook, decl.Timeout)
		ce.Details = map[string]any{"hook": string(hook), "hook_exit": res.ExitCode, "timed_out": true}
		return runner.Failed, ce
	}
	out := runner.Classify(res.ExitCode)
	switch out {
	case runner.OK:
		return out, nil
	case runner.Reboot:
		return out, e.markReboot(name, env.Phase)
	case runner.NotApplicable:
		ce := cerr.New(cerr.ExitNotApplicable, "hook.not_applicable",
			"%s: хук %s сообщил, что расширение неприменимо в этой среде (код 20)", name, hook)
		ce.Details = map[string]any{"hook": string(hook), "hook_exit": res.ExitCode}
		return out, ce
	default:
		ce := cerr.New(cerr.ExitHook, "hook.failed", "%s: хук %s завершился с кодом %d", name, hook, res.ExitCode)
		ce.Details = map[string]any{"hook": string(hook), "hook_exit": res.ExitCode, "timed_out": false}
		return out, ce
	}
}

// root is CREAM_ROOT of hooks: the layout root, "/" for the real one.
func (e *Engine) root() string {
	if e.L.Root == "" {
		return "/"
	}
	return e.L.Root
}

// markReboot records that a hook asked for a reboot (§6.2): the registry
// record, when there is one, and /run/reboot-required on a live machine.
func (e *Engine) markReboot(ext, phase string) error {
	rec, ok, err := e.Store.Get(ext)
	if err != nil {
		return err
	}
	if ok && !rec.RebootRequired {
		rec.RebootRequired = true
		if err := e.Store.Put(*rec); err != nil {
			return err
		}
	}
	if phase != PhaseLive {
		return nil
	}
	p := e.L.RebootRequired()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return cerr.Wrap(err, cerr.ExitState, "state.write", "%s", p)
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return cerr.Wrap(err, cerr.ExitState, "state.write", "%s", p)
	}
	if err := f.Close(); err != nil {
		return cerr.Wrap(err, cerr.ExitState, "state.write", "%s", p)
	}
	return nil
}
