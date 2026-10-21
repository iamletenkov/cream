package catalog

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/fsx"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/source"
)

// Коды ошибок дерева и каталога (§4.1); все — 65.
const (
	codeDirName        = "manifest.dir_name"
	codeHookMissing    = "manifest.hook_missing"
	codeHookUndeclared = "manifest.hook_undeclared"
	codeActionMissing  = "manifest.action_missing"
	codeFileMissing    = "manifest.file_missing"
	codeNotExecutable  = "manifest.not_executable"
	codeBadExecutable  = "manifest.bad_executable"
	codeLinkOutside    = "manifest.link_outside"
	codeSpecialFile    = "manifest.special_file"
	codeDuplicateName  = "catalog.duplicate_name"
	codeCatalogInvalid = "catalog.invalid"
	codeCreamVersion   = "platform.cream_version" // 68 manifest.Load: нужен cream новее
)

// Коды предупреждений валидатора.
const (
	warnLockNoneSet   = "validate.lock_none_set"  // §6.3: cream set в действии lock: none
	warnUnitLock      = "validate.unit_lock"      // §7.3: Exec* юнита зовёт действие под блокировкой
	warnApplyBlocking = "validate.apply_blocking" // §8.4: systemctl без --no-block в apply
	warnCreamNewer    = "catalog.cream_newer"     // Р22: расширению нужен cream новее текущего
	warnOpenWrtTmpfs  = "validate.openwrt_tmpfs"  // ADR-0005: /var на OpenWrt — tmpfs
)

// maxScript — сколько байт исполняемого файла читается для проверки UTF-8 и
// поиска вызовов; скрипт длиннее — ошибка, ELF проверяется по заголовку.
const maxScript = 4 << 20

// Entry is an extension found in a source cache (Lookup) or loaded from a
// directory (LoadExtension, Source is zero then). Digest is the tree digest
// of Dir (§9.5).
type Entry struct {
	Source   source.SourceDir
	Dir      string
	Manifest *manifest.Manifest
	Digest   string
}

// loaded is a valid extension of the report: its directory and manifest.
type loaded struct {
	rel string
	dir string
	m   *manifest.Manifest
}

// Validate checks the catalog at root (cream-catalog.yaml, every extension
// under its roots) against §4.1 and collects the warnings of §6.3, §7.3 and
// §8.4. It does not stop at the first error: the report lists them all, and
// the returned error is Report.Err() (nil when the catalog is valid).
// Directories starting with "." and plain files under a root are skipped.
// An extension that needs a newer cream is not an error of the catalog
// (Р22): it goes to NotApplicable with a catalog.cream_newer warning, so a
// catalog published for a newer cream still syncs; installing it stays 68.
func Validate(root string, cream manifest.CreamBuild) (Report, error) {
	r, _ := validate(root, cream)
	return r, r.Err()
}

// validate is Validate returning also the valid extensions (for the graph).
func validate(root string, cream manifest.CreamBuild) (Report, []loaded) {
	r := newReport()
	cat, err := manifest.LoadCatalog(root)
	if err != nil {
		r.failErr("", manifest.CatalogFile, err)
		return r, nil
	}
	r.Catalog, r.CatalogVersion = cat.Name, cat.Version.String()
	var exts []loaded
	seen := map[string]string{}
	for _, rootRel := range cat.Roots {
		if msg := checkRootDir(root, rootRel); msg != "" {
			r.fail("", rootRel, codeCatalogInvalid, "каталог: roots %s: %s", rootRel, msg)
			continue
		}
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(rootRel)))
		if err != nil {
			r.failErr("", rootRel, err)
			continue
		}
		for _, de := range entries {
			name := de.Name()
			rel := path.Join(rootRel, name)
			switch t := de.Type(); {
			case strings.HasPrefix(name, "."), t.IsRegular():
				continue
			case t&fs.ModeSymlink != 0:
				r.fail(name, rel, codeCatalogInvalid, "%s: ссылка вместо каталога расширения", rel)
				continue
			case !t.IsDir():
				r.fail(name, rel, codeCatalogInvalid, "%s: не каталог и не файл", rel)
				continue
			}
			m := checkExtension(&r, filepath.Join(root, filepath.FromSlash(rel)), rel, cream, true)
			if m == nil {
				continue
			}
			if prev, dup := seen[m.Name]; dup {
				r.fail(m.Name, rel, codeDuplicateName,
					"%s: имя %s уже занято %s (имя уникально во всём каталоге)", rel, m.Name, prev)
				continue
			}
			seen[m.Name] = rel
			exts = append(exts, loaded{rel: rel, dir: filepath.Join(root, filepath.FromSlash(rel)), m: m})
		}
	}
	idx := map[string]loaded{}
	for _, x := range exts {
		idx[x.m.Name] = x
		r.Extensions = append(r.Extensions, ExtensionInfo{Name: x.m.Name, Version: x.m.Version.String(), Path: x.rel})
	}
	a := newUnitLockAnalysis(defaultUnitLockLimits)
	for _, x := range exts {
		warnExtension(&r, x, idx, a)
	}
	return r, exts
}

