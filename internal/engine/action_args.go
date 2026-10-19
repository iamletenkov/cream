package engine

import (
	"io"
	"slices"
	"strings"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/envfile"
	"github.com/iamletenkov/cream/internal/manifest"
)

// actionMaxStdin bounds a secret read from cream's stdin (KEY=-).
const actionMaxStdin = 64 << 10

// actionEnvDenied reports names a pairs parameter never puts into the action
// environment, even when the manifest lets them through as a parameter: they
// change how the action's interpreter runs.
func actionEnvDenied(k string) bool {
	switch k {
	case "PATH", "LANG", "HOME", "PWD", "LD_PRELOAD", "LD_LIBRARY_PATH", "BASH_ENV", "ENV", "IFS":
		return true
	}
	return strings.HasPrefix(k, "CREAM_")
}

// actionInput is what `cream run` hands to the action besides the domain.
type actionInput struct {
	// argv is the action's argv after its path: the arguments as given
	// (pairs included, the secret one as KEY=-).
	argv []string
	// values are the non-secret pairs; they override domain values of the
	// same name in the environment.
	values map[string]string
	// secret is the value of the secret parameter read from stdin.
	secret    string
	hasSecret bool
}

// actionParseArgs checks the arguments of action a (§7.1–§7.2) before it
// starts: none — no arguments; free — as is; pairs — KEY=VALUE by params,
// every check before stdin is read. A secret parameter is accepted only as
// KEY=- (anything else in argv is 64 secret.in_argv, R3); its value is read
// from stdin, empty stdin is 64 usage.stdin_empty.
func actionParseArgs(a manifest.Action, args []string, stdin io.Reader) (actionInput, error) {
	switch a.Args {
	case manifest.ArgsFree:
		return actionInput{argv: slices.Clone(args)}, nil
	case manifest.ArgsPairs:
	default:
		if len(args) > 0 {
			return actionInput{}, cerr.New(cerr.ExitUsage, "usage.args", "действие не принимает аргументов: %q", args)
		}
		return actionInput{argv: []string{}}, nil
	}

	in := actionInput{argv: slices.Clone(args), values: map[string]string{}}
	seen := map[string]bool{}
	secretKey := ""
	for _, arg := range args {
		k, v, ok := strings.Cut(arg, "=")
		if !ok || k == "" {
			shown := arg
			if ok {
				shown = "=…"
			}
			return actionInput{}, cerr.New(cerr.ExitUsage, "usage.assignment", "ожидали KEY=VALUE, получили %q", shown)
		}
		p, known := a.Params[k]
		if !known || !envfile.ValidName(k) {
			return actionInput{}, actionKeyErr(cerr.New(cerr.ExitUsage, "usage.unknown_param",
				"у действия нет параметра %s (параметры: %s)", k, strings.Join(actionParamNames(a), ", ")), k)
		}
		if seen[k] {
			return actionInput{}, actionKeyErr(cerr.New(cerr.ExitUsage, "usage.duplicate_key", "параметр %s задан дважды", k), k)
		}
		seen[k] = true
		switch {
		case p.Secret && v != "-":
			return actionInput{}, actionKeyErr(cerr.New(cerr.ExitUsage, "secret.in_argv",
				"%s — секрет: значение в аргументах запрещено, передайте %s=- и значение на stdin", k, k), k)
		case p.Secret:
			secretKey = k
			continue
		case v == "-":
			return actionInput{}, actionKeyErr(cerr.New(cerr.ExitUsage, "usage.stdin_param",
				"%s=-: со stdin читается только секретный параметр", k), k)
		case v == "" && p.Required:
			return actionInput{}, actionKeyErr(cerr.New(cerr.ExitUsage, "usage.param_required",
				"параметр %s обязателен и не может быть пустым", k), k)
		case v != "":
			if err := p.Type.Check(v); err != nil {
				return actionInput{}, actionKeyErr(cerr.Wrap(err, cerr.ExitData, "config.invalid",
					"%s: значение не подходит под тип %s", k, p.Type.Raw), k)
			}
		}
		in.values[k] = v
	}
	for _, k := range actionParamNames(a) {
		if a.Params[k].Required && !seen[k] {
			return actionInput{}, actionKeyErr(cerr.New(cerr.ExitUsage, "usage.param_required",
				"не задан обязательный параметр %s", k), k)
		}
	}
	if secretKey != "" {
		v, err := actionReadSecret(stdin, secretKey)
		if err != nil {
			return actionInput{}, err
		}
		in.secret, in.hasSecret = v, true
	}
	return in, nil
}

// actionReadSecret reads the whole stdin as one value without one trailing
// newline; empty stdin is a refusal, as for cream set KEY=- (§8.3).
func actionReadSecret(r io.Reader, key string) (string, error) {
	empty := actionKeyErr(cerr.New(cerr.ExitUsage, "usage.stdin_empty", "%s=-: stdin пуст — значение не прочитано", key), key)
	if r == nil {
		return "", empty
	}
	data, err := io.ReadAll(io.LimitReader(r, actionMaxStdin+1))
	if err != nil {
		return "", cerr.Wrap(err, cerr.ExitState, cerr.CodeIOStdin, "%s=-: чтение stdin", key)
	}
	if len(data) > actionMaxStdin {
		return "", cerr.New(cerr.ExitUsage, "usage.stdin_too_large", "%s=-: stdin больше %d байт", key, actionMaxStdin)
	}
	v := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
	if v == "" {
		return "", empty
	}
	return v, nil
}

// actionEnvValues merges the non-secret pairs over the domain values; names
// that would change the interpreter (actionEnvDenied) stay out.
func actionEnvValues(domain, pairs map[string]string) map[string]string {
	out := make(map[string]string, len(domain)+len(pairs))
	for k, v := range domain {
		out[k] = v
	}
	for k, v := range pairs {
		if actionEnvDenied(k) {
			continue
		}
		out[k] = v
	}
	return out
}

func actionParamNames(a manifest.Action) []string {
	names := make([]string, 0, len(a.Params))
	for k := range a.Params {
		names = append(names, k)
	}
	slices.Sort(names)
	return names
}

func actionKeyErr(e *cerr.Error, key string) *cerr.Error {
	e.Details = map[string]any{"key": key}
	return e
}
