package config

import (
	"errors"
	"strings"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/keytype"
)

// Keys `scope: build` (ADR-0004, spec 2026-10-21 §10.9) are never stored:
// not in config or secrets of the domain, not in params_digest, not in the
// journal. Their values live only in the environment of the install or
// upgrade hook of the current operation. The domain refuses them (64
// config.scope_build); SplitBuild takes them out of the assignments before
// the domain sees them, BuildValues turns them into the hook environment.

func isBuild(sp keytype.Spec) bool { return sp.Scope == keytype.ScopeBuild }

func errScopeBuild(ext, key string) error {
	prefix := ""
	if ext != "" {
		prefix = ext + ": "
	}
	return errDetails(cerr.New(cerr.ExitUsage, "config.scope_build",
		"%s%s — ключ только для сборки: значение передаётся хуку install/upgrade через cream install (%s=…, %s=- со stdin или --params-file) и нигде не хранится",
		prefix, key, key, key), key)
}

// SplitBuild separates the assignments of `scope: build` keys from those the
// domain stores, keeping the input order in both. Build values are checked
// here (type, empty, no NUL), since Domain.Set never sees them; the message of
// a secret key does not quote the value. Unknown keys go to stored, where
// Domain.Set refuses them.
func SplitBuild(ch []Change, s keytype.Schema) (stored, build []Change, err error) {
	for _, c := range ch {
		sp, ok := s.Lookup(c.Key)
		if !ok || !isBuild(sp) {
			stored = append(stored, c)
			continue
		}
		if err := checkBuildValue(sp, c); err != nil {
			return nil, nil, err
		}
		build = append(build, c)
	}
	return stored, build, nil
}

func checkBuildValue(sp keytype.Spec, c Change) error {
	err := sp.CheckValue(c.Value)
	if err == nil && strings.ContainsRune(c.Value, 0) {
		err = errors.New("значение содержит NUL")
	}
	if err == nil {
		return nil
	}
	if isSecret(sp) {
		err = errors.New("значение секрета не прошло проверку типа " + sp.Type.Raw)
	}
	return errDetails(cerr.Wrap(err, cerr.ExitData, "config.invalid", "%s", c.Key), c.Key)
}

// BuildValues is the environment of `scope: build` keys for one hook run: the
// default of every non-pattern build key, overridden by build (changes from
// SplitBuild). A required key (empty: false and no default) without a value
// is 64 config.build_required when required is set (the install hook);
// otherwise it is left out of the environment.
func BuildValues(s keytype.Schema, build []Change, required bool) (map[string]string, error) {
	out := map[string]string{}
	for _, c := range build {
		out[c.Key] = c.Value
	}
	var missing []string
	for k, sp := range s {
		if !isBuild(sp) || sp.Pattern {
			continue
		}
		if _, ok := out[k]; ok {
			continue
		}
		if !sp.Empty && sp.Default == "" {
			missing = append(missing, k)
			continue
		}
		out[k] = sp.Default
	}
	if required && len(missing) > 0 {
		key := missing[0]
		for _, k := range missing[1:] {
			key = min(key, k)
		}
		return nil, errDetails(cerr.New(cerr.ExitUsage, "config.build_required",
			"%s — обязательный ключ только для сборки: передайте его в cream install (%s=…, %s=- со stdin или --params-file)", key, key, key), key)
	}
	return out, nil
}
