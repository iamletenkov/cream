package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/iamletenkov/cream/internal/paths"
	"github.com/iamletenkov/cream/internal/place"
	"github.com/iamletenkov/cream/internal/state"
)

// Adapted from the reviewer's ENOTDIR probe: an inaccessible stale file must
// not turn install into a refusal when upgrade merely warns and continues.
func TestStaleUnplaceWarningParity(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"not-directory", "denied-parent", "unreadable-file", "backup-name-too-long"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			if os.Geteuid() == 0 && (scenario == "denied-parent" || scenario == "unreadable-file") {
				t.Skip("root can bypass the permissions used to reproduce EACCES")
			}
			var wantRecords map[string]state.Record
			var wantDisk map[string]string
			for _, command := range []string{"install", "upgrade"} {
				root := buildCLIRoot(t)
				l := paths.Layout{Root: root}
				dst := "/etc/probe-d/f"
				if scenario == "backup-name-too-long" {
					dst = "/etc/" + strings.Repeat("f", 239)
				}
				files := fmt.Sprintf("files: [{src: payload, dst: %s}]\n", dst)
				preflightCatalog(t, root, "1.0.0", "", files, false)
				out := runCLI(t, cliIn{Root: root}, "install", "--phase", "live", "--with-deps", "app")
				if out.Code != 0 {
					t.Fatalf("seed: %d %s", out.Code, out.Stderr)
				}
				store := state.NewStore(l, nil)
				old, ok, err := store.Get("dep")
				if err != nil || !ok {
					t.Fatalf("seed record: %v %v", ok, err)
				}
				physical := filepath.Join(root, dst)
				probe, content := dst, "dep 1.0.0"
				restore := func() {}
				switch scenario {
				case "not-directory":
					if err := os.Remove(physical); err != nil {
						t.Fatal(err)
					}
					if err := os.Remove(filepath.Dir(physical)); err != nil {
						t.Fatal(err)
					}
					probe, content = "/etc/probe-d", "now a file"
					writeRootFile(t, root, probe, content, 0o644)
					if _, err := os.Lstat(physical); !errors.Is(err, syscall.ENOTDIR) {
						t.Fatalf("ENOTDIR not reproduced: %v", err)
					}
				case "denied-parent":
					parent := filepath.Dir(physical)
					if err := os.Chmod(parent, 0); err != nil {
						t.Fatal(err)
					}
					restore = func() {
						if err := os.Chmod(parent, 0o755); err != nil {
							t.Error(err)
						}
					}
					t.Cleanup(restore)
					if _, err := os.Lstat(physical); !errors.Is(err, syscall.EACCES) {
						t.Fatalf("Lstat EACCES not reproduced: %v", err)
					}
				case "unreadable-file":
					if err := os.Chmod(physical, 0); err != nil {
						t.Fatal(err)
					}
					restore = func() {
						if err := os.Chmod(physical, 0o644); err != nil {
							t.Error(err)
						}
					}
					t.Cleanup(restore)
					if _, err := os.ReadFile(physical); !errors.Is(err, syscall.EACCES) {
						t.Fatalf("ReadFile EACCES not reproduced: %v", err)
					}
				case "backup-name-too-long":
					content = "modified by user"
					writeRootFile(t, root, dst, content, 0o644)
					writeRootFile(t, root, dst+place.ModifiedSuffix, "earlier backup", 0o644)
					if _, err := os.Lstat(physical + place.ModifiedSuffix + ".1"); !errors.Is(err, syscall.ENAMETOOLONG) {
						t.Fatalf("ENAMETOOLONG not reproduced: %v", err)
					}
				}
				removed, preserved := map[string]bool{}, map[string]bool{}
				place.PlanUnplace(l, old.Files, removed, preserved, map[string]bool{})
				if len(removed) != 0 || len(preserved) != 0 {
					t.Fatalf("uncertain path was projected: removed=%v preserved=%v", removed, preserved)
				}
				preflightCatalog(t, root, "2.0.0", "", "", false)
				args := []string{command, "--phase", "live", "dep", "app"}
				if out = runCLI(t, cliIn{Root: root}, append(args, "--dry-run")...); out.Code != 0 {
					t.Errorf("%s dry-run: %d %s", command, out.Code, out.Stderr)
				}
				out = runCLI(t, cliIn{Root: root}, args...)
				if out.Code != 0 || !strings.Contains(out.Stderr, "install.unplace") {
					t.Errorf("%s must warn and succeed: %d %s", command, out.Code, out.Stderr)
				}
				restore()
				records := map[string]state.Record{}
				disk := map[string]string{}
				for _, name := range []string{"dep", "app"} {
					rec, ok, err := store.Get(name)
					if err != nil || !ok {
						t.Fatalf("%s record: %v %v", name, ok, err)
					}
					if rec.Version != "2.0.0" || rec.Status != state.StatusInstalled || len(rec.Files) != 0 {
						t.Errorf("%s record: %+v", command, rec)
					}
					rec.Source.Origin = strings.ReplaceAll(rec.Source.Origin, root, "<ROOT>")
					records[name] = *rec
					payload, err := os.ReadFile(filepath.Join(l.ExtTree(name), "payload"))
					if err != nil || string(payload) != name+" 2.0.0" {
						t.Fatalf("%s payload: %q %v", command, payload, err)
					}
					disk[name] = string(payload)
				}
				data, err := os.ReadFile(filepath.Join(root, probe))
				if err != nil || string(data) != content {
					t.Fatalf("stale path changed: %q %v", data, err)
				}
				disk[probe] = string(data)
				history, err := os.ReadFile(l.History())
				if err != nil {
					t.Fatal(err)
				}
				disk["history"] = strings.ReplaceAll(string(history), root, "<ROOT>")
				if wantRecords == nil {
					wantRecords, wantDisk = records, disk
				} else if !reflect.DeepEqual(wantRecords, records) || !reflect.DeepEqual(wantDisk, disk) {
					t.Errorf("install/upgrade diverged: records=%+v / %+v, disk=%v / %v", wantRecords, records, wantDisk, disk)
				}
			}
		})
	}
}
