package catalog

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
)

// graphCatalog — каталог фикстур графа (testdata/graph).
func graphCatalog() string { return filepath.Join(fixturesRoot, "graph") }

// ValidateCatalog (cream 1.1.0): граф каталога. depends на отсутствующее имя,
// с невыполнимым диапазоном и цикл depends — ошибки 65; requires без
// поставщика и conflicts.extensions на чужое имя — предупреждения;
// неприменимое (Р22) расширение считается присутствующим.
func TestValidateCatalogGraph(t *testing.T) {
	t.Parallel()
	r, err := ValidateCatalog(graphCatalog(), cream100(t))
	type pe struct{ ext, code string }
	var got []pe
	for _, p := range r.Errors {
		got = append(got, pe{p.Ext, p.Code})
		if p.Exit != cerr.ExitData || p.Path != "extensions/"+p.Ext+"/extension.yaml" {
			t.Errorf("%+v: exit/path", p)
		}
	}
	want := []pe{{"app", "catalog.depends"}, {"orphan", "catalog.depends"}, {"cyc-a", "catalog.depends_cycle"}}
	if !slices.Equal(got, want) {
		t.Fatalf("ошибки %+v, ждали %+v", got, want)
	}
	if cerr.ExitCode(err) != cerr.ExitData || cerr.CodeOf(err) != "catalog.depends" {
		t.Errorf("err = %v", err)
	}
	for _, sub := range []string{"lib-a >=2.0.0 <3", "1.0.0", "ghost", "cyc-a → cyc-b → cyc-a"} {
		if !strings.Contains(r.Errors[0].Message+r.Errors[1].Message+r.Errors[2].Message, sub) {
			t.Errorf("в сообщениях нет %q: %+v", sub, r.Errors)
		}
	}
	var warns []string
	for _, w := range r.Warnings {
		if w.Code != warnCreamNewer {
			warns = append(warns, w.Code+" "+w.Details["ext"].(string))
		}
	}
	if want := []string{"catalog.requires needy", "catalog.conflicts picky"}; !slices.Equal(warns, want) {
		t.Errorf("предупреждения %v, ждали %v", warns, want)
	}
}

// Граф проверяет только ValidateCatalog (cream validate --catalog): синхронизация
// и catalog pack по-прежнему на Validate, их поведение не меняется.
func TestValidateWithoutGraph(t *testing.T) {
	t.Parallel()
	r, err := Validate(graphCatalog(), cream100(t))
	if err != nil || len(r.Errors) != 0 {
		t.Fatalf("Validate: %v %+v", err, r.Errors)
	}
	for _, w := range r.Warnings {
		if strings.HasPrefix(w.Code, "catalog.requires") || strings.HasPrefix(w.Code, "catalog.conflicts") {
			t.Errorf("предупреждение графа в Validate: %+v", w)
		}
	}
}

// Каталоги фикстур §14 графу соответствуют.
func TestValidateCatalogFixturesClean(t *testing.T) {
	t.Parallel()
	for _, dir := range []string{"catalog", "catalog-next", "pilot"} {
		r, err := ValidateCatalog(filepath.Join(fixturesRoot, dir), cream100(t))
		if err != nil {
			t.Errorf("%s: %v %+v", dir, err, r.Errors)
		}
		for _, w := range r.Warnings {
			if w.Code == warnRequires || w.Code == warnConflicts {
				t.Errorf("%s: %+v", dir, w)
			}
		}
	}
}
