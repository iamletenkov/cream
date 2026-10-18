package source

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/fsx"
	"github.com/iamletenkov/cream/internal/paths"
)

var syncNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// fakeFetcher serves fixed bodies by URL and counts calls.
type fakeFetcher struct {
	bodies map[string][]byte
	calls  int
}

func (f *fakeFetcher) Fetch(_ context.Context, url string, w io.Writer, limit int64) error {
	f.calls++
	b, ok := f.bodies[url]
	if !ok {
		return cerr.New(cerr.ExitSource, "source.fetch", "нет %s", url)
	}
	if int64(len(b)) > limit {
		return cerr.New(cerr.ExitSource, "source.too_large", "больше лимита")
	}
	_, err := w.Write(b)
	return err
}

// catalogValidator accepts a tree with cream-catalog.yaml and returns its
// content as the catalog version; it records the directories it saw.
type catalogValidator struct{ seen []string }

func (v *catalogValidator) validate(dir string) (string, error) {
	v.seen = append(v.seen, dir)
	b, err := os.ReadFile(filepath.Join(dir, "cream-catalog.yaml"))
	if err != nil {
		return "", cerr.Wrap(err, cerr.ExitData, "manifest.catalog", "нет cream-catalog.yaml")
	}
	return strings.TrimSpace(string(b)), nil
}

func newSyncer(t *testing.T, f Fetcher) (*Syncer, *catalogValidator) {
	t.Helper()
	v := &catalogValidator{}
	return &Syncer{Layout: paths.Layout{Root: t.TempDir()}, Fetch: f, Validate: v.validate,
		Now: func() time.Time { return syncNow }}, v
}

func TestSyncArchive(t *testing.T) {
	t.Parallel()
	good := makeTarGz(t, []tarEntry{{name: "c-4.0.0/", typ: tar.TypeDir},
		{name: "c-4.0.0/cream-catalog.yaml", body: "4.0.0"}, {name: "c-4.0.0/ext/a", body: "a"}})
	const url = "https://portal.example/c.tar.gz"
	ff := &fakeFetcher{bodies: map[string][]byte{url: good}}
	s, v := newSyncer(t, ff)
	src := Source{Name: "stvor", Type: TypeArchive, URL: url, SHA256: strings.ToUpper(sha256Hex(good))}
	e, err := s.Sync(context.Background(), src)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	want := LockEntry{Name: "stvor", Type: TypeArchive, Origin: url, Resolved: "sha256:" + sha256Hex(good),
		SyncedAt: syncNow, CatalogVersion: "4.0.0"}
	if e != want {
		t.Errorf("LockEntry %+v, ждали %+v", e, want)
	}
	cache := s.Layout.SourceCache("stvor")
	if got := readFile(t, filepath.Join(cache, "ext", "a")); got != "a" {
		t.Errorf("кеш: ext/a = %q", got)
	}
	if len(v.seen) != 1 || v.seen[0] == cache {
		t.Errorf("валидатор видел %v: должен проверять частный каталог до подмены", v.seen)
	}
	lock, err := ReadLock(s.Layout.SourcesLock())
	if err != nil || len(lock) != 1 || lock[0] != want {
		t.Fatalf("sources.lock = %+v, %v", lock, err)
	}
	// Никакого мусора рядом с кешем.
	ents, _ := os.ReadDir(filepath.Dir(cache))
	if len(ents) != 1 {
		t.Errorf("рядом с кешем: %v", ents)
	}
}

func TestSyncArchiveSHA256MismatchBeforeExtract(t *testing.T) {
	t.Parallel()
	// Тело — не gzip: несовпадение sha256 должно сработать раньше распаковки.
	const url = "file:///srv/c.tar.gz"
	s, v := newSyncer(t, &fakeFetcher{bodies: map[string][]byte{url: []byte("not a tarball")}})
	_, err := s.Sync(context.Background(), Source{Name: "off", Type: TypeArchive, URL: url, SHA256: hexA})
	wantCerr(t, err, cerr.ExitSource, "source.sha256_mismatch")
	if len(v.seen) != 0 {
		t.Error("валидатор вызван при несовпадении sha256")
	}
	if _, err := os.Lstat(s.Layout.SourceCache("off")); err == nil {
		t.Error("кеш создан")
	}
	if _, err := os.Lstat(s.Layout.SourcesLock()); err == nil {
		t.Error("sources.lock записан")
	}
}

