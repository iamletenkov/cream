package runner

import (
	"slices"
	"strconv"
	"strings"

	"github.com/iamletenkov/cream/internal/manifest"
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
// LockFD <= 0, and LockMode with it) are left out of the environment.
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
	// LockMode is the mode the lock behind LockFD is held in: "exclusive" or
	// "shared"; anything else is passed as "shared" (ruling Р19).
	LockMode string
	// TmpDir is TMPDIR of the hook ("" — not set): on OpenWrt a directory on
	// disk, /tmp there is RAM (spec OpenWrt Р2).
	TmpDir string
	// InstRoot is IPKG_INSTROOT ("" — not set): "/" in the build phase on
	// OpenWrt, so package scripts run by `apk add` enable services without
	// starting them and leave uci-defaults to the first boot — OpenWrt's own
	// offline-image mode, the counterpart of policy-rc.d (ADR-0005).
	InstRoot string
	// ApkConfig is APK_CONFIG ("" — not set): with InstRoot, an apk
	// configuration with preserve-env, so apk 3 passes IPKG_INSTROOT on to
	// package scripts (it clears their environment otherwise).
	ApkConfig string
	Values    map[string]string
	// Build are the values of `scope: build` keys for this hook run only
	// (ADR-0004, spec 2026-10-21 §10.9): config.BuildValues of the install or
	// upgrade operation, never stored. They join Values in the environment; a
	// name Values already has keeps the domain value. Build may hold secrets:
	// the caller masks them in output through Spec.Secrets.
	Build map[string]string
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
	optional("TMPDIR", in.TmpDir)
	optional("IPKG_INSTROOT", in.InstRoot)
	optional("APK_CONFIG", in.ApkConfig)
	if in.LockFD > 0 {
		mode := "shared"
		if in.LockMode == "exclusive" {
			mode = "exclusive"
		}
		env = append(env, "CREAM_LOCK_FD="+strconv.Itoa(in.LockFD), "CREAM_LOCK_MODE="+mode)
	}
	vals := make(map[string]string, len(in.Values)+len(in.Build))
	for k, v := range in.Build {
		vals[k] = v
	}
	for k, v := range in.Values {
		vals[k] = v
	}
	// A domain key never overrides what cream set itself (TMPDIR, IPKG_INSTROOT).
	taken := make(map[string]bool, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		taken[name] = true
	}
	keys := make([]string, 0, len(vals))
	for k, v := range vals {
		if passableKey(k) && !taken[k] && !strings.ContainsRune(v, 0) {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	for _, k := range keys {
		env = append(env, k+"="+vals[k])
	}
	return env
}

// passableKey reports whether a domain key may enter the environment: the
// same rule as the manifest validator's (manifest.ValidKey), so a key the
// manifest refuses can never reach a hook through a hand-edited config.
func passableKey(k string) bool {
	return manifest.ValidKey(k)
}
