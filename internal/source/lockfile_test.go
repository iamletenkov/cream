package source

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/paths"
)

func TestReadLock(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		body     string // "" — файла нет
		wantN    int
		wantExit int
	}{
		{name: "missing file is empty", wantN: 0},
		{name: "two entries", body: `{"sources":[{"name":"a","type":"path","origin":"/a","resolved":"sha256:1",` +
			`"synced_at":"2026-10-18T12:00:00Z","catalog_version":"1.0.0"},{"name":"b","type":"archive",` +
			`"origin":"https://x","resolved":"sha256:2","synced_at":"2026-10-18T12:00:00Z","catalog_version":"2"}]}`, wantN: 2},
		{name: "corrupt", body: `{"sources":[`, wantExit: cerr.ExitState},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := filepath.Join(t.TempDir(), "sources.lock")
			if tc.body != "" {
				if err := os.WriteFile(p, []byte(tc.body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got, err := ReadLock(p)
			if tc.wantExit != 0 {
				wantCerr(t, err, tc.wantExit, "state.corrupt")
				return
			}
			if err != nil || len(got) != tc.wantN {
				t.Fatalf("ReadLock = %+v, %v", got, err)
			}
			if tc.wantN == 2 && (got[0].SyncedAt != syncNow || got[1].CatalogVersion != "2") {
				t.Errorf("разбор: %+v", got)
			}
		})
	}
}

func TestDirs(t *testing.T) {
	t.Parallel()
	l := paths.Layout{Root: t.TempDir()}
	f := &File{Sources: []Source{{Name: "dev", Type: TypePath, Path: "/x"}, archiveSrc("stvor"), archiveSrc("cold")}}
	if err := os.MkdirAll(l.SourceCache("stvor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(l.SourceCache("dev"), 0o755); err != nil {
		t.Fatal(err)
	}
	lock := []LockEntry{{Name: "stvor", Resolved: "sha256:1"}, {Name: "gone", Resolved: "sha256:2"}}
	got := Dirs(l, f, lock)
	if len(got) != 3 {
		t.Fatalf("Dirs = %+v", got)
	}
	tests := []struct {
		i        int
		name     string
		synced   bool
		resolved string
	}{
		{0, "dev", true, ""},
		{1, "stvor", true, "sha256:1"},
		{2, "cold", false, ""},
	}
	for _, tc := range tests {
		d := got[tc.i]
		if d.Source.Name != tc.name || d.Dir != l.SourceCache(tc.name) || d.Synced != tc.synced {
			t.Errorf("[%d] = %+v", tc.i, d)
		}
		if (d.Lock == nil) != (tc.resolved == "") || (d.Lock != nil && d.Lock.Resolved != tc.resolved) {
			t.Errorf("[%d] Lock = %+v, ждали %q", tc.i, d.Lock, tc.resolved)
		}
	}
}