func TestSyncBrokenCatalogKeepsPreviousCache(t *testing.T) {
	t.Parallel()
	const url = "https://x/c.tar.gz"
	v1 := makeTarGz(t, []tarEntry{{name: "cream-catalog.yaml", body: "1.0.0"}, {name: "a", body: "old"}})
	broken := makeTarGz(t, []tarEntry{{name: "a", body: "new"}, {name: "b", body: "new"}})
	hostile := makeTarGz(t, []tarEntry{{name: "cream-catalog.yaml", body: "2"}, {name: "../x", body: "x"}})
	ff := &fakeFetcher{bodies: map[string][]byte{url: v1}}
	s, _ := newSyncer(t, ff)
	src := Source{Name: "s", Type: TypeArchive, URL: url, SHA256: sha256Hex(v1)}
	first, err := s.Sync(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		body     []byte
		wantExit int
		wantCode string
	}{
		{name: "validator refuses", body: broken, wantExit: cerr.ExitData, wantCode: "manifest.catalog"},
		{name: "hostile archive", body: hostile, wantExit: cerr.ExitSource, wantCode: "source.bad_archive"},
	}
	for _, tc := range tests {
		ff.bodies[url] = tc.body
		_, err := s.Sync(context.Background(), Source{Name: "s", Type: TypeArchive, URL: url, SHA256: sha256Hex(tc.body)})
		wantCerr(t, err, tc.wantExit, tc.wantCode)
		if got := readFile(t, filepath.Join(s.Layout.SourceCache("s"), "a")); got != "old" {
			t.Errorf("%s: прежний кеш подменён: a = %q", tc.name, got)
		}
		lock, _ := ReadLock(s.Layout.SourcesLock())
		if len(lock) != 1 || lock[0] != first {
			t.Errorf("%s: sources.lock изменён: %+v", tc.name, lock)
		}
		ents, _ := os.ReadDir(filepath.Dir(s.Layout.SourceCache("s")))
		if len(ents) != 1 {
			t.Errorf("%s: мусор рядом с кешем: %v", tc.name, ents)
		}
	}
}

