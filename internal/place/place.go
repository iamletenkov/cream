// Package place lays out the "files" of an extension (spec §4.2) and takes
// them back on remove (§6.5): every placed file is recorded with the sha256
// of its content, ownership of a destination is decided by the registry, and
// a file changed since placement is kept as <dst>.cream-modified.
//
// Destinations are absolute paths on the machine; on disk they live under
// paths.Layout.Root (--root, the guest in the build phase).
package place

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/fsx"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/paths"
	"github.com/iamletenkov/cream/internal/state"
)

// Codes of errors and warnings of this package.
const (
	// CodeFileOwner (exit 67): dst is recorded in another extension's registry.
	CodeFileOwner = "conflict.file_owner"
	// CodeFileUnowned (exit 67): dst exists on disk but no registry lists it (ruling Р21).
	CodeFileUnowned = "conflict.file_unowned"
	// CodeFileModified (warning): a file changed since placement was kept as <dst>.cream-modified.
	CodeFileModified = "file.modified"
	// CodeFileStat (exit 74): dst could not be checked on disk (Lstat failed, not ENOENT).
	CodeFileStat = "file.stat"
	// CodeFileNotRegular (warning): the recorded dst is not a regular file any more; left alone.
	CodeFileNotRegular = "file.not_regular"
)

// ModifiedSuffix is appended to a changed file Unplace keeps.
const ModifiedSuffix = ".cream-modified"

// Owner reports which extension's registry lists dst. The caller builds it
// from all registry records of every status, the extension's own included
// (a failed install leaves its placed files there, and they stay its own);
// ruling Р11: ownership is the registry, not the directory the file lies in.
// nil means an empty registry.
type Owner func(dst string) (ext string, ok bool)

// Place copies files from the extension tree to their destinations under
// l.Root, for extension ext. Ownership of every dst is checked before
// anything is written: a dst listed by another extension is exit 67
// conflict.file_owner; a dst of ext itself is simply replaced; a dst that
// exists on disk (by Lstat, a symlink included) and is listed in no registry
// is exit 67 conflict.file_unowned (ruling Р21). Parent
// directories are created (0755) when missing. The mode is File.Mode, or
// 0755/0644 by the execute bit of the source. The returned records carry the
// hex sha256 of the content written; on an error in the middle they list the
// files already placed, so the caller can record or remove them.
//
// Parent directories are created with MkdirAll, which follows symlinks: an
// absolute symlink among the parents under a --root other than "/" leads out
// of the root. The build phase owns the guest tree, so this is accepted.
func Place(l paths.Layout, ext, tree string, files []manifest.File, owner Owner) ([]state.FileRec, error) {
	for _, f := range files {
		var other string
		var ok bool
		if owner != nil {
			other, ok = owner(f.Dst)
		}
		if ok && other != ext {
			e := cerr.New(cerr.ExitGraph, CodeFileOwner, "файл %s уже принадлежит расширению %s", f.Dst, other)
			e.Details = map[string]any{"dst": f.Dst, "owner": other}
			return nil, e
		}
		if ok {
			continue
		}
		_, err := os.Lstat(onDisk(l, f.Dst))
		if err == nil {
			e := cerr.New(cerr.ExitGraph, CodeFileUnowned, "файл %s уже есть на машине и не принадлежит ни одному расширению", f.Dst)
			e.Details = map[string]any{"dst": f.Dst}
			return nil, e
		}
		if !errors.Is(err, fs.ErrNotExist) {
			e := cerr.Wrap(err, cerr.ExitState, CodeFileStat, "раскладка files %s: проверка %s", ext, f.Dst)
			e.Details = map[string]any{"dst": f.Dst}
			return nil, e
		}
	}
	src, err := os.OpenRoot(tree)
	if err != nil {
		return nil, fmt.Errorf("раскладка files %s: %w", ext, err)
	}
	defer func() { _ = src.Close() }()
	recs := make([]state.FileRec, 0, len(files))
	for _, f := range files {
		rec, err := placeOne(l, src, f)
		if err != nil {
			return recs, fmt.Errorf("раскладка files %s: %w", ext, err)
		}
		recs = append(recs, rec)
	}
	return recs, nil
}

