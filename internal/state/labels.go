package state

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/iamletenkov/cream/internal/manifest"
)

// CodeLabelsWriteFailed is the warning of a cream.json that could not be
// written: the operation itself succeeds (spec bisquite-bundled-cream §8.2).
const CodeLabelsWriteFailed = "labels.write_failed"

// CodeLabelsTooLarge is the warning of a cream.extensions value longer than
// MaxLabelValue: bisquite skips such a file whole (spec §8.1), so the image
// gets no cream.* labels; the file is still written in full.
const CodeLabelsTooLarge = "labels.too_large"

// MaxLabelValue is the limit of one label value bisquite reads from
// /etc/image-labels.d (spec §8.1): 8 KiB.
const MaxLabelValue = 8 << 10

// maxManifestSize bounds the manifest read for provides.
const maxManifestSize = 1 << 20

// labelExt is one element of cream.extensions: no parameters, params_digest
// or paths — settings and secrets never reach the labels.
type labelExt struct {
	Name     string   `json:"name"`
	Version  string   `json:"version"`
	Source   string   `json:"source"`
	Digest   string   `json:"digest"`
	Provides []string `json:"provides"`
}

// labelSource is one element of cream.sources.
type labelSource struct {
	Name           string `json:"name"`
	Type           string `json:"type"`
	CatalogVersion string `json:"catalog_version"`
}

// writeLabels rewrites /etc/image-labels.d/cream.json from the whole
// registry (§8.2): a descendant image inherits the file, so it describes the
// base too. Only records with status installed count. Any failure is the
// labels.write_failed warning, never an error of the operation.
func (s *Store) writeLabels() {
	if err := s.labels(); err != nil && s.Warn != nil {
		s.Warn.Add(CodeLabelsWriteFailed, "%s: метки образа не записаны: %v", s.layout.ImageLabels(), err)
	}
}

func (s *Store) labels() error {
	recs, err := s.List(nil)
	if err != nil {
		return err
	}
	exts := []labelExt{}
	seen := map[labelSource]bool{}
	srcs := []labelSource{}
	for _, r := range recs {
		if r.Status != StatusInstalled {
			continue
		}
		exts = append(exts, labelExt{Name: r.Name, Version: r.Version, Source: r.Source.Name,
			Digest: r.ExtDigest, Provides: s.provides(r.Name)})
		src := labelSource{Name: r.Source.Name, Type: r.Source.Type, CatalogVersion: r.Source.CatalogVersion}
		if !seen[src] {
			seen[src] = true
			srcs = append(srcs, src)
		}
	}
	sort.Slice(srcs, func(i, j int) bool {
		a, b := srcs[i], srcs[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		return a.CatalogVersion < b.CatalogVersion
	})
	extJSON, err := json.Marshal(exts)
	if err != nil {
		return err
	}
	srcJSON, err := json.Marshal(srcs)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(map[string]string{
		"cream.version":    creamVersion(s.cream),
		"cream.extensions": string(extJSON),
		"cream.sources":    string(srcJSON),
	}, "", "  ")
	if err != nil {
		return err
	}
	if len(extJSON) > MaxLabelValue && !s.warnedLarge && s.Warn != nil {
		s.warnedLarge = true
		s.Warn.Add(CodeLabelsTooLarge,
			"%s: cream.extensions — %d байт, больше %d: bisquite пропустит файл, метки cream.* в образ не попадут",
			s.layout.ImageLabels(), len(extJSON), MaxLabelValue)
	}
	path := s.layout.ImageLabels()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return s.write(path, append(data, '\n'), 0o644)
}

// provides reads the provides of the installed tree's manifest; a missing
// or broken manifest gives none (the registry stays the truth of what is
// installed, provides only helps the descendant check).
func (s *Store) provides(name string) []string {
	out := []string{}
	f, err := os.Open(filepath.Join(s.layout.ExtTree(name), manifest.ManifestFile))
	if err != nil {
		return out
	}
	defer f.Close() //nolint:errcheck // только чтение
	var m struct {
		Provides []string `yaml:"provides"`
	}
	data, err := io.ReadAll(io.LimitReader(f, maxManifestSize+1))
	if err != nil || len(data) > maxManifestSize || yaml.Unmarshal(data, &m) != nil {
		return out
	}
	return append(out, m.Provides...)
}

// creamVersion is the build version without a leading "v"; unset is "dev".
func creamVersion(raw string) string {
	v := strings.TrimPrefix(strings.TrimSpace(raw), "v")
	if v == "" {
		return "dev"
	}
	return v
}