// ValidateDir checks one extension directory (cream validate DIR…): the same
// rules and warnings as Validate, paths relative to dir. A call of another
// extension's action from a unit is not checked: its manifest is unknown.
func ValidateDir(dir string, cream manifest.CreamBuild) (Report, error) {
	return validateDir(dir, cream, defaultUnitLockLimits)
}

// validateDir is ValidateDir with the limits of the validate.unit_lock
// analysis (tests lower them).
func validateDir(dir string, cream manifest.CreamBuild, lim unitLockLimits) (Report, error) {
	r := newReport()
	m := checkExtension(&r, dir, ".", cream, false)
	if m != nil {
		r.Extensions = append(r.Extensions, ExtensionInfo{Name: m.Name, Version: m.Version.String(), Path: "."})
		x := loaded{rel: ".", dir: dir, m: m}
		warnExtension(&r, x, map[string]loaded{m.Name: x}, newUnitLockAnalysis(lim))
	}
	return r, r.Err()
}

// LoadExtension loads and checks the extension tree at dir for
// install --from-dir: the manifest, the tree rules of §4.1 and the digest.
// Validator warnings are not reported here (they are for catalog authors);
// the first error is returned.
func LoadExtension(dir string, cream manifest.CreamBuild) (Entry, error) {
	return loadExtension(dir, cream, nil)
}

func loadExtension(dir string, cream manifest.CreamBuild, w *cerr.Warnings) (Entry, error) {
	r := newReport()
	m := checkExtension(&r, dir, ".", cream, false)
	for _, x := range r.Warnings {
		if w != nil {
			w.Add(x.Code, "%s", x.Message)
		}
	}
	if err := r.Err(); err != nil {
		return Entry{}, err
	}
	digest, err := fsx.TreeDigest(dir)
	if err != nil {
		return Entry{}, cerr.Wrap(err, cerr.ExitState, cerr.CodeIORead, "расширение %s", m.Name)
	}
	return Entry{Dir: dir, Manifest: m, Digest: digest}, nil
}

// checkRootDir checks that every component of the catalog root rel is a
// real directory, not a link: a link could lead the reader outside.
func checkRootDir(catalog, rel string) string {
	cur := catalog
	for _, c := range strings.Split(rel, "/") {
		cur = filepath.Join(cur, c)
		st, err := os.Lstat(cur)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return "каталога нет"
		case err != nil:
			return err.Error()
		case st.Mode()&fs.ModeSymlink != 0:
			return "ссылка вместо каталога"
		case !st.IsDir():
			return "не каталог"
		}
	}
	return ""
}

// checkExtension loads the manifest of the extension at dir and checks its
// tree; problems go to r with paths under rel. It returns the manifest when
// the extension has no errors, nil otherwise. In a catalog (inCatalog) a
// manifest that needs a newer cream is not an error: it goes to
// NotApplicable with a catalog.cream_newer warning (Р22).
func checkExtension(r *Report, dir, rel string, cream manifest.CreamBuild, inCatalog bool) *manifest.Manifest {
	base := filepath.Base(filepath.Clean(dir))
	var w cerr.Warnings
	m, err := manifest.Load(dir, cream, &w)
	for _, x := range w.List() {
		r.warn(x.Code, "%s: %s", base, x.Message)
	}
	if err != nil {
		if inCatalog && cerr.CodeOf(err) == codeCreamVersion {
			r.creamNewer(base, path.Join(rel, manifest.ManifestFile), err, cream)
			return nil
		}
		r.failErr(base, path.Join(rel, manifest.ManifestFile), err)
		return nil
	}
	before := len(r.Errors)
	t := tree{r: r, dir: dir, rel: rel, ext: m.Name}
	if base != m.Name {
		t.fail(".", codeDirName, "имя каталога %s не равно name %s", base, m.Name)
	}
	t.walk()
	t.checkHooks(m)
	t.checkActions(m)
	t.checkFiles(m)
	if len(r.Errors) != before {
		return nil
	}
	return m
}

// tree checks one extension tree.
type tree struct {
	r        *Report
	dir, rel string
	ext      string
}

func (t *tree) fail(p, code, format string, a ...any) {
	full := path.Join(t.rel, p)
	t.r.fail(t.ext, full, code, "%s: %s", full, fmt.Sprintf(format, a...))
}

func (t *tree) path(p string) string { return filepath.Join(t.dir, filepath.FromSlash(p)) }

