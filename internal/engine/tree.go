package engine

import (
	"errors"
	"io/fs"
	"os"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/fsx"
)

// installStage copies the extension tree src into /opt/cream/ext/.<ext>.new
// (a stale staging tree of an interrupted run is removed first).
func (e *Engine) installStage(ext, src string) error {
	staging := e.L.ExtNew(ext)
	if err := os.RemoveAll(staging); err != nil {
		return cerr.Wrap(err, cerr.ExitState, cerr.CodeIOWrite, "%s: прежний %s", ext, staging)
	}
	if err := fsx.CopyTree(src, staging); err != nil {
		return cerr.Wrap(err, cerr.ExitState, cerr.CodeIOWrite, "%s: копия дерева", ext)
	}
	return nil
}

// installDropStage removes the staging tree after a refusal.
func (e *Engine) installDropStage(ext string) {
	if err := os.RemoveAll(e.L.ExtNew(ext)); err != nil {
		e.warn("install.staging", "%s: не удалён %s: %v", ext, e.L.ExtNew(ext), err)
	}
}

// installSwap puts the staging tree in place with rename. An upgrade moves
// the current tree to .<ext>.prev, unless .prev is already there (the
// repeat of a failed upgrade: .prev holds the version still registered and
// the current tree is the failed new one, which is dropped). An install
// replaces the current tree.
func (e *Engine) installSwap(ext string, upgrade bool) error {
	tree, staging, prev := e.L.ExtTree(ext), e.L.ExtNew(ext), e.L.ExtPrev(ext)
	_, err := os.Lstat(tree)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return cerr.Wrap(err, cerr.ExitState, cerr.CodeIORead, "%s: дерево %s", ext, tree)
	case upgrade && !installExistsPath(prev):
		if err := os.Rename(tree, prev); err != nil {
			return cerr.Wrap(err, cerr.ExitState, cerr.CodeIOWrite, "%s: перенос дерева в %s", ext, prev)
		}
	default:
		if err := os.RemoveAll(tree); err != nil {
			return cerr.Wrap(err, cerr.ExitState, cerr.CodeIOWrite, "%s: удаление дерева %s", ext, tree)
		}
	}
	if err := os.Rename(staging, tree); err != nil {
		return cerr.Wrap(err, cerr.ExitState, cerr.CodeIOWrite, "%s: дерево на место", ext)
	}
	return nil
}

// installDropPrev removes .<ext>.prev after a successful install or upgrade.
func (e *Engine) installDropPrev(ext string) {
	if err := os.RemoveAll(e.L.ExtPrev(ext)); err != nil {
		e.warn("install.prev", "%s: не удалён %s: %v", ext, e.L.ExtPrev(ext), err)
	}
}

func installExistsPath(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}
