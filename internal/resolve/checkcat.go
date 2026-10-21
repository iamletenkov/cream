package resolve

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/source"
)

// Source types of a check catalog in the registry form (state.SourceRef).
const (
	checkTypeArchive = "archive"
	checkTypePath    = "path"
)

// CheckCatalog is one --catalog of cream check (spec 2026-10-21 §10.3): a
// catalog read in place, without sources.yaml, a cache or unpacking to disk.
// An archive keeps in memory only cream-catalog.yaml and the extension.yaml
// files, read under the rules of the source extraction (no `..`, no links
// out, size limits); a directory is read through os.Root.
type CheckCatalog struct {
	name, typ string
	cat       *manifest.Catalog
	mem       *source.MemTree
	root      *os.Root
}

// OpenCheckCatalog opens the catalog at path (a tar.gz file or a
// directory) under name. Errors: 69 source.path (no such path, neither a
// file nor a directory), 69 source.bad_archive, 65 source.catalog (no
// valid cream-catalog.yaml).
func OpenCheckCatalog(name, p string) (*CheckCatalog, error) {
	st, err := os.Stat(p)
	if err != nil {
		return nil, cerr.Wrap(err, cerr.ExitSource, "source.path", "каталог %s: %s", name, p)
	}
	c := &CheckCatalog{name: name}
	switch {
	case st.Mode().IsRegular():
		c.typ = checkTypeArchive
		f, err := os.Open(p)
		if err != nil {
			return nil, cerr.Wrap(err, cerr.ExitSource, "source.path", "каталог %s: %s", name, p)
		}
		defer func() { _ = f.Close() }()
		keep := func(rel string) bool {
			b := path.Base(rel)
			return b == manifest.CatalogFile || b == manifest.ManifestFile
		}
		if c.mem, err = source.ReadTarGz(f, keep); err != nil {
			return nil, cerr.Wrap(err, cerr.ExitSource, "source.bad_archive", "каталог %s: %s", name, p)
		}
	case st.IsDir():
		c.typ = checkTypePath
		if c.root, err = os.OpenRoot(p); err != nil {
			return nil, cerr.Wrap(err, cerr.ExitSource, "source.path", "каталог %s: %s", name, p)
		}
	default:
		return nil, cerr.New(cerr.ExitSource, "source.path", "каталог %s: %s — не файл и не каталог", name, p)
	}
	data, err := c.read(manifest.CatalogFile)
	if err == nil {
		c.cat, err = manifest.ParseCatalog(data)
	}
	if err != nil {
		_ = c.Close()
		return nil, cerr.Wrap(err, cerr.ExitData, "source.catalog", "каталог %s: %s: нет годного %s", name, p,
			manifest.CatalogFile)
	}
	return c, nil
}

// Name is the source name the catalog was given.
func (c *CheckCatalog) Name() string { return c.name }

// Version is the catalog version of its cream-catalog.yaml.
func (c *CheckCatalog) Version() string { return c.cat.Version.String() }

// Close releases the directory of a catalog read in place.
func (c *CheckCatalog) Close() error {
	if c.root != nil {
		return c.root.Close()
	}
	return nil
}

// read returns the file at rel (fs.ErrNotExist when absent), at most the
// manifest size limit.
func (c *CheckCatalog) read(rel string) ([]byte, error) {
	if c.mem != nil {
		data, ok := c.mem.Files[rel]
		if !ok {
			return nil, fs.ErrNotExist
		}
		return data, nil
	}
	st, err := c.root.Stat(rel)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: не обычный файл", rel)
	}
	f, err := c.root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, checkMaxFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > checkMaxFile {
		return nil, fmt.Errorf("%s: больше %d байт", rel, checkMaxFile)
	}
	return data, nil
}

// checkMaxFile bounds a manifest read from a catalog directory.
const checkMaxFile = 1 << 20

// isDir reports a real directory at rel: every component a directory, not
// a link (as catalog.Lookup sees a cache).
func (c *CheckCatalog) isDir(rel string) bool {
	if c.mem != nil {
		return rel == "." || c.mem.Dirs[rel]
	}
	cur := ""
	for _, part := range strings.Split(rel, "/") {
		cur = path.Join(cur, part)
		st, err := c.root.Lstat(cur)
		if err != nil || !st.IsDir() {
			return false
		}
	}
	return true
}

// find returns the manifest of extension name in the catalog, nil when no
// root has a directory of that name. A directory of the name is the
// extension: a defect in it is an error (66 not_found.manifest, 65
// manifest.*, 68 platform.cream_version), not a reason to look further.
func (c *CheckCatalog) find(name string, cream manifest.CreamBuild, w *cerr.Warnings) (*manifest.Manifest, error) {
	for _, root := range c.cat.Roots {
		dir := path.Join(root, name)
		if !c.isDir(root) || !c.isDir(dir) {
			continue
		}
		rel := path.Join(dir, manifest.ManifestFile)
		data, err := c.read(rel)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, cerr.Wrap(err, cerr.ExitNotFound, "not_found.manifest", "%s/%s: нет файла", c.name, rel)
		}
		if err != nil {
			return nil, cerr.Wrap(err, cerr.ExitState, cerr.CodeIORead, "%s/%s", c.name, rel)
		}
		m, err := manifest.Parse(data, cream, w)
		if err != nil {
			var ce *cerr.Error
			if errors.As(err, &ce) {
				ce.Message = c.name + "/" + rel + ": " + ce.Message
				return nil, ce
			}
			return nil, err
		}
		if m.Name != name {
			ce := cerr.New(cerr.ExitData, "manifest.dir_name", "%s/%s: имя каталога %s не равно name %s",
				c.name, dir, name, m.Name)
			ce.Details = map[string]any{"ext": name}
			return nil, ce
		}
		return m, nil
	}
	return nil, nil
}
