// Package config is the settings domain of one extension (spec §8):
// /etc/cream/<ext>/config (0644) with ordinary keys and
// /etc/cream/<ext>/secrets (0600) with keys `secret: file`, both in the
// systemd EnvironmentFile grammar. Every change is validated against the
// schema (config.keys) before anything is written.
package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"syscall"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/envfile"
	"github.com/iamletenkov/cream/internal/fsx"
	"github.com/iamletenkov/cream/internal/keytype"
	"github.com/iamletenkov/cream/internal/paths"
)

// Origins of a value in Show.
const (
	OriginDefault = "default"
	OriginFile    = "file"
)

// Masks of a secret value in Show.
const (
	MaskUnknown = "***"     // the secrets file could not be read (not root)
	MaskSet     = "(задан)" // non-empty
	MaskEmpty   = "(пусто)" // empty or absent
)

// maxFile bounds what is read from a domain or params file.
const maxFile = 1 << 20

// Domain is the settings of one extension.
type Domain struct {
	l      paths.Layout
	ext    string
	schema keytype.Schema
	// trusted is l.NeedsRoot(): files are opened through openTrusted and
	// EtcDir is checked before writing (§13, rulings Р9, Р17).
	trusted bool
	// uid owns trusted files and EtcDir: root; tests put their own uid.
	uid         int
	openTrusted func(path string, uid int, mode fs.FileMode) (*os.File, error)
}

// Open returns the domain of ext under layout l with schema s. Nothing is
// read until a method is called.
func Open(l paths.Layout, ext string, s keytype.Schema) *Domain {
	return &Domain{l: l, ext: ext, schema: s, trusted: l.NeedsRoot(), openTrusted: fsx.OpenTrusted}
}

// Entry is one row of Show. For a secret, Value is a mask.
type Entry struct {
	Key, Value, Origin, Type string
	Secret, Invalid          bool
}

// Change is one assignment KEY=VALUE.
type Change struct{ Key, Value string }

// SetOpts relaxes the checks of Set.
type SetOpts struct {
	// AllowReadonly lets readonly keys be written (hooks, actions, --allow-readonly).
	AllowReadonly bool
	// AllowInstallScope lets `scope: install` keys be written (cream install).
	AllowInstallScope bool
	// Only, when not nil, limits writing to these keys; all changes are still validated.
	Only []string
}

// lookup returns the spec of a key name that may be stored in the domain.
func (d *Domain) lookup(key string) (keytype.Spec, bool) {
	if !envfile.ValidName(key) {
		return keytype.Spec{}, false
	}
	return d.schema.Lookup(key)
}

func isSecret(sp keytype.Spec) bool { return sp.Secret != keytype.SecretNone }

func errDetails(e *cerr.Error, key string) *cerr.Error {
	e.Details = map[string]any{"key": key}
	return e
}

// validate checks one change against the schema and the options.
func (d *Domain) validate(ch Change, o SetOpts) (keytype.Spec, error) {
	sp, ok := d.lookup(ch.Key)
	if !ok {
		return sp, errDetails(cerr.New(cerr.ExitData, "config.unknown_key",
			"%s: ключа %s нет в config.keys", d.ext, ch.Key), ch.Key)
	}
	if isBuild(sp) {
		return sp, errScopeBuild(d.ext, ch.Key)
	}
	if sp.Secret == keytype.SecretHook {
		return sp, errDetails(cerr.New(cerr.ExitData, "config.reserved",
			"%s: %s: secret: hook зарезервирован в cream/v1, не реализован в cream 1.0", d.ext, ch.Key), ch.Key)
	}
	if sp.Scope == keytype.ScopeInstall && !o.AllowInstallScope {
		return sp, errDetails(cerr.New(cerr.ExitData, "config.install_scope",
			"%s: %s — параметр установки; задайте его так: cream install %s %s=…", d.ext, ch.Key, d.ext, ch.Key), ch.Key)
	}
	if sp.Readonly && !o.AllowReadonly {
		return sp, errDetails(cerr.New(cerr.ExitData, "config.readonly",
			"%s: %s только для чтения (нужен --allow-readonly)", d.ext, ch.Key), ch.Key)
	}
	err := sp.CheckValue(ch.Value)
	if err == nil {
		err = envfile.CheckValue(ch.Value)
	}
	if err != nil {
		if isSecret(sp) {
			// The message of a type check may quote the value.
			err = errors.New("значение секрета не прошло проверку типа " + sp.Type.Raw)
		}
		return sp, errDetails(cerr.Wrap(err, cerr.ExitData, "config.invalid", "%s: %s", d.ext, ch.Key), ch.Key)
	}
	return sp, nil
}