func placeOne(l paths.Layout, src *os.Root, f manifest.File) (state.FileRec, error) {
	data, srcMode, err := readSource(src, f.Src)
	if err != nil {
		return state.FileRec{}, err
	}
	mode := f.Mode
	if mode == 0 {
		mode = 0o644
		if srcMode&0o111 != 0 {
			mode = 0o755
		}
	}
	p := onDisk(l, f.Dst)
	if st, err := os.Lstat(p); err == nil && st.IsDir() {
		return state.FileRec{}, fmt.Errorf("%s: на месте файла каталог", f.Dst)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return state.FileRec{}, err
	}
	if err := fsx.WriteAtomic(p, data, mode); err != nil {
		return state.FileRec{}, err
	}
	return state.FileRec{Dst: f.Dst, SHA256: digest(data)}, nil
}

// readSource reads a regular file inside the tree; links may not lead out of it.
func readSource(src *os.Root, name string) ([]byte, fs.FileMode, error) {
	fh, err := src.Open(name)
	if err != nil {
		return nil, 0, fmt.Errorf("src %s: %w", name, err)
	}
	defer func() { _ = fh.Close() }()
	st, err := fh.Stat()
	if err != nil {
		return nil, 0, fmt.Errorf("src %s: %w", name, err)
	}
	if !st.Mode().IsRegular() {
		return nil, 0, fmt.Errorf("src %s: не обычный файл", name)
	}
	data, err := io.ReadAll(fh)
	if err != nil {
		return nil, 0, fmt.Errorf("src %s: %w", name, err)
	}
	return data, st.Mode().Perm(), nil
}

// Unplace removes the recorded files: an unchanged one is deleted, a changed
// one is renamed to <dst>.cream-modified with a file.modified warning, a
// missing one is skipped (ruling Р11: another extension's purge may have
// taken its directory), anything but a regular file is left alone with a
// file.not_regular warning. Parent directories stay. All records are
// processed; the errors are joined.
func Unplace(l paths.Layout, recs []state.FileRec, w *cerr.Warnings) error {
	var errs []error
	for _, r := range recs {
		if err := unplaceOne(l, r, w); err != nil {
			errs = append(errs, fmt.Errorf("удаление %s: %w", r.Dst, err))
		}
	}
	return errors.Join(errs...)
}

func unplaceOne(l paths.Layout, r state.FileRec, w *cerr.Warnings) error {
	p := onDisk(l, r.Dst)
	st, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		warn(w, CodeFileNotRegular, "%s: не обычный файл, оставлен как есть", r.Dst)
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	if digest(data) == r.SHA256 {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	keep, err := freeModifiedName(p)
	if err != nil {
		return err
	}
	if err := os.Rename(p, keep); err != nil {
		return err
	}
	kept := r.Dst + keep[len(p):]
	warn(w, CodeFileModified, "%s изменён после установки, сохранён как %s", r.Dst, kept)
	return nil
}

// freeModifiedName is <p>.cream-modified, or <p>.cream-modified.N with the
// first free N when earlier copies exist: an older kept copy is never replaced.
func freeModifiedName(p string) (string, error) {
	name := p + ModifiedSuffix
	for n := 1; ; n++ {
		_, err := os.Lstat(name)
		if errors.Is(err, fs.ErrNotExist) {
			return name, nil
		}
		if err != nil {
			return "", err
		}
		name = fmt.Sprintf("%s%s.%d", p, ModifiedSuffix, n)
	}
}

func warn(w *cerr.Warnings, code, format string, a ...any) {
	if w != nil {
		w.Add(code, format, a...)
	}
}

// onDisk is dst under the layout's root.
func onDisk(l paths.Layout, dst string) string {
	root := l.Root
	if root == "" {
		root = "/"
	}
	return filepath.Join(root, dst)
}

func digest(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
