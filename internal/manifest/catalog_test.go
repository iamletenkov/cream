package manifest

import (
	"slices"
	"strings"
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
)

const catalogBase = `schema: cream/v1
name: cream-extensions
version: 4.0.0
roots: [extensions]
`

func TestParseCatalog(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		yaml     string
		wantExit int
		wantCode string
	}{
		{name: "базовый", yaml: catalogBase},
		{name: "несколько roots", yaml: strings.Replace(catalogBase, "[extensions]", "[extensions/debian, extensions/openwrt]", 1)},
		{name: "root — сам корень", yaml: strings.Replace(catalogBase, "[extensions]", "[.]", 1)},
		{name: "x- поле", yaml: catalogBase + "x-maintainer: ops\n"},
		{name: "нет schema", yaml: strings.Replace(catalogBase, "schema: cream/v1\n", "", 1), wantExit: 65, wantCode: "catalog.schema"},
		{name: "чужая схема", yaml: strings.Replace(catalogBase, "cream/v1", "cream/v0", 1), wantExit: 65, wantCode: "catalog.schema"},
		{name: "пустой файл", yaml: "", wantExit: 65, wantCode: "catalog.invalid"},
		{name: "нет имени", yaml: strings.Replace(catalogBase, "name: cream-extensions\n", "", 1), wantExit: 65, wantCode: "catalog.invalid"},
		{name: "имя с заглавной", yaml: strings.Replace(catalogBase, "cream-extensions", "Cream", 1), wantExit: 65, wantCode: "catalog.invalid"},
		{name: "нет версии", yaml: strings.Replace(catalogBase, "version: 4.0.0\n", "", 1), wantExit: 65, wantCode: "catalog.invalid"},
		{name: "версия не semver", yaml: strings.Replace(catalogBase, "4.0.0", "\"4.0\"", 1), wantExit: 65, wantCode: "catalog.invalid"},
		{name: "нет roots", yaml: strings.Replace(catalogBase, "roots: [extensions]\n", "", 1), wantExit: 65, wantCode: "catalog.invalid"},
		{name: "пустой roots", yaml: strings.Replace(catalogBase, "[extensions]", "[]", 1), wantExit: 65, wantCode: "catalog.invalid"},
		{name: "roots скаляр", yaml: strings.Replace(catalogBase, "[extensions]", "extensions", 1), wantExit: 65, wantCode: "catalog.invalid"},
		{name: "root абсолютный", yaml: strings.Replace(catalogBase, "[extensions]", "[/srv/ext]", 1), wantExit: 65, wantCode: "catalog.invalid"},
		{name: "root наружу", yaml: strings.Replace(catalogBase, "[extensions]", "[../ext]", 1), wantExit: 65, wantCode: "catalog.invalid"},
		{name: "root не чистый", yaml: strings.Replace(catalogBase, "[extensions]", "[extensions/]", 1), wantExit: 65, wantCode: "catalog.invalid"},
		{name: "root пустой", yaml: strings.Replace(catalogBase, "[extensions]", "[\"\"]", 1), wantExit: 65, wantCode: "catalog.invalid"},
		{name: "root дубль", yaml: strings.Replace(catalogBase, "[extensions]", "[extensions, extensions]", 1), wantExit: 65, wantCode: "catalog.invalid"},
		{name: "неизвестное поле", yaml: catalogBase + "family: deb\n", wantExit: 65, wantCode: "catalog.invalid"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, err := ParseCatalog([]byte(tc.yaml))
			if tc.wantExit == 0 {
				if err != nil {
					t.Fatalf("ParseCatalog: %v", err)
				}
				if c == nil {
					t.Fatal("nil без ошибки")
				}
				return
			}
			if err == nil {
				t.Fatalf("без ошибки, ждали %d %s", tc.wantExit, tc.wantCode)
			}
			if got := cerr.ExitCode(err); got != tc.wantExit {
				t.Errorf("код %d, ждали %d: %v", got, tc.wantExit, err)
			}
			if got := cerr.CodeOf(err); got != tc.wantCode {
				t.Errorf("error.code %q, ждали %q: %v", got, tc.wantCode, err)
			}
		})
	}
}

func TestParseCatalogValues(t *testing.T) {
	t.Parallel()
	c, err := ParseCatalog([]byte(catalogBase))
	if err != nil {
		t.Fatal(err)
	}
	if c.Schema != SchemaV1 || c.Name != "cream-extensions" || c.Version.String() != "4.0.0" ||
		!slices.Equal(c.Roots, []string{"extensions"}) {
		t.Errorf("%+v", c)
	}
}
