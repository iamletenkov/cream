// Package resolve is cream's resolver (spec §9.4, §12.4): the platform and
// applicability checks of one extension against the host, the graph of
// "installed ∪ requested" (depends with version ranges, requires/provides
// capabilities, conflicts by name and by capability, ownership of files),
// the topological install order and the lineage layer of every requested
// extension.
//
// It is pure: it reads nothing but /etc/os-release (DetectHost) and never
// touches the catalog. A missing dependency is 67 deps.missing with the
// missing names and ranges in the details; looking up candidates in the
// sources is the caller's job (--with-deps is reserved, Р1).
package resolve

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/envfile"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/paths"
)

// Phases of an installation (§2).
const (
	PhaseBuild = string(manifest.PhaseBuild)
	PhaseLive  = string(manifest.PhaseLive)
)

// FamilyDeb is the only package family cream/v1 knows.
const FamilyDeb = "deb"

// Host is what an extension is checked against. An empty Family, OSID or
// TargetArch means "unknown here" and that check is skipped (cream resolve on
// the bisquite host knows the target arch and family, not the guest's OS;
// the guest checks again); HasCommand == nil skips platform.commands.
// Detected marks a host probed on the machine itself (Detect): there an empty
// Family means os-release is absent, and platform.family is 68, not skipped
// (Р23). Arches may be given as uname -m (x86_64, aarch64): Resolve
// canonicalizes.
type Host struct {
	Phase, TargetArch, HostArch, Family string
	OSID, OSVersion                     string
	HasCommand                          func(string) bool
	AcceptRisk                          bool
	Detected                            bool
}

// Probe detects the host: os-release under Layout.Root (/etc/os-release,
// then /usr/lib/os-release), commands through LookPath (nil — not checked)
// and Arch, the machine architecture (runtime.GOARCH or uname -m).
// LookPath searches the PATH of the cream process: under a foreign --root it
// still answers for the host's commands, not for the tree under the root.
type Probe struct {
	Layout   paths.Layout
	LookPath func(string) (string, error)
	Arch     string
}

// DetectHost is Probe{l, lookPath, runtime.GOARCH}.Detect(phase): cream is a
// static binary per architecture, so its GOARCH is the machine's.
func DetectHost(l paths.Layout, phase string, lookPath func(string) (string, error)) (Host, error) {
	return Probe{Layout: l, LookPath: lookPath, Arch: runtime.GOARCH}.Detect(phase)
}

// Detect returns the host for phase (build or live; anything else is 64
// usage.phase). The target arch is the machine's own. A missing os-release
// leaves Family, OSID and OSVersion empty; an unreadable one is 74 state.io.
// With os-release present Family is never empty (familyOf, Р23).
func (p Probe) Detect(phase string) (Host, error) {
	if err := checkPhase(phase); err != nil {
		return Host{}, err
	}
	arch := canonArch(p.Arch)
	h := Host{Phase: phase, TargetArch: arch, HostArch: arch, Detected: true}
	if p.LookPath != nil {
		look := p.LookPath
		h.HasCommand = func(c string) bool { _, err := look(c); return err == nil }
	}
	osr, err := p.readOSRelease()
	if err != nil {
		return Host{}, err
	}
	if osr == nil {
		return h, nil
	}
	id, _ := osr.Get("ID")
	like, _ := osr.Get("ID_LIKE")
	h.OSID = id
	h.OSVersion, _ = osr.Get("VERSION_ID")
	h.Family = familyOf(id, like)
	return h, nil
}

// familyLike maps an os-release ID (or an ID_LIKE word) to its package family.
var familyLike = map[string]string{
	"debian": FamilyDeb,
	"fedora": "rpm", "rhel": "rpm", "centos": "rpm", "suse": "rpm", "opensuse": "rpm",
	"alpine": "apk", "openwrt": "openwrt",
}

// familyOf is the package family of an os-release: by ID, then by ID_LIKE,
// else the ID itself ("linux" when ID is absent, as os-release(5) says).
// Only "deb" is a family cream/v1 installs; the rest are named for the 68.
func familyOf(id, like string) string {
	for _, w := range append([]string{id}, strings.Fields(like)...) {
		if f, ok := familyLike[w]; ok {
			return f
		}
	}
	if id == "" {
		return "linux"
	}
	return id
}

// osReleaseFiles are the os-release paths relative to the root, in order.
var osReleaseFiles = []string{"etc/os-release", "usr/lib/os-release"}

// readOSRelease returns the parsed os-release, nil when neither file exists.
// The files are opened through os.OpenInRoot: a symlink may not lead out of
// Layout.Root (a foreign --root does not read the host's os-release).
func (p Probe) readOSRelease() (*envfile.File, error) {
	root := p.Layout.Root
	if root == "" {
		root = "/"
	}
	for _, rel := range osReleaseFiles {
		path := filepath.Join(root, rel)
		data, err := readInRoot(root, rel)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, cerr.Wrap(err, cerr.ExitState, "state.io", "чтение %s", path)
		}
		f, err := envfile.Parse(data)
		if err != nil {
			return nil, cerr.Wrap(err, cerr.ExitState, "state.io", "разбор %s", path)
		}
		return f, nil
	}
	return nil, nil
}

func readInRoot(root, rel string) ([]byte, error) {
	f, err := os.OpenInRoot(root, rel)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}

func checkPhase(phase string) error {
	if phase != PhaseBuild && phase != PhaseLive {
		e := cerr.New(cerr.ExitUsage, "usage.phase", "фаза %q: ожидается build или live", phase)
		e.Details = map[string]any{"phase": phase}
		return e
	}
	return nil
}

// canonArch canonicalizes a known arch (x86_64 → amd64), keeps others as is.
func canonArch(a string) string {
	if c, ok := manifest.CanonArch(a); ok {
		return c
	}
	return a
}
