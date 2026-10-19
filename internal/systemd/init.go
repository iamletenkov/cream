package systemd

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/iamletenkov/cream/internal/fsx"
	"github.com/iamletenkov/cream/internal/paths"
)

// Init prepares the machine layout (spec §5, `cream system init`): the cream
// directories with their modes, the unit template from the built-in copy and
// the target of every enabled configure instance, recomputed from the stage
// recorded in its drop-in. Modes are set explicitly, whatever the umask, and
// existing directories are brought back to them. Repeating Init is a no-op.
//
// The source cache is not created here: its place depends on --cache and
// user mode, and the sync creates it.
func Init(l paths.Layout) error {
	dirs := []struct {
		path string
		mode fs.FileMode
	}{
		{filepath.Dir(l.UnitTemplate()), 0o755},
		{filepath.Dir(l.ExtTree("x")), 0o755},
		{filepath.Dir(l.EtcDir("x")), 0o755},
		{filepath.Dir(l.InstalledDir()), 0o755},
		{l.InstalledDir(), 0o755},
		{filepath.Dir(l.StateDir("x")), 0o755},
		{l.LockDir(), 0o700},
		{filepath.Dir(l.Incoming("x")), 0o700},
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d.path, 0o755); err != nil {
			return fmt.Errorf("system init: %w", err)
		}
		if err := os.Chmod(d.path, d.mode); err != nil {
			return fmt.Errorf("system init: %w", err)
		}
	}
	if err := writeUnit(l.UnitTemplate(), unitTemplate); err != nil {
		return fmt.Errorf("system init: %w", err)
	}
	if err := writeUnit(l.CloudUserUnit(), cloudUserUnit); err != nil {
		return fmt.Errorf("system init: %w", err)
	}
	return relinkAll(l, nil)
}

// writeUnit rewrites a built-in unit only when its content or mode differ.
func writeUnit(p string, body []byte) error {
	if cur, err := os.ReadFile(p); err == nil && bytes.Equal(cur, body) {
		if st, err := os.Lstat(p); err == nil && st.Mode().IsRegular() && st.Mode().Perm() == 0o644 {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return fsx.WriteAtomic(p, body, 0o644)
}

// relinkAll recomputes the target of every instance that has a stage drop-in.
// It takes no lock: a remove running at the same time may take the drop-in
// away after the stage was read, so the drop-in is checked again after the
// link and, gone, the links are removed. beforeLink is a test seam (nil in
// production).
func relinkAll(l paths.Layout, beforeLink func(ext string)) error {
	base := filepath.Dir(filepath.Dir(l.DropIn("x")))
	entries, err := os.ReadDir(base)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("system init: %w", err)
	}
	for _, e := range entries {
		ext, ok := strings.CutPrefix(e.Name(), "cream-configure@")
		if !ok {
			continue
		}
		ext, ok = strings.CutSuffix(ext, ".service.d")
		if !ok || ext == "" || !e.IsDir() {
			continue
		}
		stage, err := readStage(l.DropIn(ext))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("system init: %s: %w", UnitName(ext), err)
		}
		def, err := lookupStage(stage)
		if err != nil {
			return fmt.Errorf("system init: %s: %w", UnitName(ext), err)
		}
		if beforeLink != nil {
			beforeLink(ext)
		}
		if err := link(l, ext, def.target(l)); err != nil {
			return fmt.Errorf("system init: %w", err)
		}
		if _, err := os.Lstat(l.DropIn(ext)); errors.Is(err, fs.ErrNotExist) {
			if err := Disable(l, ext); err != nil {
				return fmt.Errorf("system init: %w", err)
			}
		}
	}
	// Once for all instances: an instance moved away from cloud-user (or a
	// helper link left without instances) takes the CloudUserUnit link along.
	if err := dropPuller(l); err != nil {
		return fmt.Errorf("system init: %w", err)
	}
	return nil
}

// readStage returns the X-CreamStage value of a drop-in.
func readStage(p string) (string, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		if v, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), stageKey); ok {
			return v, nil
		}
	}
	return "", fmt.Errorf("в %s нет %s", p, strings.TrimSuffix(stageKey, "="))
}
