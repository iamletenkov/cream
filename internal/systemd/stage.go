package systemd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/fsx"
	"github.com/iamletenkov/cream/internal/paths"
)

// Error codes of this package.
const (
	// CodeStageUnsupported is a stage the stage table does not know.
	CodeStageUnsupported = "stage.unsupported"
	// CodeUnitInvalid is an after/before/wants entry that is not a unit name.
	CodeUnitInvalid = "unit.invalid"
)

// StageSpec is the configure hook's stage and extra ordering from the manifest.
type StageSpec struct {
	Stage                string
	After, Before, Wants []string
}

// stageDef is one row of the stage table (§6.4).
type stageDef struct {
	// target picks the .wants target; it may look at the machine (cloud-user).
	target func(l paths.Layout) string
	// noDefaultDeps writes DefaultDependencies=no (early: before sysinit.target).
	noDefaultDeps bool
	// after, before, wants, conflicts are the stage's own ordering, written
	// before the manifest's.
	after, before, wants, conflicts []string
}

// stages is the stage table: system since cream 1.0; early, cloud-user and
// graphical since 1.1.0.
func stages() map[string]stageDef {
	fixed := func(t string) func(paths.Layout) string { return func(paths.Layout) string { return t } }
	return map[string]stageDef{
		// Before the network: the firewall is up before any interface.
		// Without default dependencies the shutdown ones are written by hand
		// (cream 1.1.1), as systemd adds them to a unit with defaults.
		"early": {
			target:        fixed("sysinit.target"),
			noDefaultDeps: true,
			after:         []string{"local-fs.target"},
			before:        []string{"network-pre.target", "sysinit.target", "shutdown.target"},
			wants:         []string{"network-pre.target"},
			conflicts:     []string{"shutdown.target"},
		},
		"system": {
			target: fixed("multi-user.target"),
			after:  []string{"network.target"},
		},
		// After cloud-init created the user. cloud-final is ordered after
		// multi-user.target, so a link from multi-user.target.wants would be an
		// ordering cycle, and cloud-init.target is not reached when cloud-init
		// is installed but disabled. The instance is pulled by CloudUserUnit
		// (in multi-user.target.wants, no ordering against the instances):
		// with cloud-init running the instance waits for cloud-final, without
		// it After= has nothing to wait for (cream 1.1.1).
		"cloud-user": {
			target: fixed(CloudUserUnit),
			after:  []string{"cloud-final.service"},
		},
		// After the display manager: graphical.target, not multi-user, so the
		// DM is not pulled before multi-user.target is reached; after
		// multi-user.target too, so on a machine without a DM the stage does
		// not start before the system services are up.
		"graphical": {
			target: fixed("graphical.target"),
			after:  []string{"multi-user.target", "display-manager.service"},
		},
	}
}

// knownTargets is every target a stage may link into: Enable and Disable
// clear links from all of them, so a stage or machine change leaves no stray.
// cloud-init.target is where cream 1.1.0 linked cloud-user.
func knownTargets() []string {
	return []string{"multi-user.target", "sysinit.target", "cloud-init.target", "graphical.target", CloudUserUnit}
}

// stageKey is the systemd-ignored X- key that records the stage in the
// drop-in; Init reads it back to recompute targets.
const stageKey = "X-CreamStage="

// reUnit matches a unit name that is safe to write into a drop-in line.
var reUnit = regexp.MustCompile(`^[A-Za-z0-9:_.\\@-]+\.(service|socket|target|mount|automount|path|timer|device|swap|slice|scope)$`)

func lookupStage(stage string) (stageDef, error) {
	def, ok := stages()[stage]
	if !ok {
		e := cerr.New(cerr.ExitData, CodeStageUnsupported, "стадия configure %q неизвестна (early, system, cloud-user, graphical)", stage)
		e.Details = map[string]any{"stage": stage}
		return stageDef{}, e
	}
	return def, nil
}

// Target is the .wants target of stage: what Enable links into and Init
// recomputes.
func Target(l paths.Layout, stage string) (string, error) {
	def, err := lookupStage(stage)
	if err != nil {
		return "", err
	}
	return def.target(l), nil
}

// Enable writes the stage drop-in of ext and links the instance into the
// stage target's .wants directory; it returns the target. Repeating it is a
// no-op; a link left in another target is removed.
func Enable(l paths.Layout, ext string, st StageSpec) (target string, err error) {
	def, err := lookupStage(st.Stage)
	if err != nil {
		return "", err
	}
	body, err := dropIn(ext, st, def)
	if err != nil {
		return "", err
	}
	p := l.DropIn(ext)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", fmt.Errorf("включение %s: %w", UnitName(ext), err)
	}
	if err := fsx.WriteAtomic(p, body, 0o644); err != nil {
		return "", fmt.Errorf("включение %s: %w", UnitName(ext), err)
	}
	target = def.target(l)
	if err := link(l, ext, target); err != nil {
		return "", err
	}
	if err := dropPuller(l); err != nil {
		return "", fmt.Errorf("включение %s: %w", UnitName(ext), err)
	}
	return target, nil
}

