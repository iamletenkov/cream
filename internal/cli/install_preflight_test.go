package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/iamletenkov/cream/internal/paths"
	"github.com/iamletenkov/cream/internal/state"
)

// preflightCatalog uses real manifests and source synchronization: no engine
// internals are mocked, so an early dependency write is visible in the snapshot.
func preflightCatalog(t *testing.T, root, version, appFiles, depFiles string, risk bool) {
	t.Helper()
	dir := filepath.Join(root, "srv", "catalog-"+version)
	writeRootFile(t, root, "/srv/catalog-"+version+"/cream-catalog.yaml",
		"schema: cream/v1\nname: preflight\nversion: "+version+"\nroots: [extensions]\n", 0o644)
	for _, ext := range []struct{ name, files, extra string }{
		{"app", appFiles, "depends: [{name: dep}]\n"},
		{"dep", depFiles, ""},
	} {
		extra := ext.extra
		if risk && ext.name == "app" {
			extra += "targets: {build: exec, live: confirm, live_note: test}\n"
		}
		base := "/srv/catalog-" + version + "/extensions/" + ext.name
		writeRootFile(t, root, base+"/extension.yaml", fmt.Sprintf(
			"schema: cream/v1\nname: %s\nversion: %s\nsummary: preflight\ncream: \">=1.0.0\"\nplatform: {family: deb, arch: [amd64, arm64]}\nkind: library\n%s%s",
			ext.name, version, extra, ext.files), 0o644)
		writeRootFile(t, root, base+"/payload", ext.name+" "+version, 0o644)
	}
	installCLISources(t, root, dir)
}

// snapshot includes contents, modes and mtimes of files AND directories; even
// creating then deleting policy-rc.d or a staging tree changes its parents.
func preflightSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		st, err := d.Info()
		if err != nil {
			return err
		}
		var data []byte
		if st.Mode().IsRegular() {
			data, err = os.ReadFile(p)
			if err != nil {
				return err
			}
		}
		out[p] = fmt.Sprintf("%s %s %x", st.Mode(), st.ModTime(), data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestInstallFilePreflight(t *testing.T) {
	t.Parallel()
	const files = "files: [{src: payload, dst: /etc/preflight-shared}]\n"
	for _, tc := range []struct {
		name, code, status                     string
		installed, broken, unowned, pair, risk bool
		parent                                 bool
	}{
		{name: "two-new", code: "conflict.file", pair: true},
		{name: "file-is-parent", code: "conflict.file", pair: true, parent: true},
		{name: "installed-owner", code: "conflict.file", installed: true},
		{name: "owner-without-tree", code: "conflict.file_owner", installed: true, broken: true},
		{name: "failed-owner-without-tree", code: "conflict.file_owner", installed: true, broken: true, status: state.StatusFailed},
		{name: "failed-upgrade-owner", code: "conflict.file_owner", installed: true, broken: true, status: state.StatusFailedUpgrade},
		{name: "skipped-owner", code: "conflict.file_owner", installed: true, broken: true, status: state.StatusSkipped},
		{name: "unowned-after-dependency", code: "conflict.file_unowned", unowned: true},
		{name: "accept-risk-does-not-override", code: "conflict.file_unowned", unowned: true, risk: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, phase := range []string{"live", "build"} {
				t.Run(phase, func(t *testing.T) {
					root := buildCLIRoot(t)
					if tc.installed {
						preflightCatalog(t, root, "1.0.0", "", files, false)
						out := runCLI(t, cliIn{Root: root}, "install", "--phase", "live", "dep")
						if out.Code != 0 {
							t.Fatalf("seed: %d %s", out.Code, out.Stderr)
						}
						if tc.broken {
							if err := os.RemoveAll((paths.Layout{Root: root}).ExtTree("dep")); err != nil {
								t.Fatal(err)
							}
							// A recorded claim survives both a missing tree and a
							// missing destination, regardless of install status.
							if err := os.Remove(filepath.Join(root, "etc/preflight-shared")); err != nil {
								t.Fatal(err)
							}
							if tc.status != "" {
								store := state.NewStore(paths.Layout{Root: root}, nil)
								rec, ok, err := store.Get("dep")
								if err != nil || !ok {
									t.Fatalf("owner record: %v %v", ok, err)
								}
								rec.Status = tc.status
								if err := store.Put(*rec); err != nil {
									t.Fatal(err)
								}
							}
						}
					}
					depFiles := ""
					if tc.pair || (tc.installed && !tc.broken) {
						depFiles = files
					}
					version := "1.0.0"
					if tc.broken {
						version = "2.0.0"
					}
					appFiles := files
					if tc.parent {
						appFiles = strings.ReplaceAll(files, "/etc/preflight-shared", "/etc/preflight-shared/child")
					}
					preflightCatalog(t, root, version, appFiles, depFiles, tc.risk)
					if tc.unowned {
						writeRootFile(t, root, "/etc/preflight-shared", "foreign", 0o644)
					}
					args := []string{"install", "--phase", phase, "--with-deps", "app"}
					if tc.risk {
						args = append(args, "--accept-risk")
					}
					before := preflightSnapshot(t, root)
					out := runCLI(t, cliIn{Root: root}, args...)
					if out.Code != 67 || !strings.Contains(out.Stderr, tc.code) || !strings.Contains(out.Stderr, "/etc/preflight-shared") {
						t.Errorf("refusal: %d %s", out.Code, out.Stderr)
					}
					if (tc.pair || tc.installed) && (!strings.Contains(out.Stderr, "app") || !strings.Contains(out.Stderr, "dep")) {
						t.Errorf("missing claimant names: %s", out.Stderr)
					}
					if !reflect.DeepEqual(before, preflightSnapshot(t, root)) {
						t.Error("refusal wrote to disk")
					}
					dry := runCLI(t, cliIn{Root: root}, append(args, "--dry-run")...)
					if dry.Code != out.Code || dry.Stderr != out.Stderr {
						t.Errorf("dry-run differs: %d %s", dry.Code, dry.Stderr)
					}
					if !reflect.DeepEqual(before, preflightSnapshot(t, root)) {
						t.Error("dry-run wrote to disk")
					}
				})
			}
		})
	}
}

