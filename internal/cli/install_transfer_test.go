package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// dep 1.0.0 owns F; in 2.0.0 dep drops F and app (depends on dep) takes it.
func TestReviewTransferWithinGraph(t *testing.T) {
	t.Parallel()
	root := buildCLIRoot(t)
	const f = "files: [{src: payload, dst: /etc/preflight-moved}]\n"
	preflightCatalog(t, root, "1.0.0", "", f, false)
	if out := runCLI(t, cliIn{Root: root}, "install", "--phase", "live", "--with-deps", "app"); out.Code != 0 {
		t.Fatalf("seed: %d %s", out.Code, out.Stderr)
	}
	preflightCatalog(t, root, "2.0.0", f, "", false)
	before := preflightSnapshot(t, root)
	dry := runCLI(t, cliIn{Root: root}, "install", "--phase", "live", "--dry-run", "dep", "app")
	if dry.Code != 0 {
		t.Errorf("dry-run transfer: %d %s", dry.Code, dry.Stderr)
	}
	if !reflect.DeepEqual(before, preflightSnapshot(t, root)) {
		t.Fatal("dry-run wrote to disk")
	}
	out := runCLI(t, cliIn{Root: root}, "install", "--phase", "live", "dep", "app")
	t.Logf("transfer: code=%d stderr=%s", out.Code, strings.TrimSpace(out.Stderr))
	if out.Code != 0 {
		t.Errorf("transfer refused: %d", out.Code)
	}
	assertTransferredFile(t, root, "/etc/preflight-moved", "app 2.0.0")
}

// A dangling parent used to surface only on app, after dep was installed.
// Both ordinary install and dry-run must refuse before any writes.
func TestReviewSymlinkParent(t *testing.T) {
	t.Parallel()
	for _, suffix := range []string{"x", "subdir/x"} {
		t.Run(suffix, func(t *testing.T) {
			root := buildCLIRoot(t)
			// Relative target stays under the test root and does not exist.
			if err := os.Symlink("../missing-target", filepath.Join(root, "etc/linkdir")); err != nil {
				t.Fatal(err)
			}
			preflightCatalog(t, root, "1.0.0", fmt.Sprintf("files: [{src: payload, dst: /etc/linkdir/%s}]\n", suffix), "", false)
			before := preflightSnapshot(t, root)
			for _, extra := range [][]string{{"--dry-run"}, {}} {
				args := []string{"install", "--phase", "live", "--with-deps", "app"}
				out := runCLI(t, cliIn{Root: root}, append(args, extra...)...)
				if out.Code != 74 || !strings.Contains(out.Stderr, "file.stat") || !strings.Contains(out.Stderr, "/etc/linkdir") {
					t.Errorf("dangling parent: %d %s", out.Code, out.Stderr)
				}
				if !reflect.DeepEqual(before, preflightSnapshot(t, root)) {
					t.Fatal("dangling parent refusal wrote to disk")
				}
			}
		})
	}
}

func TestReviewTransferViaUpgrade(t *testing.T) {
	t.Parallel()
	root := buildCLIRoot(t)
	const f = "files: [{src: payload, dst: /etc/preflight-moved}]\n"
	preflightCatalog(t, root, "1.0.0", "", f, false)
	if out := runCLI(t, cliIn{Root: root}, "install", "--phase", "live", "--with-deps", "app"); out.Code != 0 {
		t.Fatalf("seed: %d %s", out.Code, out.Stderr)
	}
	preflightCatalog(t, root, "2.0.0", f, "", false)
	out := runCLI(t, cliIn{Root: root}, "upgrade", "--phase", "live", "dep", "app")
	t.Logf("review: upgrade transfer code=%d stderr=%s", out.Code, strings.TrimSpace(out.Stderr))
	if out.Code != 0 {
		t.Fatalf("upgrade transfer: %d %s", out.Code, out.Stderr)
	}
	assertTransferredFile(t, root, "/etc/preflight-moved", "app 2.0.0")
}

func assertTransferredFile(t *testing.T, root, dst, content string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, dst))
	if err != nil || string(data) != content {
		t.Fatalf("transferred file: %q %v", data, err)
	}
}

