package source

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
)

// Limits of one extraction: a small archive must not unpack into a disk-full
// (gzip bomb) or millions of inodes.
const (
	maxExtractBytes   int64 = 1 << 30
	maxExtractEntries       = 100_000
)

type extractLimits struct {
	bytes   int64
	entries int
}

// ExtractTarGz unpacks a tar.gz into dst, which must not exist (its parent
// must). A hostile archive is refused as a whole and leaves nothing behind:
// absolute names, `..`, a link pointing outside the tree, an absolute link,
// a hard link outside (a hard link inside becomes a copy), devices, FIFOs,
// a repeated entry, a newline in a name, or more than 1 GiB / 100 000
// entries. Links are created after all files and directories, so no entry is
// ever written through a link. If the archive holds exactly one top-level
// directory, its contents become dst (§9.2). Files get 0755 when any execute
// bit is set, otherwise 0644; directories 0755; owners and times are not
// kept.
func ExtractTarGz(r io.Reader, dst string) error {
	return extractTarGz(r, dst, extractLimits{bytes: maxExtractBytes, entries: maxExtractEntries})
}

type pendingLink struct{ name, target string }

func extractTarGz(r io.Reader, dst string, lim extractLimits) (err error) {
	dst = filepath.Clean(dst)
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("распаковка в %s: уже существует", dst)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("распаковка в %s: %w", dst, err)
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".extract-*")
	if err != nil {
		return fmt.Errorf("распаковка в %s: %w", dst, err)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(tmp)
		}
	}()
	root, err := os.OpenRoot(tmp)
	if err != nil {
		return fmt.Errorf("распаковка в %s: %w", dst, err)
	}
	defer func() { _ = root.Close() }()
	links, tops, err := unpack(r, root, lim)
	if err != nil {
		return fmt.Errorf("распаковка архива: %w", err)
	}
	strip := ""
	if len(tops) == 1 {
		for t := range tops {
			if st, err := root.Lstat(t); err == nil && st.IsDir() {
				strip = t
			}
		}
	}
	for _, l := range links {
		final := l.name
		if strip != "" {
			final = strings.TrimPrefix(l.name, strip+"/")
		}
		if err := checkLink(final, l.target); err != nil {
			return fmt.Errorf("распаковка архива: %s: %w", l.name, err)
		}
		if err := ensureDir(root, path.Dir(l.name)); err != nil {
			return fmt.Errorf("распаковка архива: %s: %w", l.name, err)
		}
		if err := root.Symlink(l.target, l.name); err != nil {
			return fmt.Errorf("распаковка архива: %s: %w", l.name, err)
		}
	}
	if err := root.Chmod(".", 0o755); err != nil {
		return fmt.Errorf("распаковка в %s: %w", dst, err)
	}
	if strip == "" {
		if err := os.Rename(tmp, dst); err != nil {
			return fmt.Errorf("распаковка в %s: %w", dst, err)
		}
		return nil
	}
	if err := os.Rename(filepath.Join(tmp, strip), dst); err != nil {
		return fmt.Errorf("распаковка в %s: %w", dst, err)
	}
	return os.RemoveAll(tmp)
}

// unpack writes directories and regular files into root and returns the
// links to create afterwards and the set of top-level names.
func unpack(r io.Reader, root *os.Root, lim extractLimits) ([]pendingLink, map[string]bool, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = zr.Close() }()
	tr := tar.NewReader(zr)
	var (
		links   []pendingLink
		tops    = map[string]bool{}
		total   int64
		entries int
	)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return links, tops, nil
		}
		if err != nil {
			return nil, nil, err
		}
		if entries++; entries > lim.entries {
			return nil, nil, fmt.Errorf("больше %d записей", lim.entries)
		}
		if h.Typeflag == tar.TypeXGlobalHeader {
			// git archive's pax_global_header (commit id): metadata, not an entry.
			continue
		}
		name, err := cleanName(h.Name)
		if err != nil {
			return nil, nil, err
		}
		if name == "." {
			if h.Typeflag == tar.TypeDir {
				continue
			}
			return nil, nil, fmt.Errorf("%q: пустое имя", h.Name)
		}
		tops[strings.SplitN(name, "/", 2)[0]] = true
		switch h.Typeflag {
		case tar.TypeDir:
			err = ensureDir(root, name)
		case tar.TypeReg:
			if total += h.Size; total > lim.bytes {
				return nil, nil, fmt.Errorf("распакованное больше %d байт", lim.bytes)
			}
			if err = ensureDir(root, path.Dir(name)); err == nil {
				err = writeFile(root, name, tr, modeOf(h.FileInfo().Mode()))
			}
		case tar.TypeSymlink:
			links = append(links, pendingLink{name: name, target: h.Linkname})
		case tar.TypeLink:
			var n int64
			if n, err = copyHardlink(root, name, h.Linkname, lim.bytes-total); err == nil {
				total += n
			}
		default:
			err = fmt.Errorf("тип записи %q не поддерживается (устройства, FIFO и прочее)", h.Typeflag)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", name, err)
		}
	}
}