// walk checks every entry: links only inside the tree, no special files,
// every executable regular file is a UTF-8 script with a shebang or ELF.
func (t *tree) walk() {
	err := filepath.WalkDir(t.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(t.dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		switch typ := d.Type(); {
		case typ.IsDir():
		case typ&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			if msg := checkLink(rel, target); msg != "" {
				t.fail(rel, codeLinkOutside, "ссылка на %s: %s", target, msg)
			}
		case typ.IsRegular():
			info, err := d.Info()
			if err != nil {
				return err
			}
			if info.Mode().Perm()&0o111 != 0 {
				if msg := checkExecutable(p); msg != "" {
					t.fail(rel, codeBadExecutable, "%s (исполняемый файл — UTF-8-скрипт с #! или ELF)", msg)
				}
			}
		default:
			t.fail(rel, codeSpecialFile, "специальный файл (%v) в дереве расширения", typ)
		}
		return nil
	})
	if err != nil {
		t.r.failErr(t.ext, t.rel, err)
	}
}

// checkLink is the rule of fsx.CopyTree: a relative target without newline,
// ".." only as leading components and never above the tree root (a ".."
// after a name could climb through a link).
func checkLink(rel, target string) string {
	switch {
	case target == "":
		return "пустая цель"
	case strings.ContainsRune(target, '\n'):
		return "перевод строки в цели"
	case path.IsAbs(target):
		return "абсолютная ссылка ведёт за пределы дерева"
	}
	depth := strings.Count(rel, "/")
	up, seenName := 0, false
	for _, c := range strings.Split(target, "/") {
		switch c {
		case "", ".":
		case "..":
			if seenName {
				return "`..` после имени"
			}
			up++
			if up > depth {
				return "ведёт за пределы дерева"
			}
		default:
			seenName = true
		}
	}
	return ""
}

// checkExecutable returns "" for an ELF file or a valid UTF-8 script that
// starts with "#!", otherwise what is wrong.
func checkExecutable(p string) string {
	data, err := readHead(p, maxScript+1)
	if err != nil {
		return err.Error()
	}
	switch {
	case bytes.HasPrefix(data, []byte("\x7fELF")):
		return ""
	case !bytes.HasPrefix(data, []byte("#!")):
		return "нет #! и не ELF"
	case len(data) > maxScript:
		return fmt.Sprintf("скрипт длиннее %d байт", maxScript)
	case !utf8.Valid(data):
		return "скрипт не в UTF-8"
	}
	return ""
}

func readHead(p string, limit int64) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(io.LimitReader(f, limit))
}

// executable stats p following a link (links are checked by walk) and
// reports whether it is a regular file with an execute bit.
func (t *tree) executable(p string) (exists, ok bool) {
	st, err := os.Stat(t.path(p))
	if err != nil {
		return false, false
	}
	return true, st.Mode().IsRegular() && st.Mode().Perm()&0o111 != 0
}

// checkHooks: a declared hook exists as an executable file, a file in hooks/
// without a declaration is an error (a library has no hooks at all).
func (t *tree) checkHooks(m *manifest.Manifest) {
	declared := make([]string, 0, len(m.Hooks))
	for h := range m.Hooks {
		declared = append(declared, string(h))
	}
	slices.Sort(declared)
	for _, h := range declared {
		p := "hooks/" + h
		switch exists, ok := t.executable(p); {
		case !exists:
			t.fail(p, codeHookMissing, "хук %s объявлен, а файла нет", h)
		case !ok:
			t.fail(p, codeNotExecutable, "хук без бита исполнения или не файл")
		}
	}
	entries, err := os.ReadDir(t.path("hooks"))
	if err != nil {
		return // нет каталога: нехватку объявленных хуков уже сообщили
	}
	for _, e := range entries {
		if !slices.Contains(declared, e.Name()) {
			t.fail("hooks/"+e.Name(), codeHookUndeclared, "файл хука без объявления в hooks манифеста")
		}
	}
}

// checkActions: a declared action exists as an executable file; any other
// file in actions/ also needs the execute bit.
func (t *tree) checkActions(m *manifest.Manifest) {
	names := make([]string, 0, len(m.Actions))
	for a := range m.Actions {
		names = append(names, a)
	}
	slices.Sort(names)
	for _, a := range names {
		p := "actions/" + a
		switch exists, ok := t.executable(p); {
		case !exists:
			t.fail(p, codeActionMissing, "действие %s объявлено, а файла нет", a)
		case !ok:
			t.fail(p, codeNotExecutable, "действие без бита исполнения или не файл")
		}
	}
	entries, err := os.ReadDir(t.path("actions"))
	if err != nil {
		return
	}
	for _, e := range entries {
		if slices.Contains(names, e.Name()) {
			continue
		}
		p := "actions/" + e.Name()
		if _, ok := t.executable(p); !ok {
			t.fail(p, codeNotExecutable, "файл в actions/ без бита исполнения или не файл")
		}
	}
}

// checkFiles: every files[].src is a file of the tree.
func (t *tree) checkFiles(m *manifest.Manifest) {
	for _, f := range m.Files {
		st, err := os.Stat(t.path(f.Src))
		if err != nil || !st.Mode().IsRegular() {
			t.fail(f.Src, codeFileMissing, "files.src %s: нет такого файла в дереве", f.Src)
		}
	}
}