func TestTransferThreeLayerGraph(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"install", "upgrade"} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			root := buildCLIRoot(t)
			const f = "files: [{src: payload, dst: /etc/preflight-moved}]\n"
			for _, version := range []string{"1.0.0", "2.0.0"} {
				appFiles, baseFiles := "", f
				if version == "2.0.0" {
					appFiles, baseFiles = f, ""
				}
				preflightCatalog(t, root, version, appFiles, "", false)
				prefix := "/srv/catalog-" + version
				depManifest := filepath.Join(root, prefix, "extensions/dep/extension.yaml")
				fh, err := os.OpenFile(depManifest, os.O_APPEND|os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				_, err = fh.WriteString("depends: [{name: base}]\n")
				closeErr := fh.Close()
				if err != nil || closeErr != nil {
					t.Fatalf("dep manifest: %v %v", err, closeErr)
				}
				writeRootFile(t, root, prefix+"/extensions/base/extension.yaml", fmt.Sprintf(
					"schema: cream/v1\nname: base\nversion: %s\nsummary: base\ncream: \">=1.0.0\"\nkind: library\nplatform: {family: deb, arch: [amd64, arm64]}\n%s", version, baseFiles), 0o644)
				writeRootFile(t, root, prefix+"/extensions/base/payload", "base "+version, 0o644)
				installCLISources(t, root, filepath.Join(root, prefix))
				if version == "1.0.0" {
					out := runCLI(t, cliIn{Root: root}, "install", "--phase", "live", "--with-deps", "app")
					if out.Code != 0 {
						t.Fatalf("seed chain: %d %s", out.Code, out.Stderr)
					}
				}
			}
			args := []string{command, "--phase", "live", "app", "base", "dep"}
			before := preflightSnapshot(t, root)
			dry := runCLI(t, cliIn{Root: root}, append(args, "--dry-run")...)
			if dry.Code != 0 {
				t.Errorf("chain dry-run: %d %s", dry.Code, dry.Stderr)
			}
			if !reflect.DeepEqual(before, preflightSnapshot(t, root)) {
				t.Fatal("chain dry-run wrote to disk")
			}
			out := runCLI(t, cliIn{Root: root}, args...)
			if out.Code != 0 {
				t.Fatalf("chain transfer: %d %s", out.Code, out.Stderr)
			}
			assertTransferredFile(t, root, "/etc/preflight-moved", "app 2.0.0")
		})
	}
}

func TestTransferDoesNotFreeNonregularFile(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := buildCLIRoot(t)
			const f = "files: [{src: payload, dst: /etc/preflight-moved}]\n"
			preflightCatalog(t, root, "1.0.0", "", f, false)
			if out := runCLI(t, cliIn{Root: root}, "install", "--phase", "live", "--with-deps", "app"); out.Code != 0 {
				t.Fatalf("seed: %d %s", out.Code, out.Stderr)
			}
			dst := filepath.Join(root, "etc/preflight-moved")
			if err := os.Remove(dst); err != nil {
				t.Fatal(err)
			}
			var err error
			if kind == "directory" {
				err = os.Mkdir(dst, 0o755)
			} else {
				err = os.Symlink("foreign", dst)
			}
			if err != nil {
				t.Fatal(err)
			}
			preflightCatalog(t, root, "2.0.0", f, "", false)
			before := preflightSnapshot(t, root)
			for _, extra := range [][]string{{"--dry-run"}, {}} {
				args := []string{"install", "--phase", "live", "dep", "app"}
				out := runCLI(t, cliIn{Root: root}, append(args, extra...)...)
				if out.Code != 67 {
					t.Errorf("nonregular transfer: %d %s", out.Code, out.Stderr)
				}
				if !reflect.DeepEqual(before, preflightSnapshot(t, root)) {
					t.Fatal("nonregular refusal wrote to disk")
				}
			}
		})
	}
}

