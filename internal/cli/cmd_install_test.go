package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iamletenkov/cream/internal/catalog"
	"github.com/iamletenkov/cream/internal/fsx"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/paths"
	"github.com/iamletenkov/cream/internal/source"
)

// installCLICatalog собирает под root каталог srv/<name> версии version из
// расширений exts (имя → каталог фикстуры): путь — под <ROOT>, отпечаток не
// зависит от прочих фикстур.
func installCLICatalog(t *testing.T, root, name, version string, exts map[string]string) string {
	t.Helper()
	dir := filepath.Join(root, "srv", name)
	writeRootFile(t, root, "/srv/"+name+"/cream-catalog.yaml",
		"schema: cream/v1\nname: cream-fixtures\nversion: "+version+"\nroots: [extensions]\n", 0o644)
	for ext, src := range exts {
		if err := fsx.CopyTree(src, filepath.Join(dir, "extensions", ext)); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// installCLISources пишет sources.yaml под root (источник fixtures — path
// dir) и синхронизирует его, как cream source sync.
func installCLISources(t *testing.T, root, dir string) {
	t.Helper()
	const name = "fixtures"
	l := paths.Layout{Root: root}
	f := &source.File{Sources: []source.Source{{Name: name, Type: source.TypePath, Path: dir}}}
	if err := os.MkdirAll(filepath.Dir(l.Sources()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(l.Sources()); err != nil {
		t.Fatal(err)
	}
	cream := manifest.ParseCreamBuild("1.0.0")
	sy := &source.Syncer{Layout: l, Now: func() time.Time { return cliNow }, Validate: func(d string) (string, error) {
		r, err := catalog.Validate(d, cream)
		return r.CatalogVersion, err
	}}
	if _, err := sy.Sync(context.Background(), f.Sources[0]); err != nil {
		t.Fatal(err)
	}
}

// installCLIRoot — корень «Debian 12» с источником fixtures из noop,
// lib-fixture, depends-lib и upgrade-fixture 1.0.0.
func installCLIRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeRootFile(t, root, "/etc/os-release", "ID=debian\nVERSION_ID=12\n", 0o644)
	dir := installCLICatalog(t, root, "catalog", "1.0.0", map[string]string{
		"noop": fixtureDir(t, "noop"), "lib-fixture": fixtureDir(t, "lib-fixture"),
		"depends-lib": fixtureDir(t, "depends-lib"), "upgrade-fixture": fixtureDir(t, "upgrade-fixture")})
	installCLISources(t, root, dir)
	return root
}

// TestGoldenInstall — формы result install/upgrade (§11.4): запись реестра + changed.
func TestGoldenInstall(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		before [][]string
		args   []string
		next   bool // перед вызовом источник fixtures переключён на upgrade-fixture 2.0.0
		// prevNext: before идёт по источнику с upgrade-fixture 2.0.0, перед вызовом
		// источник fixtures возвращается к 1.0.0 (понижение).
		prevNext bool
		wantExit int
	}{
		{name: "install-noop", args: []string{"install", "--json", "noop"}},
		{name: "install-unchanged", before: [][]string{{"install", "noop"}}, args: []string{"install", "--json", "noop"}},
		{name: "install-several", args: []string{"install", "--json", "depends-lib", "lib-fixture"}},
		{name: "install-deps-missing", args: []string{"install", "--json", "depends-lib"}, wantExit: 67},
		{name: "install-several-params", args: []string{"install", "--json", "noop", "lib-fixture", "X=1"}, wantExit: 64},
		{name: "install-with-deps", args: []string{"install", "--json", "--with-deps", "depends-lib"}},
		{name: "upgrade", before: [][]string{{"install", "upgrade-fixture"}}, next: true,
			args: []string{"upgrade", "--json", "upgrade-fixture"}},
		{name: "upgrade-all", before: [][]string{{"install", "upgrade-fixture"}, {"install", "noop"}}, next: true,
			args: []string{"upgrade", "--json", "--all"}},
		{name: "install-downgrade-refused", before: [][]string{{"install", "upgrade-fixture"}}, prevNext: true,
			args: []string{"install", "--json", "upgrade-fixture"}, wantExit: 65},
		{name: "install-downgrade", before: [][]string{{"install", "upgrade-fixture"}}, prevNext: true,
			args: []string{"install", "--json", "--allow-downgrade", "upgrade-fixture"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := installCLIRoot(t)
			if tc.prevNext {
				installCLISources(t, root, installCLICatalog(t, root, "next", "1.1.0", map[string]string{
					"upgrade-fixture": cliTestdataDir(t, "catalog-next", "extensions", "upgrade-fixture")}))
			}
			for _, b := range tc.before {
				if out := runCLI(t, cliIn{Root: root}, b...); out.Code != 0 {
					t.Fatalf("%v: %d\n%s", b, out.Code, out.Stderr)
				}
			}
			if tc.prevNext {
				installCLISources(t, root, filepath.Join(root, "srv", "catalog"))
			}
			if tc.next {
				dir := installCLICatalog(t, root, "next", "1.1.0", map[string]string{
					"noop":            fixtureDir(t, "noop"),
					"upgrade-fixture": cliTestdataDir(t, "catalog-next", "extensions", "upgrade-fixture")})
				installCLISources(t, root, dir)
			}
			out := runCLI(t, cliIn{Root: root}, tc.args...)
			if out.Code != tc.wantExit {
				t.Errorf("exit = %d, want %d; stderr:\n%s", out.Code, tc.wantExit, out.Stderr)
			}
			golden(t, tc.name, normRoot(out.Stdout, root))
		})
	}
}

func TestInstallCommandText(t *testing.T) {
	t.Parallel()
	root := installCLIRoot(t)
	in := cliIn{Root: root}
	if out := runCLI(t, in, "install", "noop"); out.Code != 0 || out.Stdout != "noop 1.0.0: installed (изменено)\n" {
		t.Errorf("install: %+v", out)
	}
	if out := runCLI(t, in, "install", "noop"); out.Code != 0 || out.Stdout != "noop 1.0.0: installed (без изменений)\n" {
		t.Errorf("повтор: %+v", out)
	}
	// --allow-downgrade больше не зарезервирован: повтор той же версии и отпечатка — без изменений.
	if out := runCLI(t, in, "install", "--allow-downgrade", "noop"); out.Code != 0 ||
		out.Stdout != "noop 1.0.0: installed (без изменений)\n" {
		t.Errorf("--allow-downgrade: %+v", out)
	}
	if out := runCLI(t, in, "--json", "install", "--require-pinned", "noop"); out.Code != 64 ||
		!strings.Contains(out.Stdout, `"usage.reserved"`) {
		t.Errorf("--require-pinned: %+v", out)
	}
	if out := runCLI(t, in, "upgrade"); out.Code != 64 {
		t.Errorf("upgrade без имён: %+v", out)
	}
	if out := runCLI(t, in, "install"); out.Code != 64 {
		t.Errorf("install без ссылки: %+v", out)
	}
	// Без --root (настоящий корень) и не root — 77.
	if out := runCLI(t, cliIn{Euid: 1000}, "install", "noop"); out.Code != 77 || !strings.Contains(out.Stderr, "root") {
		t.Errorf("не root: %+v", out)
	}
}