func TestSyncReplacesCacheAndKeepsOtherLockEntries(t *testing.T) {
	t.Parallel()
	const url = "https://x/c.tar.gz"
	v1 := makeTarGz(t, []tarEntry{{name: "cream-catalog.yaml", body: "1"}, {name: "gone", body: "old"}})
	v2 := makeTarGz(t, []tarEntry{{name: "cream-catalog.yaml", body: "2"}, {name: "new", body: "new"}})
	ff := &fakeFetcher{bodies: map[string][]byte{url: v1}}
	s, _ := newSyncer(t, ff)
	dev := filepath.Join(t.TempDir(), "dev")
	writeTree(t, dev, map[string]string{"cream-catalog.yaml": "0.1"})
	if _, err := s.Sync(context.Background(), Source{Name: "dev", Type: TypePath, Path: dev}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sync(context.Background(), Source{Name: "s", Type: TypeArchive, URL: url, SHA256: sha256Hex(v1)}); err != nil {
		t.Fatal(err)
	}
	ff.bodies[url] = v2
	e, err := s.Sync(context.Background(), Source{Name: "s", Type: TypeArchive, URL: url, SHA256: sha256Hex(v2)})
	if err != nil {
		t.Fatal(err)
	}
	cache := s.Layout.SourceCache("s")
	if _, err := os.Lstat(filepath.Join(cache, "gone")); err == nil {
		t.Error("старый файл остался в кеше")
	}
	if readFile(t, filepath.Join(cache, "new")) != "new" {
		t.Error("новый кеш не на месте")
	}
	lock, err := ReadLock(s.Layout.SourcesLock())
	if err != nil || len(lock) != 2 || lock[0].Name != "dev" || lock[1] != e || e.CatalogVersion != "2" {
		t.Errorf("sources.lock = %+v, %v", lock, err)
	}
	ents, _ := os.ReadDir(filepath.Dir(cache))
	if len(ents) != 2 {
		t.Errorf("рядом с кешем: %v", ents)
	}
}

func TestSyncPath(t *testing.T) {
	t.Parallel()
	s, _ := newSyncer(t, nil)
	dev := filepath.Join(t.TempDir(), "dev")
	writeTree(t, dev, map[string]string{"cream-catalog.yaml": "0.1", "ext/x/extension.yaml": "m"})
	if err := os.Symlink("x", filepath.Join(dev, "ext", "y")); err != nil {
		t.Fatal(err)
	}
	e, err := s.Sync(context.Background(), Source{Name: "dev", Type: TypePath, Path: dev})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	digest, err := fsx.TreeDigest(dev)
	if err != nil {
		t.Fatal(err)
	}
	if e.Resolved != digest || e.Origin != dev || e.Type != TypePath || e.CatalogVersion != "0.1" {
		t.Errorf("LockEntry %+v, resolved ждали %s", e, digest)
	}
	cache := s.Layout.SourceCache("dev")
	if st, err := os.Lstat(cache); err != nil || !st.IsDir() {
		t.Fatalf("кеш path — не копия-каталог: %v", err)
	}
	if l, err := os.Readlink(filepath.Join(cache, "ext", "y")); err != nil || l != "x" {
		t.Errorf("ссылка внутри дерева: %q, %v", l, err)
	}

	// Ссылка наружу — отказ, кеш прежний.
	if err := os.Symlink("../../outside", filepath.Join(dev, "ext", "z")); err != nil {
		t.Fatal(err)
	}
	_, err = s.Sync(context.Background(), Source{Name: "dev", Type: TypePath, Path: dev})
	wantCerr(t, err, cerr.ExitSource, "source.path")
	if _, err := os.Lstat(filepath.Join(cache, "ext", "y")); err != nil {
		t.Errorf("прежний кеш тронут: %v", err)
	}

	_, err = s.Sync(context.Background(), Source{Name: "none", Type: TypePath, Path: filepath.Join(dev, "none")})
	wantCerr(t, err, cerr.ExitSource, "source.path")
}

func TestSyncRefusesInvalidSource(t *testing.T) {
	t.Parallel()
	s, _ := newSyncer(t, &fakeFetcher{})
	tests := []struct {
		name     string
		src      Source
		wantExit int
		wantCode string
	}{
		{name: "git", src: Source{Name: "g", Type: "git", URL: "https://x/y.git"}, wantExit: cerr.ExitData, wantCode: "source.reserved"},
		{name: "no sha256", src: Source{Name: "a", Type: TypeArchive, URL: "https://x"}, wantExit: cerr.ExitData, wantCode: "source.invalid"},
		{name: "fetch fails", src: Source{Name: "a", Type: TypeArchive, URL: "https://x/none", SHA256: hexA},
			wantExit: cerr.ExitSource, wantCode: "source.fetch"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Sync(context.Background(), tc.src)
			wantCerr(t, err, tc.wantExit, tc.wantCode)
		})
	}
}

func TestSyncWithCacheFlagKeepsLockInCache(t *testing.T) {
	t.Parallel()
	s, _ := newSyncer(t, nil)
	s.Layout.CacheDir = filepath.Join(t.TempDir(), "bisquite-cache")
	dev := filepath.Join(t.TempDir(), "dev")
	writeTree(t, dev, map[string]string{"cream-catalog.yaml": "1"})
	if _, err := s.Sync(context.Background(), Source{Name: "dev", Type: TypePath, Path: dev}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Layout.CacheDir, "sources.lock")); err != nil {
		t.Errorf("Р12: sources.lock не в каталоге --cache: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Layout.Root, "var/lib/cream/sources.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Р12: общий sources.lock тронут: %v", err)
	}
}

func TestForget(t *testing.T) {
	t.Parallel()
	s, _ := newSyncer(t, nil)
	for _, n := range []string{"a", "b"} {
		dev := filepath.Join(t.TempDir(), n)
		writeTree(t, dev, map[string]string{"cream-catalog.yaml": n})
		if _, err := s.Sync(context.Background(), Source{Name: n, Type: TypePath, Path: dev}); err != nil {
			t.Fatal(err)
		}
	}
	if err := Forget(s.Layout, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(s.Layout.SourceCache("a")); err == nil {
		t.Error("кеш a остался")
	}
	lock, err := ReadLock(s.Layout.SourcesLock())
	if err != nil || len(lock) != 1 || lock[0].Name != "b" {
		t.Errorf("sources.lock = %+v, %v", lock, err)
	}
	if err := Forget(s.Layout, "never-synced"); err != nil {
		t.Errorf("Forget несинхронизированного: %v", err)
	}
}
