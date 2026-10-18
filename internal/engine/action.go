package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/lock"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/runner"
)

// Action exit codes (§7.2): an action owns 0–63, anything else is a protocol
// violation of cream run.
const (
	actionMaxExit = 63
	// actionMaxJSON bounds the stdout of an output: json action.
	actionMaxJSON = 4 << 20
)

// ActionRequest is one `cream run <ext> <action> [args…]`.
type ActionRequest struct {
	Ext, Action string
	// Args are everything after the action name, never permuted (Р13).
	Args []string
	// Envelope is --json: the action's JSON object goes only into
	// ActionResult.Output, not to Stdout (stdout belongs to the envelope).
	Envelope bool
	// Euid is cream's effective uid, for root: true actions on the real root (Р9).
	Euid int
}

// ActionResult is the result of `cream run --json` (§7.2): a contract for
// stvor-agent, fields are only added. Output is the action's object for
// output: json, null for text or for a failed action that printed nothing.
type ActionResult struct {
	Action   string          `json:"action"`
	ExitCode int             `json:"exit_code"`
	Output   json.RawMessage `json:"output"`
}

// RunAction runs an action of an installed extension (§7.1–§7.2). Before the
// start: the extension and the action (66), the phase (68 action.phase), root
// (77), the arguments (64/65), then lock/<ext> by the action's lock (75). The
// action's own code 0–63 is ActionResult.ExitCode with a nil error; a code
// outside that range or stdout that is not exactly one JSON object for
// output: json is 76 action.protocol; a timeout is 79 action.timeout.
//
// Stdout of a text action goes to e.Stdout as is; stderr goes to e.Stderr
// through the secret filter (§6.3). A JSON action's stdout is checked first
// and then written to e.Stdout unchanged (without Envelope).
func (e *Engine) RunAction(ctx context.Context, r ActionRequest) (ActionResult, error) {
	res := ActionResult{Action: r.Ext + "/" + r.Action}
	in, err := e.Find(r.Ext)
	if err != nil {
		return res, err
	}
	a, ok := in.Manifest.Actions[r.Action]
	if !ok {
		ce := cerr.New(cerr.ExitNotFound, "not_found.action", "у расширения %s нет действия %q", r.Ext, r.Action)
		ce.Details = map[string]any{"ext": r.Ext, "action": r.Action}
		return res, ce
	}
	phase := e.Phase()
	if !slices.Contains(a.Phases, manifest.Phase(phase)) {
		ce := cerr.New(cerr.ExitNotApplicable, "action.phase", "%s: действие недопустимо в фазе %s", res.Action, phase)
		ce.Details = map[string]any{"action": res.Action, "phase": phase, "phases": a.Phases}
		return res, ce
	}
	if a.Root && e.L.NeedsRoot() && r.Euid != 0 {
		return res, cerr.New(cerr.ExitPerm, "perm.root", "%s: нужен root", res.Action)
	}
	args, err := actionParseArgs(a, r.Args, e.Stdin)
	if err != nil {
		return res, err
	}

	dom := e.Domain(in)
	values, err := dom.Values()
	if err != nil {
		return res, err
	}
	secrets, err := dom.Secrets()
	if err != nil {
		return res, err
	}
	if args.hasSecret {
		secrets = append(secrets, args.secret)
	}
	if err := os.MkdirAll(e.L.StateDir(r.Ext), 0o755); err != nil {
		return res, cerr.Wrap(err, cerr.ExitState, "state.write", "%s: каталог состояния", r.Ext)
	}

	env := runner.EnvInput{Phase: phase, Ext: r.Ext, ExtVersion: in.Record.Version,
		ExtDir: e.L.ExtTree(r.Ext), EtcDir: e.L.EtcDir(r.Ext), StateDir: e.L.StateDir(r.Ext), Root: e.root(),
		ConfigFile: e.L.ConfigFile(r.Ext), SecretsFile: e.L.SecretsFile(r.Ext),
		Action: r.Action, Output: string(a.Output), Values: actionEnvValues(values, args.values)}
	spec := runner.Spec{
		Path:    filepath.Join(e.L.ExtTree(r.Ext), "actions", r.Action),
		Args:    args.argv,
		Dir:     e.L.ExtTree(r.Ext),
		Timeout: a.Timeout,
	}
	switch a.Stdin {
	case manifest.StdinSecret:
		if args.hasSecret {
			spec.Stdin = strings.NewReader(args.secret)
		}
	case manifest.StdinPass:
		spec.Stdin = e.Stdin
	}

	if a.Lock != manifest.LockNone {
		mode := lock.Exclusive
		if a.Lock == manifest.LockShared {
			mode = lock.Shared
		}
		h, err := e.Locks.Ext(ctx, r.Ext, mode)
		if err != nil {
			return res, err
		}
		defer h.Release()
		spec.ExtraFiles = []*os.File{h.File()}
		env.LockFD, env.LockMode = runner.FirstExtraFD, mode.String()
	}
	spec.Env = runner.BuildEnv(env)

	stderr := runner.NewSecretFilter(e.stderr(), secrets)
	var captured *actionCapture
	if a.Output == manifest.OutputJSON {
		captured = &actionCapture{limit: actionMaxJSON}
		spec.Stdout = captured
	} else {
		spec.Stdout = e.actionStdout()
	}
	spec.Stderr = stderr
	out, runErr := e.Run.Run(ctx, spec)
	// What a broken action printed is still worth seeing, but never on stdout.
	dump := func() {
		if captured != nil && captured.buf.Len() > 0 {
			_, _ = stderr.Write(captured.buf.Bytes())
		}
	}
	if runErr != nil {
		dump()
		_ = stderr.Close()
		ce := cerr.Wrap(runErr, cerr.ExitHook, "action.failed", "%s: действие не запустилось", res.Action)
		ce.Details = map[string]any{"action": res.Action}
		return res, ce
	}
	if out.TimedOut {
		dump()
		_ = stderr.Close()
		ce := cerr.New(cerr.ExitHook, "action.timeout", "%s: действие не завершилось за %s", res.Action, a.Timeout)
		ce.Details = map[string]any{"action": res.Action, "timeout": a.Timeout.String()}
		return res, ce
	}
	if out.ExitCode < 0 || out.ExitCode > actionMaxExit {
		dump()
		_ = stderr.Close()
		ce := cerr.New(cerr.ExitProtocol, "action.protocol",
			"%s: код возврата %d вне диапазона действий 0–%d", res.Action, out.ExitCode, actionMaxExit)
		ce.Details = map[string]any{"action": res.Action, "exit_code": out.ExitCode}
		return res, ce
	}
	res.ExitCode = out.ExitCode
	if captured != nil {
		obj, err := actionCheckJSON(captured, out.ExitCode)
		if err != nil {
			dump()
			_ = stderr.Close()
			ce := cerr.Wrap(err, cerr.ExitProtocol, "action.protocol",
				"%s: output: json — stdout должен содержать ровно один JSON-объект", res.Action)
			ce.Details = map[string]any{"action": res.Action, "exit_code": out.ExitCode}
			return ActionResult{Action: res.Action}, ce
		}
		res.Output = obj
		if !r.Envelope && captured.buf.Len() > 0 {
			if _, err := e.actionStdout().Write(captured.buf.Bytes()); err != nil {
				_ = stderr.Close()
				return res, cerr.Wrap(err, cerr.ExitState, "io.stdout", "%s: запись stdout", res.Action)
			}
		}
	}
	if err := stderr.Close(); err != nil {
		return res, cerr.Wrap(err, cerr.ExitState, "io.stderr", "%s: запись stderr", res.Action)
	}
	return res, nil
}

