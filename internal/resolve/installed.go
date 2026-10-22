package resolve

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"strings"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/semver"
	"github.com/iamletenkov/cream/internal/state"
)

// CodeInstalledInvalid is a malformed --installed file (65).
const CodeInstalledInvalid = "installed.invalid"

// maxInstalledSize bounds the --installed file.
const maxInstalledSize = 1 << 20

// reCapability is a provides token (§4.2).
var reCapability = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// reLineageToken is a lineage token of §12.4: [<source>/]<name>@<version>[#<rev>].
var reLineageToken = regexp.MustCompile(`^(?:[a-z0-9][a-z0-9._-]{0,63}/)?([a-z0-9][a-z0-9._-]{0,63})@([^#@/\s]+)(?:#[0-9a-f]+)?$`)

// installedItem is one element of the --installed file: name and version, or
// a lineage token instead of both; provides and source are optional. Other
// fields (the rest of a lineage entry, digest of the cream.extensions label)
// are ignored.
type installedItem struct {
	Name     string   `json:"name"`
	Version  string   `json:"version"`
	Token    string   `json:"token"`
	Source   string   `json:"source"`
	Provides []string `json:"provides"`
}

// ReadInstalled reads the extensions already installed in the base image of
// a build layer (`cream resolve --installed`, from the lineage of FROM; `cream
// check --installed`, the cream.extensions label of the base, §8.2): a JSON
// array of {name, version, source?, provides?} or {token, provides?}. source
// is where the base got the extension (upgrade looks there). They join
// the graph as installed nodes, so depends and requires of the layer are
// closed by them. A later element of the same name replaces an earlier one
// (lineage accumulates base first).
//
// Errors: 66 not_found.installed, 74 io.read, 65 installed.invalid.
func ReadInstalled(path string) ([]Node, error) {
	data, err := readInstalledFile(path)
	if err != nil {
		return nil, err
	}
	bad := func(format string, a ...any) error {
		e := cerr.New(cerr.ExitData, CodeInstalledInvalid, "--installed %s: %s", path, fmt.Sprintf(format, a...))
		e.Details = map[string]any{"file": path}
		return e
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	var items []installedItem
	if err := dec.Decode(&items); err != nil {
		return nil, bad("нужен JSON-массив [{name, version, provides?}]: %v", err)
	}
	if dec.More() {
		return nil, bad("после массива есть данные")
	}
	if items == nil {
		return nil, bad("нужен JSON-массив [{name, version, provides?}]")
	}
	var order []string
	byName := map[string]*manifest.Manifest{}
	sourceOf := map[string]string{}
	for i, it := range items {
		name, ver := it.Name, it.Version
		if it.Token != "" {
			sub := reLineageToken.FindStringSubmatch(it.Token)
			if sub == nil {
				return nil, bad("[%d].token %q: ожидается [источник/]имя@версия[#ревизия]", i, it.Token)
			}
			if (name != "" && name != sub[1]) || (ver != "" && ver != sub[2]) {
				return nil, bad("[%d]: token %q расходится с name/version", i, it.Token)
			}
			name, ver = sub[1], sub[2]
		}
		if !manifest.ValidName(name) {
			return nil, bad("[%d].name %q: не имя расширения", i, name)
		}
		v, err := semver.Parse(ver)
		if err != nil {
			return nil, bad("[%d].version %q: %v", i, ver, err)
		}
		for _, c := range it.Provides {
			if !reCapability.MatchString(c) {
				return nil, bad("[%d].provides %q: ожидается [a-z0-9][a-z0-9._-]{0,63}", i, c)
			}
		}
		if it.Source != "" && !reCapability.MatchString(it.Source) {
			return nil, bad("[%d].source %q: ожидается [a-z0-9][a-z0-9._-]{0,63}", i, it.Source)
		}
		sourceOf[name] = it.Source
		if _, seen := byName[name]; !seen {
			order = append(order, name)
		}
		byName[name] = &manifest.Manifest{Schema: manifest.SchemaV1, Name: name, Version: v,
			Kind: manifest.KindExtension, Provides: append([]string(nil), it.Provides...)}
	}
	out := make([]Node, 0, len(order))
	for _, name := range order {
		out = append(out, Node{Manifest: byName[name], Installed: true, Files: []string{},
			Source: state.SourceRef{Name: sourceOf[name]}})
	}
	return out, nil
}

func readInstalledFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, cerr.Wrap(err, cerr.ExitNotFound, "not_found.installed", "--installed: нет файла %s", path)
		}
		return nil, cerr.Wrap(err, cerr.ExitState, cerr.CodeIORead, "--installed: чтение %s", path)
	}
	defer f.Close() //nolint:errcheck // только чтение
	data, err := io.ReadAll(io.LimitReader(f, maxInstalledSize+1))
	if err != nil {
		return nil, cerr.Wrap(err, cerr.ExitState, cerr.CodeIORead, "--installed: чтение %s", path)
	}
	if len(data) > maxInstalledSize {
		return nil, cerr.New(cerr.ExitData, CodeInstalledInvalid, "--installed %s: больше %d байт", path, maxInstalledSize)
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil, cerr.New(cerr.ExitData, CodeInstalledInvalid, "--installed %s: пустой файл", path)
	}
	return data, nil
}
