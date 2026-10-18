package manifest

import (
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/semver"
)

type rawCatalog struct {
	Schema  string   `yaml:"schema"`
	Name    string   `yaml:"name"`
	Version string   `yaml:"version"`
	Roots   []string `yaml:"roots"`
}

// ParseCatalog разбирает и проверяет cream-catalog.yaml.
func ParseCatalog(data []byte) (*Catalog, error) {
	root, err := parseNode(data, codeCatInvalid, "каталог")
	if err != nil {
		return nil, err
	}
	if s := topScalar(root, "schema"); s != SchemaV1 {
		if s == "" {
			return nil, cerr.New(cerr.ExitData, codeCatSchema, "каталог: нет поля schema (нужно %s)", SchemaV1)
		}
		return nil, cerr.New(cerr.ExitData, codeCatSchema, "каталог: неизвестная схема %q (нужно %s)", s, SchemaV1)
	}
	var raw rawCatalog
	if err := decodeStrict(root, &raw, codeCatInvalid); err != nil {
		return nil, err
	}
	return validateCatalog(&raw)
}

// LoadCatalog читает dir/cream-catalog.yaml и разбирает его ParseCatalog.
func LoadCatalog(dir string) (*Catalog, error) {
	path := filepath.Join(dir, CatalogFile)
	data, err := readLimited(path, "not_found.catalog", codeCatInvalid)
	if err != nil {
		return nil, err
	}
	c, err := ParseCatalog(data)
	if err != nil {
		return nil, wrapPath(err, path)
	}
	return c, nil
}

func validateCatalog(r *rawCatalog) (*Catalog, error) {
	bad := func(field, format string, a ...any) error {
		e := cerr.New(cerr.ExitData, codeCatInvalid, "каталог: %s: %s", field, fmt.Sprintf(format, a...))
		e.Details = map[string]any{"field": field}
		return e
	}
	c := &Catalog{Schema: SchemaV1}
	if !reName.MatchString(r.Name) {
		return nil, bad("name", "%q: ожидается [a-z0-9][a-z0-9._-]{0,63}", r.Name)
	}
	c.Name = r.Name
	if r.Version == "" {
		return nil, bad("version", "обязательное поле")
	}
	v, err := semver.Parse(r.Version)
	if err != nil {
		return nil, bad("version", "%v", err)
	}
	c.Version = v
	if len(r.Roots) == 0 {
		return nil, bad("roots", "непустой список каталогов")
	}
	for i, root := range r.Roots {
		field := fmt.Sprintf("roots[%d]", i)
		if root == "" || path.IsAbs(root) || path.Clean(root) != root || root == ".." || strings.HasPrefix(root, "../") {
			return nil, bad(field, "%q: ожидается чистый относительный путь внутри каталога", root)
		}
		if slices.Contains(r.Roots[:i], root) {
			return nil, bad(field, "%q повторяется", root)
		}
	}
	c.Roots = r.Roots
	return c, nil
}
