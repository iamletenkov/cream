package config

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/envfile"
	"github.com/iamletenkov/cream/internal/fsx"
	"github.com/iamletenkov/cream/internal/keytype"
)

// AssignInput is what cream set/install received: KEY=VAL arguments, stdin
// for a single KEY=-, and an optional --params-file in env format.
type AssignInput struct {
	Args       []string
	Stdin      io.Reader
	ParamsFile string
	// CheckPerm requires the params file to be root-owned and 0600 on a
	// root-owned path (spec §8.3, §13); the caller sets it on the real root (Р9).
	CheckPerm bool
}

// ParseAssignments turns the input into changes, arguments first, then the
// params file in its order. It rejects (64) a malformed argument, a key
// given twice, more than one KEY=-, empty stdin and a secret value in argv
// (clearing a secret with KEY= is allowed); a key unknown to the schema is
// 65. Values are not type-checked here: Domain.Set does that.
func ParseAssignments(in AssignInput, s keytype.Schema) ([]Change, error) {
	return parseAssignments(in, s, func(path string) (*os.File, error) {
		return fsx.OpenTrusted(path, 0, 0o600)
	})
}

func parseAssignments(in AssignInput, s keytype.Schema, trusted func(string) (*os.File, error)) ([]Change, error) {
	lookup := func(k string) (keytype.Spec, error) {
		sp, ok := s.Lookup(k)
		if !ok || !envfile.ValidName(k) {
			return sp, errDetails(cerr.New(cerr.ExitData, "config.unknown_key", "ключа %s нет в config.keys", k), k)
		}
		return sp, nil
	}
	seen := map[string]bool{}
	dup := func(k string) error {
		if seen[k] {
			return errDetails(cerr.New(cerr.ExitUsage, "usage.duplicate_key", "ключ %s задан дважды", k), k)
		}
		seen[k] = true
		return nil
	}

	out := make([]Change, 0, len(in.Args))
	stdinAt := -1
	for _, a := range in.Args {
		k, v, ok := strings.Cut(a, "=")
		if !ok || k == "" {
			name := a
			if ok {
				name = "=…"
			}
			return nil, cerr.New(cerr.ExitUsage, "usage.assignment", "ожидали KEY=VALUE, получили %q", name)
		}
		if err := dup(k); err != nil {
			return nil, err
		}
		sp, err := lookup(k)
		if err != nil {
			return nil, err
		}
		if v == "-" {
			if stdinAt >= 0 {
				return nil, cerr.New(cerr.ExitUsage, "usage.stdin_twice", "со stdin читается только одно значение (%s=-)", k)
			}
			stdinAt = len(out)
		} else if isSecret(sp) && v != "" {
			return nil, errDetails(cerr.New(cerr.ExitUsage, "secret.in_argv",
				"%s — секрет: значение в аргументах запрещено, передайте %s=- со stdin или --params-file", k, k), k)
		}
		out = append(out, Change{Key: k, Value: v})
	}

	if in.ParamsFile != "" {
		pf, err := readParams(in.ParamsFile, in.CheckPerm, trusted)
		if err != nil {
			return nil, err
		}
		for _, k := range pf.Keys() {
			if err := dup(k); err != nil {
				return nil, err
			}
			if _, err := lookup(k); err != nil {
				return nil, err
			}
			v, _ := pf.Get(k)
			out = append(out, Change{Key: k, Value: v})
		}
	}

	if stdinAt >= 0 {
		v, err := readStdin(in.Stdin, out[stdinAt].Key)
		if err != nil {
			return nil, err
		}
		out[stdinAt].Value = v
	}
	return out, nil
}

// readStdin reads the whole stdin as one value, dropping one trailing
// newline. Empty stdin is a refusal, not an empty value: `echo "$UNSET" |`
// must not clear a key (clear it with KEY=).
func readStdin(r io.Reader, key string) (string, error) {
	if r == nil {
		return "", errDetails(cerr.New(cerr.ExitUsage, "usage.stdin_empty",
			"%s=-: stdin пуст — значение не прочитано (очистить ключ: %s=)", key, key), key)
	}
	data, err := io.ReadAll(io.LimitReader(r, maxFile+1))
	if err != nil {
		return "", cerr.Wrap(err, cerr.ExitUsage, "usage.stdin", "%s=-: чтение stdin", key)
	}
	if len(data) > maxFile {
		return "", cerr.New(cerr.ExitUsage, "usage.stdin_too_large", "%s=-: stdin больше %d байт", key, maxFile)
	}
	v := strings.TrimSuffix(string(data), "\n")
	v = strings.TrimSuffix(v, "\r")
	if v == "" {
		return "", errDetails(cerr.New(cerr.ExitUsage, "usage.stdin_empty",
			"%s=-: stdin пуст — значение не прочитано (очистить ключ: %s=)", key, key), key)
	}
	return v, nil
}

// readParams opens the params file through a checked descriptor (ruling
// Р17) when checkPerm is set, otherwise without following a final link.
func readParams(path string, checkPerm bool, trusted func(string) (*os.File, error)) (*envfile.File, error) {
	open := openPlain
	if checkPerm {
		open = trusted
	}
	f, err := open(path)
	switch {
	case errors.Is(err, fsx.ErrUnsafePerm), errors.Is(err, fs.ErrPermission):
		return nil, cerr.Wrap(err, cerr.ExitPerm, "perm.params_file",
			"--params-file %s: нужен файл root с правами 0600", path)
	case errors.Is(err, fs.ErrNotExist):
		return nil, cerr.Wrap(err, cerr.ExitNotFound, "not_found.params_file", "--params-file %s", path)
	case err != nil:
		return nil, cerr.Wrap(err, cerr.ExitState, "io.params_file", "--params-file %s", path)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if err != nil {
		return nil, cerr.Wrap(err, cerr.ExitState, "io.params_file", "--params-file %s", path)
	}
	if len(data) > maxFile {
		return nil, cerr.New(cerr.ExitData, "config.params_file", "--params-file %s больше %d байт", path, maxFile)
	}
	pf, err := envfile.Parse(data)
	if err != nil {
		return nil, cerr.Wrap(err, cerr.ExitData, "config.params_file", "--params-file %s: разбор", path)
	}
	return pf, nil
}