// actionCheckJSON checks the stdout of an output: json action: exactly one
// JSON object. A failed action (code != 0) may print nothing; Output is then
// nil (null in the envelope).
func actionCheckJSON(c *actionCapture, code int) (json.RawMessage, error) {
	if c.over {
		return nil, errors.New("stdout больше предела")
	}
	data := bytes.TrimSpace(c.buf.Bytes())
	if len(data) == 0 {
		if code != 0 {
			return nil, nil
		}
		return nil, errors.New("stdout пуст")
	}
	if data[0] != '{' {
		return nil, errors.New("stdout — не JSON-объект")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	var obj json.RawMessage
	if err := dec.Decode(&obj); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("после объекта есть ещё данные")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, obj); err != nil {
		return nil, err
	}
	return compact.Bytes(), nil
}

// actionCapture buffers a JSON action's stdout up to limit; the rest is
// dropped and marked, so the action is never blocked on a full pipe.
type actionCapture struct {
	buf   bytes.Buffer
	limit int
	over  bool
}

func (c *actionCapture) Write(p []byte) (int, error) {
	if room := c.limit - c.buf.Len(); room < len(p) {
		c.over = true
		if room > 0 {
			c.buf.Write(p[:room])
		}
		return len(p), nil
	}
	return c.buf.Write(p)
}

// actionStdout is Stdout or a discarding writer.
func (e *Engine) actionStdout() io.Writer {
	if e.Stdout == nil {
		return io.Discard
	}
	return e.Stdout
}

// ActionParam is one parameter of an args: pairs action in `cream actions`.
type ActionParam struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
	Secret   bool   `json:"secret"`
}

// ActionInfo is one action in `cream actions --json` (§7.2): a contract for
// stvor-agent, which checks the actions it needs by Name; fields are only added.
type ActionInfo struct {
	Name           string        `json:"name"`
	Summary        string        `json:"summary"`
	Args           string        `json:"args"`
	Params         []ActionParam `json:"params"`
	Stdin          string        `json:"stdin"`
	Lock           string        `json:"lock"`
	Output         string        `json:"output"`
	TimeoutSeconds int64         `json:"timeout_seconds"`
	Phases         []string      `json:"phases"`
	Root           bool          `json:"root"`
}

// ActionsResult is the result of `cream actions <ext>`.
type ActionsResult struct {
	Ext     string       `json:"ext"`
	Version string       `json:"version"`
	Actions []ActionInfo `json:"actions"`
}

// Actions is `cream actions <ext>`: the actions of an installed extension,
// sorted by name.
func (e *Engine) Actions(ext string) (ActionsResult, error) {
	in, err := e.Find(ext)
	if err != nil {
		return ActionsResult{}, err
	}
	out := ActionsResult{Ext: ext, Version: in.Record.Version, Actions: []ActionInfo{}}
	names := make([]string, 0, len(in.Manifest.Actions))
	for name := range in.Manifest.Actions {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		a := in.Manifest.Actions[name]
		info := ActionInfo{Name: name, Summary: a.Summary, Args: string(a.Args), Params: []ActionParam{},
			Stdin: string(a.Stdin), Lock: string(a.Lock), Output: string(a.Output),
			TimeoutSeconds: int64(a.Timeout.Seconds()), Phases: []string{}, Root: a.Root}
		for _, p := range actionParamNames(a) {
			pp := a.Params[p]
			info.Params = append(info.Params, ActionParam{Name: p, Type: pp.Type.Raw, Required: pp.Required,
				Secret: pp.Secret})
		}
		for _, ph := range a.Phases {
			info.Phases = append(info.Phases, string(ph))
		}
		out.Actions = append(out.Actions, info)
	}
	return out, nil
}
