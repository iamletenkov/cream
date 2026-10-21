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
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/iamletenkov/cream/internal/envfile"
)

// FamilyOpenWrt is the OpenWrt layout (spec OpenWrt §4.1): /var is a tmpfs
// there, so the registry, the state and the cache live under /opt/cream/var,
// configure runs from procd init scripts. The empty family is the Debian
// layout of spec §5.
const FamilyOpenWrt = "openwrt"

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
	// Family is the layout family: "" (deb, spec §5) or FamilyOpenWrt.
	// Detect reads it from the os-release under Root.
	Family string
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
	l.Family = DetectFamily(l.Root)
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

// osReleaseFiles are the os-release paths relative to the root, in order.
var osReleaseFiles = []string{"etc/os-release", "usr/lib/os-release"}

// maxOSRelease bounds what is read from an os-release.
const maxOSRelease = 64 << 10

// DetectFamily returns the layout family of the system under root:
// FamilyOpenWrt when ID or a word of ID_LIKE of its os-release is openwrt or
// lede, else "" — the Debian layout, also without a readable os-release (the
// resolver refuses a foreign family by platform.family on its own). The file
// is opened through os.OpenInRoot: a symlink does not lead out of root.
func DetectFamily(root string) string {
	if root == "" {
		root = "/"
	}
	for _, rel := range osReleaseFiles {
		f, err := os.OpenInRoot(root, rel)
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(f, maxOSRelease))
		_ = f.Close()
		if err != nil {
			return ""
		}
		osr, err := envfile.Parse(data)
		if err != nil {
			return ""
		}
		id, _ := osr.Get("ID")
		like, _ := osr.Get("ID_LIKE")
		for _, w := range append([]string{id}, strings.Fields(like)...) {
			if w == "openwrt" || w == "lede" {
				return FamilyOpenWrt
			}
		}
		return ""
	}
	return ""
}

// OpenWrt reports the OpenWrt layout.
func (l Layout) OpenWrt() bool { return l.Family == FamilyOpenWrt }

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
	// optVar is varLib of the OpenWrt layout: on disk, not in the tmpfs /var.
	optVar = "opt/cream/var"
)

// varDir is the registry and state root: /var/lib/cream, /opt/cream/var on OpenWrt.
func (l Layout) varDir() string {
	if l.OpenWrt() {
		return optVar
	}
	return varLib
}

// runDir is the per-boot directory: /run, /var/run on OpenWrt (tmpfs there).
func (l Layout) runDir() string {
	if l.OpenWrt() {
		return "var/run"
	}
	return "run"
}

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
func (l Layout) StateDir(ext string) string { return l.sys(l.varDir(), "ext", ext) }

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

// SourcesLock is the observed source revisions: <CacheDir>/sources.lock with
// a cache override (ruling Р12: a bisquite build with its own cache leaves the
// machine's lock alone), else XDG state in user mode, else /var/lib/cream/sources.lock.
func (l Layout) SourcesLock() string {
	if l.CacheDir != "" {
		return filepath.Join(filepath.Clean(l.CacheDir), "sources.lock")
	}
	if l.userMode() {
		return filepath.Join(l.StateHome, "cream", "sources.lock")
	}
	return l.sys(l.varDir(), "sources.lock")
}

// CacheRoot is the cache root: the override, else XDG cache in user mode, else /var/cache/cream.
func (l Layout) CacheRoot() string {
	switch {
	case l.CacheDir != "":
		return filepath.Clean(l.CacheDir)
	case l.userMode():
		return filepath.Join(l.CacheHome, "cream")
	default:
		if l.OpenWrt() {
			return l.sys(optVar, "cache")
		}
		return l.sys("var/cache/cream")
	}
}

// SourceCache is the cache of one source, <CacheRoot>/sources/<name>.
func (l Layout) SourceCache(name string) string {
	return filepath.Join(l.CacheRoot(), "sources", name)
}

// InstalledDir is the registry directory, /var/lib/cream/installed.
func (l Layout) InstalledDir() string { return l.sys(l.varDir(), "installed") }

// InstalledFile is one registry record, /var/lib/cream/installed/<ext>.json.
func (l Layout) InstalledFile(ext string) string {
	return l.sys(l.varDir(), "installed", ext+".json")
}

// ConfiguredDir holds the configure run marks, /var/lib/cream/configured.
func (l Layout) ConfiguredDir() string { return l.sys(l.varDir(), "configured") }

// ConfiguredFile is the configure run mark of one extension (spec
// bisquite-bundled-cream §10.7), /var/lib/cream/configured/<ext>.json.
func (l Layout) ConfiguredFile(ext string) string {
	return l.sys(l.varDir(), "configured", ext+".json")
}

// MachineID is /etc/machine-id; the configure mark keeps only its sha256.
func (l Layout) MachineID() string { return l.sys("etc/machine-id") }

// History is the operation journal, /var/lib/cream/history.jsonl.
func (l Layout) History() string { return l.sys(l.varDir(), "history.jsonl") }

