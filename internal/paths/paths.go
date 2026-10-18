// Package paths is the on-machine layout of cream (spec §5).
//
// Every path is computed from a Layout, never hard-coded at the call site:
// --root (CREAM_ROOT) moves the whole tree under another directory for tests
// and fixtures, and a non-root build host keeps its sources, cache and
// sources.lock in XDG directories (user mode).
//
// Extension names passed to the methods must already be validated
// (manifest name rules); paths does not check them.
package paths

import (
	"path/filepath"
	"strings"
)

// Layout is the resolved set of base directories.
//
// A literal Layout{Root: dir} is valid and means the system layout under dir;
// the zero Layout is the system layout under "/".
type Layout struct {
	// Root is the directory every system path lives under; "" or "/" is the real root.
	Root string
	// User is the XDG mode for sources.yaml, the source cache and sources.lock
	// (non-root build host). Only meaningful with Root "/" and all three XDG
	// homes set (Detect fills them); otherwise the system paths are used.
	User bool
	// ConfigHome, CacheHome, StateHome are the XDG base directories used in user mode.
	ConfigHome, CacheHome, StateHome string
	// SourcesFile and CacheDir override sources.yaml and the cache root
	// (--sources/--cache or CREAM_SOURCES/CREAM_CACHE); used as is, not under Root.
	SourcesFile, CacheDir string
}

// Detect resolves the layout. Flags beat the environment:
// rootFlag > CREAM_ROOT > "/", sourcesFlag > CREAM_SOURCES, cacheFlag > CREAM_CACHE.
// User mode is on when euid != 0, the root is "/" and a home for XDG is known.
// Relative paths are returned as given: the caller rejects a relative Root.
func Detect(env func(string) string, euid int, rootFlag, sourcesFlag, cacheFlag string) Layout {
	l := Layout{
		Root:        firstNonEmpty(rootFlag, env("CREAM_ROOT"), "/"),
		SourcesFile: firstNonEmpty(sourcesFlag, env("CREAM_SOURCES")),
		CacheDir:    firstNonEmpty(cacheFlag, env("CREAM_CACHE")),
	}
	l.Root = filepath.Clean(l.Root)
	if euid == 0 || l.Root != "/" {
		return l
	}
	home := absOrEmpty(env("HOME"))
	l.ConfigHome = xdg(env, "XDG_CONFIG_HOME", home, ".config")
	l.CacheHome = xdg(env, "XDG_CACHE_HOME", home, ".cache")
	l.StateHome = xdg(env, "XDG_STATE_HOME", home, ".local/state")
	l.User = l.ConfigHome != "" && l.CacheHome != "" && l.StateHome != ""
	if !l.User {
		l.ConfigHome, l.CacheHome, l.StateHome = "", "", ""
	}
	return l
}

// xdg returns $name if it is absolute (the XDG spec ignores relative values),
// otherwise home/fallback, or "" without a home.
func xdg(env func(string) string, name, home, fallback string) string {
	if v := absOrEmpty(env(name)); v != "" {
		return v
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, fallback)
}