// cleanName turns an archive name into a slash-separated relative path
// without `.` components; absolute names, `..`, NUL and newlines are refused.
func cleanName(n string) (string, error) {
	if n == "" {
		return "", errors.New("пустое имя записи")
	}
	if strings.HasPrefix(n, "/") {
		return "", fmt.Errorf("%q: абсолютный путь", n)
	}
	if strings.ContainsAny(n, "\n\x00") {
		return "", fmt.Errorf("%q: перевод строки в имени", n)
	}
	var parts []string
	for _, c := range strings.Split(n, "/") {
		switch c {
		case "", ".":
		case "..":
			return "", fmt.Errorf("%q: `..` в пути", n)
		default:
			parts = append(parts, c)
		}
	}
	if len(parts) == 0 {
		return ".", nil
	}
	return strings.Join(parts, "/"), nil
}

// ensureDir creates every component of rel as a 0755 directory; an existing
// component must be a real directory, not a link or a file.
func ensureDir(root *os.Root, rel string) error {
	if rel == "." || rel == "" {
		return nil
	}
	cur := ""
	for _, c := range strings.Split(rel, "/") {
		cur = path.Join(cur, c)
		st, err := root.Lstat(cur)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			if err := root.Mkdir(cur, 0o755); err != nil {
				return err
			}
			if err := root.Chmod(cur, 0o755); err != nil {
				return err
			}
		case err != nil:
			return err
		case !st.IsDir():
			return fmt.Errorf("%s: не каталог", cur)
		}
	}
	return nil
}

func writeFile(root *os.Root, name string, r io.Reader, mode fs.FileMode) error {
	out, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, mode)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return errors.New("запись повторяется")
		}
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Chmod(mode); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// copyHardlink materializes a hard link to a regular file already extracted
// as an independent copy (the cache must not carry multi-link files, which
// fsx.CopyTree refuses on install).
// The copy is refused before anything is written when its size exceeds
// budget, the bytes left of the extraction limit.
func copyHardlink(root *os.Root, name, target string, budget int64) (int64, error) {
	t, err := cleanName(target)
	if err != nil || t == "." {
		return 0, fmt.Errorf("жёсткая ссылка на %q за пределы дерева", target)
	}
	in, err := root.OpenFile(t, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return 0, fmt.Errorf("жёсткая ссылка на %s: %w", t, err)
	}
	defer func() { _ = in.Close() }()
	st, err := in.Stat()
	if err != nil {
		return 0, err
	}
	if !st.Mode().IsRegular() {
		return 0, fmt.Errorf("жёсткая ссылка на %s: не обычный файл", t)
	}
	if st.Size() > budget {
		return 0, fmt.Errorf("распакованное больше лимита: жёсткая ссылка на %s в %d байт", t, st.Size())
	}
	if err := ensureDir(root, path.Dir(name)); err != nil {
		return 0, err
	}
	return st.Size(), writeFile(root, name, in, modeOf(st.Mode()))
}

// modeOf keeps only the execute bit: 0755 or 0644.
func modeOf(m fs.FileMode) fs.FileMode {
	if m.Perm()&0o111 != 0 {
		return 0o755
	}
	return 0o644
}

// checkLink accepts a link at rel (slash-separated, relative to the final
// tree root) to target only when the target is relative, has no newline,
// uses `..` only as leading components and does not climb above the root —
// the rule of fsx.CopyTree, so whatever is extracted can later be copied.
func checkLink(rel, target string) error {
	if target == "" {
		return errors.New("пустая цель ссылки")
	}
	if strings.ContainsAny(target, "\n\x00") {
		return fmt.Errorf("перевод строки в цели ссылки %q", target)
	}
	if path.IsAbs(target) {
		return fmt.Errorf("абсолютная ссылка на %s", target)
	}
	depth := strings.Count(rel, "/")
	up, seenName := 0, false
	for _, c := range strings.Split(target, "/") {
		switch c {
		case "", ".":
		case "..":
			if seenName {
				return fmt.Errorf("ссылка на %s: `..` после имени", target)
			}
			up++
			if up > depth {
				return fmt.Errorf("ссылка на %s ведёт за пределы дерева", target)
			}
		default:
			seenName = true
		}
	}
	return nil
}
