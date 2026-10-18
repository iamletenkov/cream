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
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
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
// Arches may be given as uname -m (x86_64, aarch64): Resolve canonicalizes.
type Host struct {
	Phase, TargetArch, HostArch, Family string
	OSID, OSVersion                     string
	HasCommand                          func(string) bool
	AcceptRisk                          bool
}

// Probe detects the host: os-release under Layout.Root (/etc/os-release,
// then /usr/lib/os-release), commands through LookPath (nil — not checked)
// and Arch, the machine architecture (runtime.GOARCH or uname -m).
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
// Family is "deb" when ID or ID_LIKE names debian.
func (p Probe) Detect(phase string) (Host, error) {
	if err := checkPhase(phase); err != nil {
		return Host{}, err
	}
	arch := canonArch(p.Arch)
	h := Host{Phase: phase, TargetArch: arch, HostArch: arch}
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
	if id == "debian" || slices.Contains(strings.Fields(like), "debian") {
		h.Family = FamilyDeb
	}
	return h, nil
}

// readOSRelease returns the parsed os-release, nil when neither file exists.
func (p Probe) readOSRelease() (*envfile.File, error) {
	etc := p.Layout.OSRelease()
	usr := filepath.Join(filepath.Dir(filepath.Dir(etc)), "usr", "lib", "os-release")
	for _, path := range []string{etc, usr} {
		data, err := os.ReadFile(path)
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