// Set validates every change, then writes the domain: secret keys to
// secrets (0600), the others to config (0644). Nothing is written if any
// change is refused. changed lists, in input order, the keys whose stored
// value differs from before (absent counts as different); with nothing
// changed no file is touched.
func (d *Domain) Set(ch []Change, o SetOpts) (changed []string, err error) {
	specs := make([]keytype.Spec, len(ch))
	seen := make(map[string]bool, len(ch))
	for i, c := range ch {
		if seen[c.Key] {
			return nil, errDetails(cerr.New(cerr.ExitUsage, "usage.duplicate_key",
				"%s: ключ %s задан дважды", d.ext, c.Key), c.Key)
		}
		seen[c.Key] = true
		if specs[i], err = d.validate(c, o); err != nil {
			return nil, err
		}
	}
	write := func(key string) bool { return o.Only == nil || slices.Contains(o.Only, key) }

	cfg, err := d.readConfig()
	if err != nil {
		return nil, err
	}
	var sec *envfile.File
	for i, c := range ch {
		if write(c.Key) && isSecret(specs[i]) {
			var readable bool
			if sec, readable, err = d.readSecrets(); err != nil {
				return nil, err
			}
			if !readable {
				// Rewriting secrets without reading them would drop the other keys.
				return nil, cerr.New(cerr.ExitPerm, "perm.root", "%s: секреты меняет только root", d.ext)
			}
			break
		}
	}
	cfgDirty, secDirty := false, false
	for i, c := range ch {
		if !write(c.Key) {
			continue
		}
		target, other := cfg, sec
		if isSecret(specs[i]) {
			target, other = sec, cfg
		}
		if old, ok := target.Get(c.Key); !ok || old != c.Value {
			target.Set(c.Key, c.Value) // name and value validated above
			changed = append(changed, c.Key)
			if target == sec {
				secDirty = true
			} else {
				cfgDirty = true
			}
		}
		if other != nil {
			if _, ok := other.Get(c.Key); ok {
				other.Delete(c.Key) // a key in the wrong file of the domain
				if other == sec {
					secDirty = true
				} else {
					cfgDirty = true
				}
			}
		}
	}
	if !cfgDirty && !secDirty {
		return nil, nil
	}
	if err := os.MkdirAll(d.l.EtcDir(d.ext), 0o755); err != nil {
		return nil, cerr.Wrap(err, cerr.ExitState, cerr.CodeIOWrite, "%s: каталог настроек", d.ext)
	}
	if err := d.checkEtcDir(); err != nil {
		return nil, err
	}
	if secDirty {
		if err := fsx.WriteAtomic(d.l.SecretsFile(d.ext), sec.Bytes(), 0o600); err != nil {
			return nil, cerr.Wrap(err, cerr.ExitState, cerr.CodeIOWrite, "%s: запись секретов", d.ext)
		}
	}
	if cfgDirty {
		if err := fsx.WriteAtomic(d.l.ConfigFile(d.ext), cfg.Bytes(), 0o644); err != nil {
			return nil, cerr.Wrap(err, cerr.ExitState, cerr.CodeIOWrite, "%s: запись настроек", d.ext)
		}
	}
	return changed, nil
}

