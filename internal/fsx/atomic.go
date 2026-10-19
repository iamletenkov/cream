// Package fsx holds the file-system primitives cream relies on: atomic
// writes, tree copies without following links, tree digests and the
// owner/permission check for files that carry secrets.
package fsx

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// WriteAtomic replaces path with data: a temporary file in the same
// directory is written, chmod'ed to perm (umask does not apply), fsync'ed,
// renamed over path, and the directory is fsync'ed. Readers see either the
// old content or the new one, never a mix.
func WriteAtomic(path string, data []byte, perm fs.FileMode) error {
	return writeAtomic(path, data, perm, nil)
}

// writeAtomic is WriteAtomic with a hook between the durable write of the
// temporary file and the rename; tests inject a crash there.
func writeAtomic(path string, data []byte, perm fs.FileMode, beforeRename func(tmp string) error) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("запись %s: %w", path, err)
	}
	name := tmp.Name()
	renamed := false
	defer func() {
		if err != nil {
			_ = tmp.Close()
			if !renamed {
				_ = os.Remove(name)
			}
		}
	}()
	if err = tmp.Chmod(perm); err != nil {
		return fmt.Errorf("запись %s: %w", path, err)
	}
	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("запись %s: %w", path, err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("запись %s: fsync: %w", path, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("запись %s: %w", path, err)
	}
	if beforeRename != nil {
		if err = beforeRename(name); err != nil {
			return fmt.Errorf("запись %s: %w", path, err)
		}
	}
	if err = os.Rename(name, path); err != nil {
		return fmt.Errorf("запись %s: %w", path, err)
	}
	renamed = true
	if err = syncDir(dir); err != nil {
		return fmt.Errorf("запись %s: fsync каталога: %w", path, err)
	}
	return nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return err
	}
	return d.Close()
}