func TestInstallFilePreflightOwnDirectory(t *testing.T) {
	t.Parallel()
	root := buildCLIRoot(t)
	const own = "files: [{src: payload, dst: /etc/preflight-own}]\n"
	preflightCatalog(t, root, "1.0.0", own, "", false)
	args := []string{"install", "--phase", "live", "--with-deps", "app"}
	if out := runCLI(t, cliIn{Root: root}, args...); out.Code != 0 {
		t.Fatalf("seed: %d %s", out.Code, out.Stderr)
	}
	if err := os.Remove(filepath.Join(root, "etc/preflight-own")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "etc/preflight-own"), 0o755); err != nil {
		t.Fatal(err)
	}
	preflightCatalog(t, root, "2.0.0", own, "", false)
	before := preflightSnapshot(t, root)
	for _, extra := range [][]string{{"--dry-run"}, {}} {
		out := runCLI(t, cliIn{Root: root}, append(args, extra...)...)
		if out.Code != 67 || !strings.Contains(out.Stderr, "/etc/preflight-own") {
			t.Errorf("own directory: %d %s", out.Code, out.Stderr)
		}
		if !reflect.DeepEqual(before, preflightSnapshot(t, root)) {
			t.Error("own directory refusal wrote to disk")
		}
	}
}

func TestInstallFilePreflightOwnParentDoesNotOwnChildren(t *testing.T) {
	t.Parallel()
	root := buildCLIRoot(t)
	const own = "files: [{src: payload, dst: /etc/preflight-own}]\n"
	preflightCatalog(t, root, "1.0.0", own, "", false)
	args := []string{"install", "--phase", "live", "--with-deps", "app"}
	if out := runCLI(t, cliIn{Root: root}, args...); out.Code != 0 {
		t.Fatalf("seed: %d %s", out.Code, out.Stderr)
	}
	if err := os.Remove(filepath.Join(root, "etc/preflight-own")); err != nil {
		t.Fatal(err)
	}
	writeRootFile(t, root, "/etc/preflight-own/child", "foreign content", 0o644)
	preflightCatalog(t, root, "2.0.0", strings.ReplaceAll(own, "/etc/preflight-own", "/etc/preflight-own/child"), "", false)
	before := preflightSnapshot(t, root)
	for _, extra := range [][]string{{"--dry-run"}, {}} {
		out := runCLI(t, cliIn{Root: root}, append(args, extra...)...)
		if out.Code != 67 || !strings.Contains(out.Stderr, "conflict.file_unowned") {
			t.Errorf("unowned child: %d %s", out.Code, out.Stderr)
		}
		if !reflect.DeepEqual(before, preflightSnapshot(t, root)) {
			t.Error("overwrote unowned child")
		}
	}
}

func TestInstallFilePreflightUpgradeAndDryRun(t *testing.T) {
	t.Parallel()
	root := buildCLIRoot(t)
	const own = "files: [{src: payload, dst: /etc/preflight-own}]\n"
	const dep = "files: [{src: payload, dst: /usr/local/share/preflight-dep}]\n"
	preflightCatalog(t, root, "1.0.0", own, dep, false)
	args := []string{"install", "--phase", "live", "--with-deps", "app"}
	before := preflightSnapshot(t, root)
	out := runCLI(t, cliIn{Root: root}, append(args, "--dry-run", "--json")...)
	if out.Code != 0 || !strings.Contains(out.Stdout, `"status": "planned"`) || !strings.Contains(out.Stdout, `"changed": false`) {
		t.Fatalf("dry-run: %d %s %s", out.Code, out.Stdout, out.Stderr)
	}
	if !reflect.DeepEqual(before, preflightSnapshot(t, root)) {
		t.Fatal("successful dry-run wrote to disk")
	}
	if out = runCLI(t, cliIn{Root: root}, args...); out.Code != 0 {
		t.Fatalf("install: %d %s", out.Code, out.Stderr)
	}
	preflightCatalog(t, root, "2.0.0", own, dep, false)
	if out = runCLI(t, cliIn{Root: root}, args...); out.Code != 0 {
		t.Fatalf("upgrade own file: %d %s", out.Code, out.Stderr)
	}
	data, err := os.ReadFile(filepath.Join(root, "etc/preflight-own"))
	if err != nil || string(data) != "app 2.0.0" {
		t.Fatalf("upgrade file: %q %v", data, err)
	}
}
