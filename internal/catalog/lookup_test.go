package catalog

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/fsx"
	"github.com/iamletenkov/cream/internal/source"
)

func srcDir(name, dir string, synced bool) source.SourceDir {
	return source.SourceDir{Source: source.Source{Name: name, Type: source.TypePath, Path: dir}, Dir: dir, Synced: synced}
}

func TestLookup(t *testing.T) {
	t.Parallel()
	cream := cream100(t)
	cat := filepath.Join(fixturesRoot, "catalog")
	next := filepath.Join(fixturesRoot, "catalog-next")
	twoSources := []source.SourceDir{srcDir("a", cat, true), srcDir("b", next, true)}
	tests := []struct {
		name     string
		dirs     []source.SourceDir
		ref      string
		wantSrc  string
		wantVer  string
		wantWarn []string
		wantExit int
		wantCode string
	}{
		{name: "первый побеждает, затенение", dirs: twoSources, ref: "upgrade-fixture",
			wantSrc: "a", wantVer: "1.0.0", wantWarn: []string{"source.shadowed"}},
		{name: "только в первом", dirs: twoSources, ref: "action-json", wantSrc: "a", wantVer: "1.0.0"},
		{name: "явный источник без затенения", dirs: twoSources, ref: "b/upgrade-fixture",
			wantSrc: "b", wantVer: "2.0.0"},
		{name: "@version совпала", dirs: twoSources, ref: "upgrade-fixture@1.0.0",
			wantSrc: "a", wantVer: "1.0.0", wantWarn: []string{"source.shadowed"}},
		{name: "@version — утверждение, не выбор", dirs: twoSources, ref: "upgrade-fixture@2.0.0",
			wantExit: cerr.ExitNotFound, wantCode: "version.mismatch"},
		{name: "явный источник и @version", dirs: twoSources, ref: "b/upgrade-fixture@2.0.0",
			wantSrc: "b", wantVer: "2.0.0"},
		{name: "нигде нет", dirs: twoSources, ref: "missing",
			wantExit: cerr.ExitNotFound, wantCode: "not_found.extension"},
		{name: "нет источников", dirs: nil, ref: "noop",
			wantExit: cerr.ExitNotFound, wantCode: "not_found.extension"},
		{name: "неизвестный источник", dirs: twoSources, ref: "c/noop",
			wantExit: cerr.ExitNotFound, wantCode: "not_found.source"},
		{name: "в явном источнике нет", dirs: twoSources, ref: "b/action-json",
			wantExit: cerr.ExitNotFound, wantCode: "not_found.extension"},
		{name: "не синхронизирован и больше нигде", dirs: []source.SourceDir{srcDir("u", cat, false), srcDir("b", next, true)},
			ref: "action-json", wantExit: cerr.ExitSource, wantCode: "source.not_synced"},
		{name: "явный несинхронизированный", dirs: []source.SourceDir{srcDir("u", cat, false), srcDir("b", next, true)},
			ref: "u/noop", wantExit: cerr.ExitSource, wantCode: "source.not_synced"},
		{name: "несинхронизированный впереди найденного", dirs: []source.SourceDir{srcDir("u", cat, false), srcDir("b", next, true)},
			ref: "noop", wantSrc: "b", wantVer: "1.0.0", wantWarn: []string{"source.not_synced"}},
		{name: "несинхронизированный позади найденного", dirs: []source.SourceDir{srcDir("b", next, true), srcDir("u", cat, false)},
			ref: "noop", wantSrc: "b", wantVer: "1.0.0"},
		{name: "дефект дерева в кеше", dirs: []source.SourceDir{
			srcDir("x", filepath.Join(fixturesRoot, "invalid", "hook-not-executable"), true)},
			ref: "hook-not-executable", wantExit: cerr.ExitData, wantCode: "manifest.not_executable"},
		{name: "имя каталога не равно name", dirs: []source.SourceDir{
			srcDir("x", filepath.Join(fixturesRoot, "invalid", "dir-name-mismatch"), true)},
			ref: "wrong-dir", wantExit: cerr.ExitData, wantCode: "manifest.dir_name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ref, err := ParseRef(tc.ref)
			if err != nil {
				t.Fatal(err)
			}
			var w cerr.Warnings
			e, err := Lookup(tc.dirs, ref, cream, &w)
			if tc.wantCode != "" {
				if cerr.ExitCode(err) != tc.wantExit || cerr.CodeOf(err) != tc.wantCode {
					t.Fatalf("Lookup(%s) = %v (%d %s), ждали %d %s", tc.ref, err, cerr.ExitCode(err), cerr.CodeOf(err),
						tc.wantExit, tc.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatalf("Lookup(%s): %v", tc.ref, err)
			}
			if e.Source.Source.Name != tc.wantSrc || e.Manifest.Version.String() != tc.wantVer || e.Manifest.Name != ref.Name {
				t.Errorf("найдено %s %s@%s, ждали %s @%s", e.Source.Source.Name, e.Manifest.Name, e.Manifest.Version,
					tc.wantSrc, tc.wantVer)
			}
			var codes []string
			for _, x := range w.List() {
				codes = append(codes, x.Code)
				if !strings.Contains(x.Message, ref.Name) {
					t.Errorf("предупреждение не называет расширение: %q", x.Message)
				}
			}
			if !slices.Equal(codes, tc.wantWarn) {
				t.Errorf("предупреждения %v, ждали %v", codes, tc.wantWarn)
			}
			wantDir := filepath.Join(e.Source.Dir, "extensions", ref.Name)
			if e.Dir != wantDir {
				t.Errorf("Dir = %s, ждали %s", e.Dir, wantDir)
			}
			digest, err := fsx.TreeDigest(e.Dir)
			if err != nil {
				t.Fatal(err)
			}
			if e.Digest != digest {
				t.Errorf("Digest = %s, ждали %s", e.Digest, digest)
			}
		})
	}
}

// TestLookupRoots: расширение ищется во всех roots каталога, по порядку.
func TestLookupRoots(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"cream-catalog.yaml":          "schema: cream/v1\nname: c\nversion: 1.0.0\nroots: [debian, extra]\n",
		"debian/a/extension.yaml":     manifestYAML("a", "hooks: {install: {}}\n"),
		"debian/a/hooks/install":      sh,
		"extra/b/extension.yaml":      manifestYAML("b", "hooks: {install: {}}\n"),
		"extra/b/hooks/install":       sh,
		"extra/notdir":                "файл, не расширение\n",
		"debian/notext/README.md":     "каталог без манифеста\n",
		"extra/notext/extension.yaml": manifestYAML("notext", "hooks: {install: {}}\n"),
		"extra/notext/hooks/install":  sh,
	}, "debian/a/hooks/install", "extra/b/hooks/install", "extra/notext/hooks/install")
	dirs := []source.SourceDir{srcDir("s", dir, true)}
	cream := cream100(t)
	for _, tc := range []struct{ name, wantDir string }{
		{"a", "debian/a"},
		{"b", "extra/b"},
	} {
		var w cerr.Warnings
		e, err := Lookup(dirs, Ref{Name: tc.name}, cream, &w)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if e.Dir != filepath.Join(dir, tc.wantDir) {
			t.Errorf("%s: Dir = %s", tc.name, e.Dir)
		}
	}
	// Каталог с именем расширения в первом root — это и есть расширение: без
	// манифеста он даёт ошибку, а не уступает одноимённому во втором root.
	var w cerr.Warnings
	if _, err := Lookup(dirs, Ref{Name: "notext"}, cream, &w); cerr.CodeOf(err) != "not_found.manifest" {
		t.Errorf("каталог без манифеста в первом root: %v", err)
	}
	if _, err := Lookup(dirs, Ref{Name: "notdir"}, cream, &w); cerr.CodeOf(err) != "not_found.extension" {
		t.Errorf("файл вместо каталога: %v", err)
	}
}
