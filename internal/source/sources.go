// Package source holds catalog sources (spec §9.1–§9.2): sources.yaml, the
// only network access of cream (Sync), the source cache with sources.lock,
// safe archive extraction and the deterministic catalog archive (§11.2).
//
// cream 1.0 implements the types archive and path; git is reserved (ruling
// Р1). Catalog validation is not done here: the Syncer receives it as a
// function, so source does not import the catalog layer.
package source

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/fsx"
)

// Source types. TypeGit is recognized only to be refused as reserved.
const (
	TypeArchive = "archive"
	TypePath    = "path"
	TypeGit     = "git"
)

// reservedMsg is the text of ruling Р1.
const reservedMsg = "зарезервировано в cream/v1, не реализовано в cream 1.0"

// maxSourcesFile bounds sources.yaml; a real file is a few hundred bytes.
const maxSourcesFile = 1 << 20

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// Source is one entry of sources.yaml. The fields used depend on Type:
// archive — URL (https:// or file:///) and SHA256 (64 hex, lower case);
// path — Path (absolute).
type Source struct {
	Name   string `yaml:"name"`
	Type   string `yaml:"type"`
	URL    string `yaml:"url,omitempty"`
	SHA256 string `yaml:"sha256,omitempty"`
	Path   string `yaml:"path,omitempty"`
}

// File is sources.yaml; the order of Sources is the lookup priority.
//
// Fields named x-… are accepted on load (reserved for extensions such as
// signers, §13) but not kept by Save.
type File struct {
	Sources []Source `yaml:"sources"`
}

// Get returns the source called name.
func (f *File) Get(name string) (Source, bool) {
	for _, s := range f.Sources {
		if s.Name == name {
			return s, true
		}
	}
	return Source{}, false
}

// Add adds s at the end (the lowest priority). The same fields again change
// nothing (changed=false); other fields under an existing name are refused
// (65 source.conflict) unless replace, which overwrites the entry in place.
// Invalid fields are a usage error (64 usage.source); type git is 65
// source.reserved.
func (f *File) Add(s Source, replace bool) (changed bool, err error) {
	s.SHA256 = strings.ToLower(s.SHA256)
	if err := check(s); err != nil {
		var r reservedError
		if errors.As(err, &r) {
			return false, cerr.New(cerr.ExitData, "source.reserved", "источник %s: тип %s %s", s.Name, s.Type, reservedMsg)
		}
		return false, cerr.Wrap(err, cerr.ExitUsage, "usage.source", "источник %q", s.Name)
	}
	for i, cur := range f.Sources {
		if cur.Name != s.Name {
			continue
		}
		if cur == s {
			return false, nil
		}
		if !replace {
			return false, cerr.New(cerr.ExitData, "source.conflict",
				"источник %s уже есть с другими полями; заменить — --replace", s.Name)
		}
		f.Sources[i] = s
		return true, nil
	}
	f.Sources = append(f.Sources, s)
	return true, nil
}

// Remove deletes the source called name (66 not_found.source if absent).
func (f *File) Remove(name string) error {
	for i, s := range f.Sources {
		if s.Name == name {
			f.Sources = append(f.Sources[:i], f.Sources[i+1:]...)
			return nil
		}
	}
	return cerr.New(cerr.ExitNotFound, "not_found.source", "источника %s нет", name)
}