// LockDir holds the flock files, /var/lib/cream/lock.
func (l Layout) LockDir() string { return l.sys(l.varDir(), "lock") }

// GlobalLock is /var/lib/cream/lock/global.
func (l Layout) GlobalLock() string { return l.sys(l.varDir(), "lock", "global") }

// ExtLock is the domain lock of one extension, /var/lib/cream/lock/<ext>.
func (l Layout) ExtLock(ext string) string { return l.sys(l.varDir(), "lock", ext) }

// Incoming is the build-phase drop directory, /var/lib/cream/incoming/<id>.
func (l Layout) Incoming(id string) string { return l.sys(l.varDir(), "incoming", id) }

// ImageLabels is the composition of the image for its OCI labels (spec
// bisquite-bundled-cream §8.2), /etc/image-labels.d/cream.json.
func (l Layout) ImageLabels() string { return l.sys("etc/image-labels.d", "cream.json") }

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
func (l Layout) RunDir() string { return l.sys(l.runDir(), "cream") }

// RunReason is the CREAM_REASON marker for configure, /run/cream/<ext>.reason.
func (l Layout) RunReason(ext string) string { return l.sys(l.runDir(), "cream", ext+".reason") }

// RunResult is the outcome of the last configure run of the unit,
// /run/cream/configure/<ext>.result: what `cream configure --wait` reports
// after a restart of cream-configure@<ext>.
func (l Layout) RunResult(ext string) string {
	return l.sys(l.runDir(), "cream", "configure", ext+".result")
}

// RebootRequired is /run/reboot-required.
func (l Layout) RebootRequired() string { return l.sys(l.runDir(), "reboot-required") }

// PolicyRcD is /usr/sbin/policy-rc.d.
func (l Layout) PolicyRcD() string { return l.sys("usr/sbin/policy-rc.d") }

// OSRelease is /etc/os-release.
func (l Layout) OSRelease() string { return l.sys("etc/os-release") }

// SystemdRunning is /run/systemd/system: it exists only while systemd is PID 1.
func (l Layout) SystemdRunning() string { return l.sys("run/systemd/system") }

// PID1Comm is /proc/1/comm, the name of PID 1: cream decides its phase by it (§10.1).
func (l Layout) PID1Comm() string { return l.sys("proc/1/comm") }

// CloudUserUnit is the unit that pulls the configure instances of the
// cloud-user stage, /usr/lib/systemd/system/cream-configure-cloud-user.service.
func (l Layout) CloudUserUnit() string { return l.sys(unitDir, "cream-configure-cloud-user.service") }

// HookTmp is TMPDIR of hooks on OpenWrt, /opt/cream/var/tmp: /tmp there is
// RAM (spec OpenWrt Р2). On the Debian layout hooks keep the system /tmp.
func (l Layout) HookTmp() string { return l.sys(optVar, "tmp") }

// ApkBuildConfig is APK_CONFIG of hooks in the build phase on OpenWrt,
// /opt/cream/var/apk-build.conf: the image's apk config plus preserve-env.
func (l Layout) ApkBuildConfig() string { return l.sys(optVar, "apk-build.conf") }

// InitScript is a procd init script, /etc/init.d/<name>.
func (l Layout) InitScript(name string) string { return l.sys("etc/init.d", name) }

// RcLink is a start link of procd, /etc/rc.d/<link> (S<NN><name>).
func (l Layout) RcLink(link string) string { return l.sys("etc/rc.d", link) }

// StageDir lists the configure instances of one stage on OpenWrt,
// /opt/cream/var/stages/<stage>: the init script of the stage runs them.
func (l Layout) StageDir(stage string) string { return l.sys(optVar, "stages", stage) }

// StageMark enables the configure of ext at stage on OpenWrt,
// /opt/cream/var/stages/<stage>/<ext>.
func (l Layout) StageMark(stage, ext string) string { return l.sys(optVar, "stages", stage, ext) }

// Dir is a directory `cream system init` creates, with its mode.
type Dir struct {
	Path string
	Mode fs.FileMode
}

// InitDirs are the cream directories of the layout with their modes (spec
// §5): the extension trees, /etc/cream, the registry, the state, the locks
// and the build drop; on OpenWrt also TMPDIR of hooks (sticky, as /tmp). The
// init system adds its own (the unit directory, the stage lists).
func (l Layout) InitDirs() []Dir {
	dirs := []Dir{
		{filepath.Dir(l.ExtTree("x")), 0o755},
		{filepath.Dir(l.EtcDir("x")), 0o755},
		{filepath.Dir(l.InstalledDir()), 0o755},
		{l.InstalledDir(), 0o755},
		{filepath.Dir(l.StateDir("x")), 0o755},
		{l.LockDir(), 0o700},
		{filepath.Dir(l.Incoming("x")), 0o700},
	}
	if l.OpenWrt() {
		dirs = append(dirs, Dir{l.HookTmp(), fs.ModeSticky | 0o777})
	}
	return dirs
}
