package runner

import (
	"slices"
	"strconv"
	"strings"
)

// fixedPath and fixedLang are the only inherited-looking variables a hook
// gets; everything else in its environment is set by cream (spec §6.3).
const (
	fixedPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	fixedLang = "C.UTF-8"
)

// FirstExtraFD is the descriptor number of Spec.ExtraFiles[0] in the child:
// pass the lock file there and set EnvInput.LockFD to this value.
const FirstExtraFD = 3

// EnvInput is everything BuildEnv puts into a hook or action environment.
// Empty optional fields (PrevVersion, Reason, ChangedKeys, Action, Output,
// LockFD <= 0) are left out of the environment.
//
// Values are the non-secret keys of the settings domain (config.Domain.Values):
// secrets never go here — the caller is responsible, BuildEnv has no way to
// tell a secret apart (spec §13, R3).
type EnvInput struct {
	Phase, Ext, ExtVersion, PrevVersion                     string
	ExtDir, EtcDir, StateDir, Root, ConfigFile, SecretsFile string
	Reason, Action, Output                                  string
	ChangedKeys                                             []string
	LockFD                                                  int
	Values                                                  map[string]string
}

// BuildEnv returns the complete, clean environment of a hook or action
// (spec §6.3, §7.2): PATH and LANG, the CREAM_* variables in a fixed order,
// then domain keys sorted by name. A domain key cannot override PATH, LANG,
// HOME, PWD or CREAM_*, and names or values that cannot be passed through
// execve (not [A-Z][A-Z0-9_]*, NUL in the value) are dropped; the manifest
// validator rejects such keys earlier.
func BuildEnv(in EnvInput) []string {
	env := []string{
		"PATH=" + fixedPath,
		"LANG=" + fixedLang,
		"CREAM_PHASE=" + in.Phase,
		"CREAM_EXT=" + in.Ext,
		"CREAM_EXT_VERSION=" + in.ExtVersion,
	}
	optional := func(name, v string) {
		if v != "" {
			env = append(env, name+"="+v)
		}
	}
	optional("CREAM_PREV_VERSION", in.PrevVersion)
	env = append(env,
		"CREAM_EXT_DIR="+in.ExtDir,
		"CREAM_ETC_DIR="+in.EtcDir,
		"CREAM_STATE_DIR="+in.StateDir,
		"CREAM_ROOT="+in.Root,
		"CREAM_CONFIG_FILE="+in.ConfigFile,
		"CREAM_SECRETS_FILE="+in.SecretsFile,
	)
	optional("CREAM_REASON", in.Reason)
	optional("CREAM_CHANGED_KEYS", strings.Join(in.ChangedKeys, " "))
	optional("CREAM_ACTION", in.Action)
	optional("CREAM_OUTPUT", in.Output)
	if in.LockFD > 0 {
		env = append(env, "CREAM_LOCK_FD="+strconv.Itoa(in.LockFD))
	}
	keys := make([]string, 0, len(in.Values))
	for k, v := range in.Values {
		if passableKey(k) && !strings.ContainsRune(v, 0) {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	for _, k := range keys {
		env = append(env, k+"="+in.Values[k])
	}
	return env
}

// passableKey reports whether a domain key may enter the environment.
func passableKey(k string) bool {
	switch {
	case k == "" || k[0] < 'A' || k[0] > 'Z':
		return false
	case k == "PATH" || k == "LANG" || k == "HOME" || k == "PWD" || strings.HasPrefix(k, "CREAM_"):
		return false
	}
	for i := 1; i < len(k); i++ {
		c := k[i]
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}