// Show lists every key of the schema and every key found in the files,
// sorted by name. Pattern templates themselves are not rows. A file value
// that fails its type, or a key unknown to the schema, is Invalid.
func (d *Domain) Show() ([]Entry, error) {
	cfg, err := d.readConfig()
	if err != nil {
		return nil, err
	}
	sec, readable, err := d.readSecrets()
	if err != nil {
		return nil, err
	}
	keys := d.allKeys(cfg, sec)
	out := make([]Entry, 0, len(keys))
	for _, k := range keys {
		sp, known := d.lookup(k)
		if known && isBuild(sp) {
			continue // never stored: nothing to show (§10.9)
		}
		e := Entry{Key: k, Origin: OriginDefault, Type: sp.Type.Raw, Secret: known && isSecret(sp)}
		v, inFile := cfg.Get(k)
		if sv, ok := sec.Get(k); ok {
			v, inFile, e.Secret = sv, true, true
		}
		switch {
		case inFile:
			e.Origin = OriginFile
			e.Invalid = !known || sp.CheckValue(v) != nil || envfile.CheckValue(v) != nil
		default:
			v = sp.Default
		}
		e.Value = v
		if e.Secret {
			switch {
			case !inFile && !readable && d.secretsExist():
				e.Value, e.Origin = MaskUnknown, OriginFile
			case v != "":
				e.Value = MaskSet
			default:
				e.Value = MaskEmpty
			}
		}
		out = append(out, e)
	}
	return out, nil
}

