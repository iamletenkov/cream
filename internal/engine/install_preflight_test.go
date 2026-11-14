package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/state"
)

// Gate on a platform command after the first source/registry snapshot. A
// concurrent operation changes that snapshot before the global lock is granted.
func TestInstallPreflightRechecksUnderGlobalLock(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"registry", "source"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			ext := t.TempDir()
			manifest := "schema: cream/v1\nname: preflight\nversion: 1.0.0\nsummary: preflight\ncream: \">=1.0.0\"\nkind: library\nplatform: {family: deb, arch: [amd64, arm64], commands: [preflight-gate]}\nfiles: [{src: payload, dst: /etc/preflight-recheck}]\n"
			if err := os.WriteFile(filepath.Join(ext, "extension.yaml"), []byte(manifest), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(ext, "payload"), []byte("payload"), 0o644); err != nil {
				t.Fatal(err)
			}
			catalog := installCatalog(t, "1.0.0", map[string]string{"preflight": ext})
			te := installEngine(t, installSrc{"test", catalog})
			global, err := te.Locks.Global(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer global.Release()
			ready, proceed := make(chan struct{}), make(chan struct{})
			calls := 0
			te.LookPath = func(string) (string, error) {
				calls++
				if calls == 1 {
					close(ready)
					<-proceed
				}
				return "/test/preflight-gate", nil
			}
			done := make(chan error, 1)
			go func() {
				_, err := te.Install(context.Background(), InstallRequest{Refs: []string{"preflight"}})
				done <- err
			}()
			<-ready
			want := "conflict.file_owner"
			if change == "registry" {
				err = te.Store.Put(state.Record{Name: "other", Version: "1.0.0", Kind: "library", Status: state.StatusFailed,
					Files: []state.FileRec{{Dst: "/etc/preflight-recheck", SHA256: strings.Repeat("a", 64)}}})
			} else {
				want = "lock.changed"
				err = os.WriteFile(filepath.Join(te.L.SourceCache("test"), "extensions/preflight/payload"), []byte("changed"), 0o644)
			}
			close(proceed)
			global.Release()
			if err != nil {
				t.Fatal(err)
			}
			err = <-done
			exit := cerr.ExitGraph
			if change == "source" {
				exit = cerr.ExitLock
			}
			wantCode(t, err, exit, want)
			for _, p := range []string{te.L.ExtTree("preflight"), te.L.ExtNew("preflight"), filepath.Join(te.Root, "etc/preflight-recheck"), te.L.History()} {
				if installExists(p) {
					t.Errorf("recheck refusal wrote %s", p)
				}
			}
		})
	}
}