func absOrEmpty(p string) string {
	if !filepath.IsAbs(p) {
		return ""
	}
	return filepath.Clean(p)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// userMode is User with every XDG home known: a literal Layout{User: true}
// never yields relative paths.
func (l Layout) userMode() bool {
	return l.User && filepath.IsAbs(l.ConfigHome) && filepath.IsAbs(l.CacheHome) && filepath.IsAbs(l.StateHome)
}

// NeedsRoot reports whether root and file-permission checks apply (ruling Р9):
// only on the real root; with another --root they are off.
func (l Layout) NeedsRoot() bool { return l.root() == "/" }

func (l Layout) root() string {
	if l.Root == "" {
		return "/"
	}
	return l.Root
}

// sys returns an absolute system path under Root.
func (l Layout) sys(parts ...string) string {
	return filepath.Join(append([]string{l.root()}, parts...)...)
}

const (
	optExt    = "opt/cream/ext"
	etcCream  = "etc/cream"
	varLib    = "var/lib/cream"
	unitDir   = "usr/lib/systemd/system"
	etcSystem = "etc/systemd/system"
)

// Bin is the cream binary, /usr/bin/cream.
func (l Layout) Bin() string { return l.sys("usr/bin/cream") }

// ExtTree is the installed extension tree, /opt/cream/ext/<ext>.
func (l Layout) ExtTree(ext string) string { return l.sys(optExt, ext) }

// ExtNew is the staging tree next to ExtTree, renamed into place on install/upgrade.
func (l Layout) ExtNew(ext string) string { return l.sys(optExt, "."+ext+".new") }

// ExtPrev is the previous tree kept next to ExtTree while an upgrade runs.
func (l Layout) ExtPrev(ext string) string { return l.sys(optExt, "."+ext+".prev") }

// EtcDir is the extension settings directory, /etc/cream/<ext>.
func (l Layout) EtcDir(ext string) string { return l.sys(etcCream, ext) }

// ConfigFile is the non-secret EnvironmentFile, /etc/cream/<ext>/config.
func (l Layout) ConfigFile(ext string) string { return l.sys(etcCream, ext, "config") }

// SecretsFile is the secret EnvironmentFile, /etc/cream/<ext>/secrets.
func (l Layout) SecretsFile(ext string) string { return l.sys(etcCream, ext, "secrets") }

// StateDir is the extension's own state, /var/lib/cream/ext/<ext>.
func (l Layout) StateDir(ext string) string { return l.sys(varLib, "ext", ext) }

// Sources is sources.yaml: the override, else XDG config in user mode, else /etc/cream/sources.yaml.
func (l Layout) Sources() string {
	switch {
	case l.SourcesFile != "":
		return l.SourcesFile
	case l.userMode():
		return filepath.Join(l.ConfigHome, "cream", "sources.yaml")
	default:
		return l.sys(etcCream, "sources.yaml")
	}
}

// SourcesLock is the observed source revisions: XDG state in user mode, else /var/lib/cream/sources.lock.
func (l Layout) SourcesLock() string {
	if l.userMode() {
		return filepath.Join(l.StateHome, "cream", "sources.lock")
	}
	return l.sys(varLib, "sources.lock")
}

// CacheRoot is the cache root: the override, else XDG cache in user mode, else /var/cache/cream.
func (l Layout) CacheRoot() string {
	switch {
	case l.CacheDir != "":
		return filepath.Clean(l.CacheDir)
	case l.userMode():
		return filepath.Join(l.CacheHome, "cream")
	default:
		return l.sys("var/cache/cream")
	}
}

// SourceCache is the cache of one source, <CacheRoot>/sources/<name>.
func (l Layout) SourceCache(name string) string {
	return filepath.Join(l.CacheRoot(), "sources", name)
}

// InstalledDir is the registry directory, /var/lib/cream/installed.
func (l Layout) InstalledDir() string { return l.sys(varLib, "installed") }

// InstalledFile is one registry record, /var/lib/cream/installed/<ext>.json.
func (l Layout) InstalledFile(ext string) string {
	return l.sys(varLib, "installed", ext+".json")
}

// History is the operation journal, /var/lib/cream/history.jsonl.
func (l Layout) History() string { return l.sys(varLib, "history.jsonl") }

// LockDir holds the flock files, /var/lib/cream/lock.
func (l Layout) LockDir() string { return l.sys(varLib, "lock") }

// GlobalLock is /var/lib/cream/lock/global.
func (l Layout) GlobalLock() string { return l.sys(varLib, "lock", "global") }

// ExtLock is the domain lock of one extension, /var/lib/cream/lock/<ext>.
func (l Layout) ExtLock(ext string) string { return l.sys(varLib, "lock", ext) }

// Incoming is the build-phase drop directory, /var/lib/cream/incoming/<id>.
func (l Layout) Incoming(id string) string { return l.sys(varLib, "incoming", id) }

// UnitTemplate is the configure unit template, /usr/lib/systemd/system/cream-configure@.service.
func (l Layout) UnitTemplate() string { return l.sys(unitDir, "cream-configure@.service") }

// DropIn is the stage drop-in of one configure instance,
// /etc/systemd/system/cream-configure@<ext>.service.d/10-stage.conf.
func (l Layout) DropIn(ext string) string {
	return l.sys(etcSystem, "cream-configure@"+ext+".service.d", "10-stage.conf")
}

// WantsDir is /etc/systemd/system/<target>.wants.
func (l Layout) WantsDir(target string) string {
	return l.sys(etcSystem, strings.TrimSuffix(target, "/")+".wants")
}

// RunDir is /run/cream.
func (l Layout) RunDir() string { return l.sys("run/cream") }

// RunReason is the CREAM_REASON marker for configure, /run/cream/<ext>.reason.
func (l Layout) RunReason(ext string) string { return l.sys("run/cream", ext+".reason") }

// RebootRequired is /run/reboot-required.
func (l Layout) RebootRequired() string { return l.sys("run/reboot-required") }

// PolicyRcD is /usr/sbin/policy-rc.d.
func (l Layout) PolicyRcD() string { return l.sys("usr/sbin/policy-rc.d") }

// OSRelease is /etc/os-release.
func (l Layout) OSRelease() string { return l.sys("etc/os-release") }

// SystemdRunning is /run/systemd/system: it exists only while systemd is PID 1.
func (l Layout) SystemdRunning() string { return l.sys("run/systemd/system") }

// CloudFinalUnit is /usr/lib/systemd/system/cloud-final.service.
func (l Layout) CloudFinalUnit() string { return l.sys(unitDir, "cloud-final.service") }
