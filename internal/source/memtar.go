package source

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// MemTree is what ReadTarGz kept of an archive: the wanted regular files by
// their slash-separated path and every directory (explicit or implied by a
// path), after the single top-level directory is stripped as ExtractTarGz
// does.
type MemTree struct {
	Files map[string][]byte
	Dirs  map[string]bool
}

// memLimits bound one in-memory read: entries and bytes as for extraction,
// file — the size of one kept file.
type memLimits struct {
	entries     int
	bytes, file int64
}

// maxMemFile is the bound of one kept file (the bound of extension.yaml and
// cream-catalog.yaml).
const maxMemFile int64 = 1 << 20

// ReadTarGz reads a catalog archive without unpacking it to disk (cream
// check, spec 2026-10-21 §10.3): it keeps in memory only the regular files
// keep accepts and records the directories. keep sees the path in the
// archive, before the top-level directory is stripped: decide by base name. The archive is refused as a
// whole by the rules of ExtractTarGz (absolute names, `..`, links leading
// outside or absolute, devices and FIFOs, a newline in a name, more than
// 1 GiB / 100 000 entries); a kept file over 1 MiB is refused too. Links
// are checked, not followed: a link is neither a file nor a directory here.
func ReadTarGz(r io.Reader, keep func(rel string) bool) (*MemTree, error) {
	return readTarGz(r, keep, memLimits{entries: maxExtractEntries, bytes: maxExtractBytes, file: maxMemFile})
}

func readTarGz(r io.Reader, keep func(rel string) bool, lim memLimits) (*MemTree, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("чтение архива: %w", err)
	}
	defer func() { _ = zr.Close() }()
	tr := tar.NewReader(zr)
	var (
		files   = map[string][]byte{}
		dirs    = map[string]bool{}
		other   = map[string]bool{}
		sizes   = map[string]int64{}
		links   []pendingLink
		tops    = map[string]bool{}
		total   int64
		entries int
	)
	addDirs := func(p string) error {
		for ; p != "." && p != ""; p = path.Dir(p) {
			if other[p] {
				return fmt.Errorf("чтение архива: %s: не каталог, а под ним есть записи", p)
			}
			dirs[p] = true
		}
		return nil
	}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("чтение архива: %w", err)
		}
		if entries++; entries > lim.entries {
			return nil, fmt.Errorf("чтение архива: больше %d записей", lim.entries)
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		name, err := cleanName(h.Name)
		if err != nil {
			return nil, fmt.Errorf("чтение архива: %w", err)
		}
		if name == "." {
			if h.Typeflag == tar.TypeDir {
				continue
			}
			return nil, fmt.Errorf("чтение архива: %q: пустое имя", h.Name)
		}
		if other[name] || (h.Typeflag != tar.TypeDir && dirs[name]) {
			return nil, fmt.Errorf("чтение архива: %s: запись повторяется", name)
		}
		tops[strings.SplitN(name, "/", 2)[0]] = true
		switch h.Typeflag {
		case tar.TypeDir:
			if err := addDirs(name); err != nil {
				return nil, err
			}
			continue
		case tar.TypeReg:
			if total += h.Size; total > lim.bytes {
				return nil, fmt.Errorf("чтение архива: распакованное больше %d байт", lim.bytes)
			}
			if err := addDirs(path.Dir(name)); err != nil {
				return nil, err
			}
			other[name], sizes[name] = true, h.Size
			if !keep(name) {
				continue
			}
			if h.Size > lim.file {
				return nil, fmt.Errorf("чтение архива: %s: больше %d байт", name, lim.file)
			}
			data, err := io.ReadAll(io.LimitReader(tr, lim.file+1))
			if err != nil {
				return nil, fmt.Errorf("чтение архива: %s: %w", name, err)
			}
			files[name] = data
		case tar.TypeSymlink:
			if err := addDirs(path.Dir(name)); err != nil {
				return nil, err
			}
			other[name] = true
			links = append(links, pendingLink{name: name, target: h.Linkname})
		case tar.TypeLink:
			target, err := cleanName(h.Linkname)
			if err != nil {
				return nil, fmt.Errorf("чтение архива: %s: %w", name, err)
			}
			size, regular := sizes[target]
			if !regular {
				return nil, fmt.Errorf("чтение архива: %s: жёсткая ссылка на %s — не на прочитанный обычный файл",
					name, h.Linkname)
			}
			if total += size; total > lim.bytes {
				return nil, fmt.Errorf("чтение архива: распакованное больше %d байт", lim.bytes)
			}
			if err := addDirs(path.Dir(name)); err != nil {
				return nil, err
			}
			other[name], sizes[name] = true, size
			if body, ok := files[target]; ok {
				files[name] = body
			} else if keep(name) {
				return nil, fmt.Errorf("чтение архива: %s: жёсткая ссылка на %s, который не прочитан", name, h.Linkname)
			}
		default:
			return nil, fmt.Errorf("чтение архива: %s: тип записи %q не поддерживается (устройства, FIFO и прочее)",
				name, h.Typeflag)
		}
	}
	strip := ""
	if len(tops) == 1 {
		for t := range tops {
			if dirs[t] {
				strip = t
			}
		}
	}
	for _, l := range links {
		if err := checkLink(trimTop(l.name, strip), l.target); err != nil {
			return nil, fmt.Errorf("чтение архива: %s: %w", l.name, err)
		}
	}
	out := &MemTree{Files: make(map[string][]byte, len(files)), Dirs: make(map[string]bool, len(dirs))}
	for name, body := range files {
		if strip == "" || strings.HasPrefix(name, strip+"/") {
			out.Files[trimTop(name, strip)] = body
		}
	}
	for d := range dirs {
		if d != strip {
			out.Dirs[trimTop(d, strip)] = true
		}
	}
	return out, nil
}

func trimTop(name, strip string) string {
	if strip == "" {
		return name
	}
	return strings.TrimPrefix(name, strip+"/")
}