func TestTransferRequiresEarlierOwner(t *testing.T) {
	t.Parallel()
	root := buildCLIRoot(t)
	const f = "files: [{src: payload, dst: /etc/preflight-moved}]\n"
	preflightCatalog(t, root, "1.0.0", "", f, false)
	if out := runCLI(t, cliIn{Root: root}, "install", "--phase", "live", "--with-deps", "app"); out.Code != 0 {
		t.Fatalf("seed: %d %s", out.Code, out.Stderr)
	}
	preflightCatalog(t, root, "2.0.0", f, "", false)
	// Reverse the dependency: app now runs before dep can release the file.
	for _, name := range []string{"dep", "app"} {
		p := filepath.Join(root, "srv/catalog-2.0.0/extensions", name, "extension.yaml")
		body, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(string(body), "depends: [{name: dep}]\n", "")
		if name == "dep" {
			text += "depends: [{name: app}]\n"
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	installCLISources(t, root, filepath.Join(root, "srv/catalog-2.0.0"))
	before := preflightSnapshot(t, root)
	for _, extra := range [][]string{{"--dry-run"}, {}} {
		out := runCLI(t, cliIn{Root: root}, append([]string{"install", "--phase", "live", "dep", "app"}, extra...)...)
		if out.Code != 67 || !strings.Contains(out.Stderr, "conflict.file_owner") {
			t.Errorf("late release: %d %s", out.Code, out.Stderr)
		}
		if !reflect.DeepEqual(before, preflightSnapshot(t, root)) {
			t.Fatal("late release refusal wrote to disk")
		}
	}
}

func TestTransferModifiedFile(t *testing.T) {
	t.Parallel()
	root := buildCLIRoot(t)
	const f = "files: [{src: payload, dst: /etc/preflight-moved}]\n"
	preflightCatalog(t, root, "1.0.0", "", f, false)
	if out := runCLI(t, cliIn{Root: root}, "install", "--phase", "live", "--with-deps", "app"); out.Code != 0 {
		t.Fatalf("seed: %d %s", out.Code, out.Stderr)
	}
	writeRootFile(t, root, "/etc/preflight-moved", "modified by user", 0o644)
	preflightCatalog(t, root, "2.0.0", f, "", false)
	if out := runCLI(t, cliIn{Root: root}, "install", "--phase", "live", "app", "dep"); out.Code != 0 {
		t.Fatalf("modified transfer: %d %s", out.Code, out.Stderr)
	}
	assertTransferredFile(t, root, "/etc/preflight-moved", "app 2.0.0")
	assertTransferredFile(t, root, "/etc/preflight-moved.cream-modified", "modified by user")
}

func TestPreflightAllowsValidSymlinkParent(t *testing.T) {
	t.Parallel()
	root := buildCLIRoot(t)
	if err := os.Mkdir(filepath.Join(root, "real-parent"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../real-parent", filepath.Join(root, "etc/linkdir")); err != nil {
		t.Fatal(err)
	}
	preflightCatalog(t, root, "1.0.0", "files: [{src: payload, dst: /etc/linkdir/x}]\n", "", false)
	if out := runCLI(t, cliIn{Root: root}, "install", "--phase", "live", "--with-deps", "app"); out.Code != 0 {
		t.Fatalf("valid parent: %d %s", out.Code, out.Stderr)
	}
	assertTransferredFile(t, root, "/real-parent/x", "app 1.0.0")
}

func TestTransferReservesModifiedBackup(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"first-backup", "existing-backup", "new-file-at-backup", "new-directory-at-backup"} {
		t.Run(scenario, func(t *testing.T) {
			root := buildCLIRoot(t)
			const original = "files: [{src: payload, dst: /etc/preflight-moved}]\n"
			const backup = "files: [{src: payload, dst: /etc/preflight-moved.cream-modified}]\n"
			preflightCatalog(t, root, "1.0.0", "", original, false)
			if out := runCLI(t, cliIn{Root: root}, "install", "--phase", "live", "--with-deps", "app"); out.Code != 0 {
				t.Fatalf("seed: %d %s", out.Code, out.Stderr)
			}
			writeRootFile(t, root, "/etc/preflight-moved", "modified by user", 0o644)
			appFiles, depFiles := backup, ""
			if scenario != "first-backup" {
				appFiles = strings.ReplaceAll(backup, "cream-modified}", "cream-modified.1}")
			}
			if scenario == "existing-backup" {
				writeRootFile(t, root, "/etc/preflight-moved.cream-modified", "earlier backup", 0o644)
			}
			if scenario == "new-file-at-backup" {
				depFiles = backup
			}
			if scenario == "new-directory-at-backup" {
				depFiles = strings.ReplaceAll(backup, "cream-modified}", "cream-modified/child}")
			}
			preflightCatalog(t, root, "2.0.0", appFiles, depFiles, false)
			before := preflightSnapshot(t, root)
			for _, extra := range [][]string{{"--dry-run"}, {}} {
				out := runCLI(t, cliIn{Root: root}, append([]string{"install", "--phase", "live", "dep", "app"}, extra...)...)
				if out.Code != 67 || !strings.Contains(out.Stderr, "conflict.file_unowned") {
					t.Errorf("backup conflict: %d %s", out.Code, out.Stderr)
				}
				if !reflect.DeepEqual(before, preflightSnapshot(t, root)) {
					t.Fatal("backup conflict wrote to disk")
				}
			}
		})
	}
}
