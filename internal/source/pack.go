package source

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"syscall"
	"time"
)

// Pack writes dir as a deterministic tar.gz (§11.2, `cream catalog pack`):
// entries sorted by name, no top-level directory, uid/gid 0 without names,
// every mtime = epoch (SOURCE_DATE_EPOCH), file modes reduced to 0644/0755,
// directories 0755, links kept as links (a link outside dir is refused, as
// on extraction), and a gzip header without name and mtime (gzip -n). The
// same tree and epoch always give the same bytes. Validation of the catalog
// is the caller's job.
func Pack(dir string, w io.Writer, epoch time.Time) error {
	st, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("упаковка %s: %w", dir, err)
	}
	if !st.IsDir() {
		return fmt.Errorf("упаковка %s: не каталог", dir)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return fmt.Errorf("упаковка %s: %w", dir, err)
	}
	defer func() { _ = root.Close() }()
	type entry struct {
		name string // tar name: directories end with "/"
		rel  string
		typ  fs.FileMode
	}
	var entries []entry
	err = fs.WalkDir(root.FS(), ".", func(rel string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if rel == "." {
			return nil
		}
		t := d.Type()
		switch {
		case t.IsDir():
			entries = append(entries, entry{name: rel + "/", rel: rel, typ: t})
		case t.IsRegular(), t&fs.ModeSymlink != 0:
			entries = append(entries, entry{name: rel, rel: rel, typ: t})
		default:
			return fmt.Errorf("%s: особый файл (%v) не поддерживается", rel, t)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("упаковка %s: %w", dir, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })

	mtime := epoch.UTC().Truncate(time.Second)
	zw, err := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(zw)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, ModTime: mtime, Format: tar.FormatPAX}
		var body *os.File
		switch {
		case e.typ.IsDir():
			h.Typeflag, h.Mode = tar.TypeDir, 0o755
		case e.typ&fs.ModeSymlink != 0:
			target, err := root.Readlink(e.rel)
			if err != nil {
				return fmt.Errorf("упаковка %s: %w", e.rel, err)
			}
			if err := checkLink(e.rel, target); err != nil {
				return fmt.Errorf("упаковка %s: %w", e.rel, err)
			}
			h.Typeflag, h.Mode, h.Linkname = tar.TypeSymlink, 0o777, target
		default:
			f, fst, err := openPackFile(root, e.rel)
			if err != nil {
				return fmt.Errorf("упаковка %s: %w", e.rel, err)
			}
			body = f
			h.Typeflag, h.Mode, h.Size = tar.TypeReg, int64(modeOf(fst.Mode())), fst.Size()
		}
		if err := writeEntry(tw, h, body); err != nil {
			return fmt.Errorf("упаковка %s: %w", e.rel, err)
		}
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("упаковка %s: %w", dir, err)
	}
	return zw.Close()
}

// writeEntry writes the header and, for a regular file, exactly Size bytes
// of body, closing it.
func writeEntry(tw *tar.Writer, h *tar.Header, body *os.File) error {
	if body == nil {
		return tw.WriteHeader(h)
	}
	defer func() { _ = body.Close() }()
	if err := tw.WriteHeader(h); err != nil {
		return err
	}
	n, err := io.Copy(tw, io.LimitReader(body, h.Size))
	if err != nil {
		return err
	}
	if n != h.Size {
		return errors.New("файл изменился во время упаковки")
	}
	return nil
}

// openPackFile opens the regular file rel without following a link and
// checks that the descriptor is that very entry.
func openPackFile(root *os.Root, rel string) (*os.File, fs.FileInfo, error) {
	f, err := root.OpenFile(rel, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, err
	}
	st, err := f.Stat()
	if err == nil {
		var lst fs.FileInfo
		if lst, err = root.Lstat(rel); err == nil && (!st.Mode().IsRegular() || !os.SameFile(st, lst)) {
			err = errors.New("файл подменён во время упаковки")
		}
	}
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	return f, st, nil
}