// Save writes the file atomically with mode 0644, creating the directory.
func (f *File) Save(path string) error {
	out := File{Sources: f.Sources}
	if out.Sources == nil {
		out.Sources = []Source{}
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(out); err != nil {
		return cerr.Wrap(err, cerr.ExitInternal, cerr.CodeInternal, "кодирование %s", path)
	}
	if err := enc.Close(); err != nil {
		return cerr.Wrap(err, cerr.ExitInternal, cerr.CodeInternal, "кодирование %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return cerr.Wrap(err, cerr.ExitState, cerr.CodeIOWrite, "каталог %s", filepath.Dir(path))
	}
	if err := fsx.WriteAtomic(path, buf.Bytes(), 0o644); err != nil {
		return cerr.Wrap(err, cerr.ExitState, cerr.CodeIOWrite, "источники %s", path)
	}
	return nil
}

// LoadFile reads sources.yaml; a missing file is an empty list. With
// checkPerm (running as root on the real root, §13) the file is opened
// through fsx.OpenTrusted: owned by root, not writable by group or others
// (77 perm.sources otherwise). Any invalid entry is 65 source.invalid, type
// git is 65 source.reserved.
func LoadFile(path string, checkPerm bool) (*File, error) {
	data, err := readSources(path, checkPerm)
	if errors.Is(err, fs.ErrNotExist) {
		return &File{Sources: []Source{}}, nil
	}
	if err != nil {
		return nil, err
	}
	f, err := parseFile(data)
	if err != nil {
		var ce *cerr.Error
		if errors.As(err, &ce) {
			return nil, err
		}
		return nil, cerr.Wrap(err, cerr.ExitData, "source.invalid", "источники %s", path)
	}
	return f, nil
}

func readSources(path string, checkPerm bool) ([]byte, error) {
	var (
		fd  *os.File
		err error
	)
	if checkPerm {
		fd, err = fsx.OpenTrusted(path, 0, 0o644)
	} else {
		fd, err = os.Open(path)
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, err
	case errors.Is(err, fsx.ErrUnsafePerm):
		return nil, cerr.Wrap(err, cerr.ExitPerm, "perm.sources", "источники %s", path)
	case err != nil:
		return nil, cerr.Wrap(err, cerr.ExitState, cerr.CodeIORead, "источники %s", path)
	}
	defer func() { _ = fd.Close() }()
	data, err := io.ReadAll(io.LimitReader(fd, maxSourcesFile+1))
	if err != nil {
		return nil, cerr.Wrap(err, cerr.ExitState, cerr.CodeIORead, "источники %s", path)
	}
	if len(data) > maxSourcesFile {
		return nil, cerr.New(cerr.ExitData, "source.invalid", "источники %s: файл больше %d байт", path, maxSourcesFile)
	}
	return data, nil
}

// rawFile and rawSource catch every key: unknown ones are refused unless x-….
type rawFile struct {
	Sources []map[string]yaml.Node `yaml:"sources"`
	Extra   map[string]yaml.Node   `yaml:",inline"`
}

func parseFile(data []byte) (*File, error) {
	var raw rawFile
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if err := checkExtra(raw.Extra, "файл"); err != nil {
		return nil, err
	}
	f := &File{Sources: []Source{}}
	seen := map[string]bool{}
	for i, m := range raw.Sources {
		s, err := decodeSource(m)
		if err != nil {
			return nil, fmt.Errorf("запись %d: %w", i+1, err)
		}
		if err := check(s); err != nil {
			var r reservedError
			if errors.As(err, &r) {
				return nil, cerr.New(cerr.ExitData, "source.reserved", "источник %s: тип %s %s", s.Name, s.Type, reservedMsg)
			}
			return nil, fmt.Errorf("источник %q: %w", s.Name, err)
		}
		if seen[s.Name] {
			return nil, fmt.Errorf("источник %s повторяется", s.Name)
		}
		seen[s.Name] = true
		f.Sources = append(f.Sources, s)
	}
	return f, nil
}

func decodeSource(m map[string]yaml.Node) (Source, error) {
	var s Source
	fields := map[string]*string{"name": &s.Name, "type": &s.Type, "url": &s.URL, "sha256": &s.SHA256, "path": &s.Path}
	gitOnly := map[string]bool{"ref": true, "commit": true}
	isGit := false
	if n, ok := m["type"]; ok && n.Kind == yaml.ScalarNode && n.Value == TypeGit {
		isGit = true
	}
	for k, n := range m {
		dst, known := fields[k]
		switch {
		case known:
			if n.Kind != yaml.ScalarNode || n.Tag == "!!null" {
				return Source{}, fmt.Errorf("поле %s: нужна строка", k)
			}
			*dst = n.Value
		case strings.HasPrefix(k, "x-"), isGit && gitOnly[k]:
		default:
			return Source{}, fmt.Errorf("неизвестное поле %s", k)
		}
	}
	s.SHA256 = strings.ToLower(s.SHA256)
	return s, nil
}

func checkExtra(extra map[string]yaml.Node, where string) error {
	for k := range extra {
		if !strings.HasPrefix(k, "x-") {
			return fmt.Errorf("%s: неизвестное поле %s", where, k)
		}
	}
	return nil
}

// reservedError marks a source of a reserved type (git).
type reservedError struct{}

func (reservedError) Error() string { return reservedMsg }

// check validates the fields of s against its type, in both directions.
func check(s Source) error {
	if !nameRE.MatchString(s.Name) {
		return fmt.Errorf("имя %q: нужно [a-z0-9][a-z0-9._-]{0,63}", s.Name)
	}
	switch s.Type {
	case TypeGit:
		return reservedError{}
	case TypeArchive:
		if s.Path != "" {
			return errors.New("у archive нет поля path")
		}
		if err := checkArchiveURL(s.URL); err != nil {
			return err
		}
		if !isSHA256Hex(s.SHA256) {
			return errors.New("sha256 обязателен: 64 шестнадцатеричных знака")
		}
	case TypePath:
		if s.URL != "" || s.SHA256 != "" {
			return errors.New("у path нет полей url и sha256")
		}
		if !filepath.IsAbs(s.Path) {
			return fmt.Errorf("path %q: нужен абсолютный путь", s.Path)
		}
	default:
		return fmt.Errorf("тип %q: нужен archive или path", s.Type)
	}
	return nil
}

// checkArchiveURL accepts https://host/… and file:///absolute only: a bare
// path is refused so a URL and a path are never confused (§9.1).
func checkArchiveURL(raw string) error {
	if raw == "" {
		return errors.New("url обязателен")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("url %q: %w", raw, err)
	}
	switch u.Scheme {
	case "https":
		if u.Host == "" {
			return fmt.Errorf("url %q: нет хоста", raw)
		}
		if u.User != nil {
			return fmt.Errorf("url %q: учётные данные в url не принимаются", raw)
		}
	case "file":
		if u.Host != "" || !strings.HasPrefix(u.Path, "/") || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("url %q: нужен file:///абсолютный/путь", raw)
		}
	case "":
		return fmt.Errorf("url %q: голый путь не принимается, нужен file://", raw)
	default:
		return fmt.Errorf("url %q: только https:// или file://", raw)
	}
	return nil
}

func isSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
