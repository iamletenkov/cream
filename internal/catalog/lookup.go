package catalog

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/semver"
	"github.com/iamletenkov/cream/internal/source"
)

// Lookup finds ref in the source caches dirs (in priority order, §9.3),
// offline. Without ref.Source the first source that has the extension
// wins; the same name in a later source is reported as source.shadowed.
// A source that is not synchronized is skipped with a source.not_synced
// warning when it precedes the winner (it could have shadowed it); when the
// extension is found nowhere and some source is not synchronized, that is
// 69 source.not_synced rather than 66 not_found.extension. ref.Version is
// an assertion: a different version of the winner is 66 version.mismatch.
//
// The found extension is checked like LoadExtension (manifest against
// cream, tree rules of §4.1) and its digest computed.
func Lookup(dirs []source.SourceDir, ref Ref, cream manifest.CreamBuild, w *cerr.Warnings) (Entry, error) {
	if w == nil {
		w = &cerr.Warnings{}
	}
	if ref.Source != "" {
		for _, d := range dirs {
			if d.Source.Name == ref.Source {
				return lookupIn([]source.SourceDir{d}, ref, cream, w)
			}
		}
		e := cerr.New(cerr.ExitNotFound, "not_found.source", "источника %s нет в sources.yaml", ref.Source)
		e.Details = map[string]any{"source": ref.Source}
		return Entry{}, e
	}
	return lookupIn(dirs, ref, cream, w)
}

func lookupIn(dirs []source.SourceDir, ref Ref, cream manifest.CreamBuild, w *cerr.Warnings) (Entry, error) {
	var unsynced []string
	winner := -1
	var found string
	for i, d := range dirs {
		if !d.Synced {
			unsynced = append(unsynced, d.Source.Name)
			continue
		}
		dir, err := findIn(d.Dir, ref.Name)
		if err != nil {
			return Entry{}, err
		}
		if dir != "" {
			winner, found = i, dir
			break
		}
	}
	if winner < 0 {
		if len(unsynced) > 0 {
			e := cerr.New(cerr.ExitSource, "source.not_synced",
				"%s не найдено, а источники %s не синхронизированы: cream source sync", ref.Name, strings.Join(unsynced, ", "))
			e.Details = map[string]any{"ext": ref.Name, "sources": unsynced}
			return Entry{}, e
		}
		e := cerr.New(cerr.ExitNotFound, "not_found.extension", "расширения %s нет в источниках", ref.Name)
		e.Details = map[string]any{"ext": ref.Name}
		return Entry{}, e
	}
	win := dirs[winner]
	for _, name := range unsynced {
		w.Add("source.not_synced", "%s: источник %s не синхронизирован и пропущен; взято из %s", ref.Name, name, win.Source.Name)
	}
	for _, d := range dirs[winner+1:] {
		if !d.Synced {
			continue
		}
		if dir, err := findIn(d.Dir, ref.Name); err == nil && dir != "" {
			w.Add("source.shadowed", "%s из источника %s затеняет %s из %s", ref.Name, win.Source.Name, ref.Name, d.Source.Name)
		}
	}
	entry, err := loadExtension(found, cream, w)
	if err != nil {
		return Entry{}, err
	}
	entry.Source = win
	if ref.Version != nil && semver.Compare(*ref.Version, entry.Manifest.Version) != 0 {
		e := cerr.New(cerr.ExitNotFound, "version.mismatch", "%s: в источнике %s версия %s, а не %s",
			ref.Name, win.Source.Name, entry.Manifest.Version, ref.Version)
		e.Details = map[string]any{"ext": ref.Name, "source": win.Source.Name,
			"found": entry.Manifest.Version.String(), "want": ref.Version.String()}
		return Entry{}, e
	}
	return entry, nil
}

// findIn returns the directory of extension name under the roots of the
// catalog cached at dir, "" when no root has it. A directory with the
// extension's name is the extension: a defect in it is an error, not a
// reason to look further.
func findIn(dir, name string) (string, error) {
	cat, err := manifest.LoadCatalog(dir)
	if err != nil {
		return "", err
	}
	for _, root := range cat.Roots {
		if checkRootDir(dir, root) != "" {
			continue
		}
		p := filepath.Join(dir, filepath.FromSlash(root), name)
		if st, err := os.Lstat(p); err == nil && st.IsDir() {
			return p, nil
		}
	}
	return "", nil
}