// dropIn renders 10-stage.conf: the stage's own ordering first, then the
// manifest's in its order, duplicates dropped.
func dropIn(ext string, st StageSpec, def stageDef) ([]byte, error) {
	for _, list := range [][]string{st.After, st.Before, st.Wants} {
		for _, u := range list {
			if !reUnit.MatchString(u) {
				e := cerr.New(cerr.ExitData, CodeUnitInvalid, "%q: не имя юнита systemd", u)
				e.Details = map[string]any{"unit": u}
				return nil, e
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Generated by cream: stage order of %s. Do not edit.\n[Unit]\n%s%s\n", UnitName(ext), stageKey, st.Stage)
	if def.noDefaultDeps {
		b.WriteString("DefaultDependencies=no\n")
	}
	line := func(key string, units []string) {
		if len(units) > 0 {
			fmt.Fprintf(&b, "%s=%s\n", key, strings.Join(units, " "))
		}
	}
	line("Conflicts", def.conflicts)
	line("After", dedup(append(slices.Clone(def.after), st.After...)))
	line("Before", dedup(append(slices.Clone(def.before), st.Before...)))
	line("Wants", dedup(append(slices.Clone(def.wants), st.Wants...)))
	return []byte(b.String()), nil
}

func dedup(in []string) []string {
	var out []string
	for _, s := range in {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// link puts the instance link into target's .wants and removes it from the
// other known targets; the caller drops the CloudUserUnit link (dropPuller)
// once its .wants may have become empty.
func link(l paths.Layout, ext, target string) error {
	for _, t := range knownTargets() {
		if t == target {
			continue
		}
		if err := unlink(filepath.Join(l.WantsDir(t), UnitName(ext))); err != nil {
			return fmt.Errorf("включение %s: %w", UnitName(ext), err)
		}
	}
	if target == CloudUserUnit {
		if err := enablePuller(l); err != nil {
			return fmt.Errorf("включение %s: %w", UnitName(ext), err)
		}
	}
	if err := symlink(filepath.Join(l.WantsDir(target), UnitName(ext)), templateTarget); err != nil {
		return fmt.Errorf("включение %s: %w", UnitName(ext), err)
	}
	return nil
}

// symlink makes p a link to dst atomically; an equal link is left alone.
func symlink(p, dst string) error {
	if cur, err := os.Readlink(p); err == nil && cur == dst {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".cream-tmp"
	_ = os.Remove(tmp)
	if err := os.Symlink(dst, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// enablePuller writes CloudUserUnit (the .deb ships it too; the build phase
// may run a cream without the package) and links it into
// multi-user.target.wants.
func enablePuller(l paths.Layout) error {
	if err := writeUnit(l.CloudUserUnit(), cloudUserUnit); err != nil {
		return err
	}
	return symlink(filepath.Join(l.WantsDir("multi-user.target"), CloudUserUnit), cloudUserLink)
}

// dropPuller unlinks CloudUserUnit from multi-user.target.wants once its own
// .wants holds no instance; the unit file stays (it belongs to the package).
// The emptiness check is the removal of the .wants directory itself: a
// directory not empty (an instance, possibly linked by a parallel install
// right now) is the normal case, and the link stays.
func dropPuller(l paths.Layout) error {
	err := os.Remove(l.WantsDir(CloudUserUnit))
	switch {
	case errors.Is(err, syscall.ENOTEMPTY) || errors.Is(err, syscall.EEXIST):
		return nil
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%s: %w", CloudUserUnit, err)
	}
	if err := unlink(filepath.Join(l.WantsDir("multi-user.target"), CloudUserUnit)); err != nil {
		return fmt.Errorf("%s: %w", CloudUserUnit, err)
	}
	return nil
}

// unlink removes p only if it is a symbolic link: a regular file of the
// administrator under the same name is left alone.
func unlink(p string) error {
	st, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if st.Mode()&fs.ModeSymlink == 0 {
		return nil
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Disable removes the instance links from every known target (and the
// CloudUserUnit link once no cloud-user instance is left) and the stage
// drop-in; the drop-in directory goes only if nothing else is left in it.
func Disable(l paths.Layout, ext string) error {
	for _, t := range knownTargets() {
		if err := unlink(filepath.Join(l.WantsDir(t), UnitName(ext))); err != nil {
			return fmt.Errorf("отключение %s: %w", UnitName(ext), err)
		}
	}
	if err := dropPuller(l); err != nil {
		return fmt.Errorf("отключение %s: %w", UnitName(ext), err)
	}
	p := l.DropIn(ext)
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("отключение %s: %w", UnitName(ext), err)
	}
	if err := os.Remove(filepath.Dir(p)); err != nil && !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTEMPTY) &&
		!errors.Is(err, syscall.EEXIST) {
		return fmt.Errorf("отключение %s: %w", UnitName(ext), err)
	}
	return nil
}