// allKeys returns the non-pattern schema keys and the keys of both files, sorted.
func (d *Domain) allKeys(files ...*envfile.File) []string {
	set := map[string]bool{}
	for k, sp := range d.schema {
		if !sp.Pattern {
			set[k] = true
		}
	}
	for _, f := range files {
		for _, k := range f.Keys() {
			set[k] = true
		}
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// Get returns the effective value of key (file, else default). A secret key
// needs secret == true; reading it needs access to the secrets file.
func (d *Domain) Get(key string, secret bool) (string, error) {
	sp, ok := d.lookup(key)
	if !ok {
		return "", errDetails(cerr.New(cerr.ExitNotFound, "not_found.key",
			"%s: ключа %s нет в config.keys", d.ext, key), key)
	}
	if isBuild(sp) {
		return "", errScopeBuild(d.ext, key)
	}
	cfg, err := d.readConfig()
	if err != nil {
		return "", err
	}
	if isSecret(sp) {
		if !secret {
			return "", errDetails(cerr.New(cerr.ExitUsage, "usage.secret",
				"%s: %s — секрет; значение печатается только с --secret", d.ext, key), key)
		}
		sec, readable, err := d.readSecrets()
		if err != nil {
			return "", err
		}
		if !readable && d.secretsExist() {
			return "", cerr.New(cerr.ExitPerm, "perm.root", "%s: секреты читает только root", d.ext)
		}
		if v, ok := sec.Get(key); ok {
			return v, nil
		}
	}
	if v, ok := cfg.Get(key); ok {
		return v, nil
	}
	return sp.Default, nil
}

// Values returns the effective non-secret values: the default of every
// non-pattern schema key, overridden by config; config keys matching a
// pattern are included, keys unknown to the schema and `scope: build` keys
// (never stored, §10.9) are not.
func (d *Domain) Values() (map[string]string, error) {
	cfg, err := d.readConfig()
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for k, sp := range d.schema {
		if !sp.Pattern && !isSecret(sp) && !isBuild(sp) && envfile.ValidName(k) {
			out[k] = sp.Default
		}
	}
	for _, k := range cfg.Keys() {
		if sp, ok := d.lookup(k); ok && !isSecret(sp) && !isBuild(sp) {
			out[k], _ = cfg.Get(k)
		}
	}
	return out, nil
}

// Secrets returns the non-empty values of secret keys, sorted and without
// duplicates, for the output filter: everything in secrets plus secret keys
// misplaced in config.
func (d *Domain) Secrets() ([]string, error) {
	cfg, err := d.readConfig()
	if err != nil {
		return nil, err
	}
	sec, readable, err := d.readSecrets()
	if err != nil {
		return nil, err
	}
	if !readable && d.secretsExist() {
		return nil, cerr.New(cerr.ExitPerm, "perm.root", "%s: секреты читает только root", d.ext)
	}
	var out []string
	for _, k := range sec.Keys() {
		if v, _ := sec.Get(k); v != "" {
			out = append(out, v)
		}
	}
	for _, k := range cfg.Keys() {
		if sp, ok := d.lookup(k); ok && isSecret(sp) {
			if v, _ := cfg.Get(k); v != "" {
				out = append(out, v)
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// readConfig parses config; a missing file is an empty domain.
func (d *Domain) readConfig() (*envfile.File, error) {
	path := d.l.ConfigFile(d.ext)
	f, err := d.open(path, 0o644)
	if errors.Is(err, fs.ErrNotExist) {
		return envfile.Parse(nil)
	}
	if err != nil {
		return nil, d.fileErr(err, "config", path)
	}
	defer func() { _ = f.Close() }()
	return d.parse(f, "config", path)
}

// readSecrets parses secrets through a checked descriptor (ruling Р17): on
// the real root via fsx.OpenTrusted (root-owned path, 0600 at most); under
// another --root without owner checks (ruling Р9), still without following
// links. A missing file is an empty domain; a file the caller may not read
// gives readable == false and an empty domain.
func (d *Domain) readSecrets() (f *envfile.File, readable bool, err error) {
	path := d.l.SecretsFile(d.ext)
	fd, err := d.open(path, 0o600)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		empty, _ := envfile.Parse(nil)
		return empty, true, nil
	case errors.Is(err, fs.ErrPermission):
		empty, _ := envfile.Parse(nil)
		return empty, false, nil
	case err != nil:
		return nil, false, d.fileErr(err, "secrets", path)
	}
	defer func() { _ = fd.Close() }()
	f, err = d.parse(fd, "secrets", path)
	return f, err == nil, err
}

func (d *Domain) secretsExist() bool {
	_, err := os.Lstat(d.l.SecretsFile(d.ext))
	return err == nil
}

func (d *Domain) parse(r io.Reader, what, path string) (*envfile.File, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxFile+1))
	if err != nil {
		return nil, d.fileErr(err, what, path)
	}
	if len(data) > maxFile {
		return nil, cerr.New(cerr.ExitData, "config.invalid", "%s: %s больше %d байт", d.ext, path, maxFile)
	}
	f, err := envfile.Parse(data)
	if err != nil {
		return nil, cerr.Wrap(err, cerr.ExitData, "config.invalid", "%s: разбор %s", d.ext, path)
	}
	return f, nil
}

func (d *Domain) fileErr(err error, what, path string) error {
	if errors.Is(err, fsx.ErrUnsafePerm) {
		return cerr.Wrap(err, cerr.ExitPerm, "perm."+what, "%s: %s", d.ext, path)
	}
	return cerr.Wrap(err, cerr.ExitState, cerr.CodeIORead, "%s: чтение %s", d.ext, path)
}

// open opens a domain file: on the real root through openTrusted (owner
// uid, at most mode, checked path — ruling Р17); under another --root
// without owner checks (ruling Р9), still without following links.
func (d *Domain) open(path string, mode fs.FileMode) (*os.File, error) {
	if d.trusted {
		return d.openTrusted(path, d.uid, mode)
	}
	return openPlain(path)
}

// checkEtcDir requires, on the real root, the settings directory of the
// extension to be a directory (not a link) owned by uid and not writable by
// group or others (§13): otherwise someone else could swap the files.
func (d *Domain) checkEtcDir() error {
	if !d.trusted {
		return nil
	}
	dir := d.l.EtcDir(d.ext)
	bad := func(format string, a ...any) error {
		e := cerr.New(cerr.ExitPerm, "perm.etc_dir", "%s: каталог настроек %s: "+format, append([]any{d.ext, dir}, a...)...)
		e.Details = map[string]any{"path": dir}
		return e
	}
	fd, err := syscall.Open(dir, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.ENOTDIR) {
		return bad("ссылка или не каталог")
	}
	if err != nil {
		return cerr.Wrap(err, cerr.ExitState, cerr.CodeIORead, "%s: каталог настроек %s", d.ext, dir)
	}
	defer func() { _ = syscall.Close(fd) }()
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		return cerr.Wrap(err, cerr.ExitState, cerr.CodeIORead, "%s: каталог настроек %s", d.ext, dir)
	}
	if int(st.Uid) != d.uid {
		return bad("владелец uid %d, нужен %d", st.Uid, d.uid)
	}
	if st.Mode&0o022 != 0 {
		return bad("права %04o разрешают запись группе или всем", st.Mode&0o7777)
	}
	return nil
}

// openPlain opens a regular file without following a final link; a link
// or a non-regular file is fsx.ErrUnsafePerm.
func openPlain(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, syscall.ELOOP) {
		return nil, fmt.Errorf("%s: ссылка: %w", path, fsx.ErrUnsafePerm)
	}
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err == nil && !st.Mode().IsRegular() {
		err = fmt.Errorf("%s: не обычный файл: %w", path, fsx.ErrUnsafePerm)
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}
