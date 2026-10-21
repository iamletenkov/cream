package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/iamletenkov/cream/internal/catalog"
	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/config"
	"github.com/iamletenkov/cream/internal/keytype"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/resolve"
	"github.com/iamletenkov/cream/internal/source"
	"github.com/iamletenkov/cream/internal/state"
)

// BuildRequest is `cream install --phase build --from-dir D --record F
// --params-file P [--remove-params-file]` inside the guest (§12.2).
type BuildRequest struct {
	// FromDir is the extension tree delivered by bisquite,
	// /var/lib/cream/incoming/<id>/<name>; its directory name is the name.
	FromDir string
	// Record is record.json: the layer of `cream resolve --json` (Р6).
	Record string
	// ParamsFile holds every parameter of the layer, KEY=VALUE ("" — none).
	ParamsFile string
	// RemoveParamsFile deletes ParamsFile once cream has read it, whatever
	// the outcome: the secrets in it do not stay in the image.
	RemoveParamsFile bool
	// CheckPerm requires ParamsFile to be 0600 and owned by root (on the
	// real root only, ruling Р9).
	CheckPerm bool
	// AllowSkip turns hook code 20 into status skipped without an error.
	AllowSkip bool
}

// buildRecord is record.json: one layer of `cream resolve --json` (§12.4).
// The registry form of the source is the layer's source, source_resolved and
// catalog_version; source_type and source_origin are optional additions of
// the caller (without them the type follows pinned: archive or path).
type buildRecord struct {
	resolve.Layer
	SourceType   string `json:"source_type,omitempty"`
	SourceOrigin string `json:"source_origin,omitempty"`
}

// Fields of 65 record.mismatch (details.field).
const (
	buildFieldName    = "name"
	buildFieldVersion = "version"
	buildFieldDigest  = "ext_digest"
	buildFieldParams  = "params_digest"
)

// InstallFromDir installs one extension in the build phase (§12.2 п.5):
// under lock/global it rechecks the delivered tree and its parameters
// against record.json (65 record.mismatch), checks platform and graph
// against the guest registry (dependencies inherited through FROM count),
// runs the install hook under policy-rc.d (exit 101, only when the guest
// has none), enables configure by links without running it and writes the
// registry with phase build. After success incoming/<id> is removed; on a
// failure it stays for inspection. ParamsFile goes with RemoveParamsFile
// in any case.
func (e *Engine) InstallFromDir(ctx context.Context, r BuildRequest) (*InstallResult, error) {
	if r.RemoveParamsFile && r.ParamsFile != "" {
		defer e.buildDropParams(r.ParamsFile)
	}
	if r.FromDir == "" || r.Record == "" {
		return nil, cerr.New(cerr.ExitUsage, "usage.args", "фаза build: нужны --from-dir и --record")
	}
	dir, err := filepath.Abs(r.FromDir)
	if err != nil {
		return nil, cerr.Wrap(err, cerr.ExitUsage, "usage.args", "--from-dir %s", r.FromDir)
	}

	g, err := e.Locks.Global(ctx)
	if err != nil {
		return nil, err
	}
	defer g.Release()
	rec, err := buildReadRecord(r.Record)
	if err != nil {
		return nil, err
	}
	en, err := catalog.LoadExtension(dir, e.Cream)
	if err != nil {
		return nil, err
	}
	m := en.Manifest
	if m.Targets.Build == manifest.BuildNone {
		ce := cerr.New(cerr.ExitNotApplicable, "target.build", "%s: targets.build: none — в фазе build не ставится", m.Name)
		ce.Details = map[string]any{"ext": m.Name}
		return nil, ce
	}
	if err := buildCompare(m.Name, buildFieldName, rec.Name, m.Name); err != nil {
		return nil, err
	}
	if err := buildCompare(m.Name, buildFieldVersion, rec.Version, m.Version.String()); err != nil {
		return nil, err
	}
	if err := buildCompare(m.Name, buildFieldDigest, rec.ExtDigest, en.Digest); err != nil {
		return nil, err
	}
	var changes []config.Change
	if r.ParamsFile != "" {
		if changes, err = config.ParseAssignments(config.AssignInput{ParamsFile: r.ParamsFile,
			CheckPerm: r.CheckPerm}, m.Config); err != nil {
			return nil, err
		}
		if r.RemoveParamsFile {
			e.buildDropParams(r.ParamsFile)
		}
	}
	if err := buildCompare(m.Name, buildFieldParams, rec.ParamsDigest, buildParamsDigest(m, changes)); err != nil {
		return nil, err
	}
	en.Source = buildSourceDir(rec)

	all, err := e.Installed()
	if err != nil {
		return nil, err
	}
	drop, err := e.policyRcDPut()
	if err != nil {
		return nil, err
	}
	b := e.buildPhase()
	var byExt map[string][]config.Change
	if len(changes) > 0 {
		byExt = map[string][]config.Change{m.Name: changes}
	}
	res, err := b.installEntries(ctx, []catalog.Entry{en}, byExt, all, nil, installOpts{allowSkip: r.AllowSkip})
	drop()
	if len(res) == 0 {
		if err == nil {
			err = cerr.New(cerr.ExitInternal, "internal", "%s: фаза build не дала результата", m.Name)
		}
		return nil, err
	}
	if err == nil {
		e.buildDropIncoming(dir)
	}
	return &res[0], err
}

// buildGuard puts cream's policy-rc.d for the hooks of install, upgrade and
// remove in the build phase (§10.2) and returns what takes it away; in live
// it does nothing.
func (e *Engine) buildGuard() (func(), error) {
	if e.Phase() != PhaseBuild {
		return func() {}, nil
	}
	return e.policyRcDPut()
}

// buildPhase is a copy of the engine whose Phase() is build: hooks get
// CREAM_PHASE=build, configure is only enabled, systemd is not touched.
func (e *Engine) buildPhase() *Engine {
	b := *e
	b.phase = PhaseBuild
	env := e.Env
	b.Env = func(k string) string {
		if k == "CREAM_PHASE" {
			return PhaseBuild
		}
		if env == nil {
			return ""
		}
		return env(k)
	}
	return &b
}

// buildReadRecord reads record.json (65 record.invalid when it is not a layer).
func buildReadRecord(path string) (buildRecord, error) {
	var rec buildRecord
	data, err := os.ReadFile(path)
	if err != nil {
		exit, code := cerr.ExitState, cerr.CodeIORead
		if errors.Is(err, fs.ErrNotExist) {
			exit, code = cerr.ExitNotFound, "record.not_found"
		}
		return rec, cerr.Wrap(err, exit, code, "record.json %s", path)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&rec); err != nil {
		return rec, cerr.Wrap(err, cerr.ExitData, "record.invalid", "record.json %s: не слой resolve --json", path)
	}
	if rec.Name == "" || rec.Version == "" || rec.ExtDigest == "" || rec.ParamsDigest == "" {
		return rec, cerr.New(cerr.ExitData, "record.invalid",
			"record.json %s: нужны name, version, ext_digest и params_digest", path)
	}
	return rec, nil
}

// buildCompare is 65 record.mismatch when the delivered value differs from
// record.json. Values are names, versions and digests, never parameters.
func buildCompare(ext, field, want, found string) error {
	if want == found {
		return nil
	}
	ce := cerr.New(cerr.ExitData, "record.mismatch", "%s: %s в госте %s, а в record.json %s", ext, field, found, want)
	ce.Details = map[string]any{"ext": ext, "field": field, "record": want, "found": found}
	return ce
}

// buildParamsDigest is params_digest of the layer's parameters, as resolve
// computes it on the host: a secret enters only as "<secret>", a scope: build
// key not at all (§10.9).
func buildParamsDigest(m *manifest.Manifest, changes []config.Change) string {
	vals := make(map[string]string, len(changes))
	for _, c := range changes {
		if sp, ok := m.Config.Lookup(c.Key); ok && sp.Scope == keytype.ScopeBuild {
			continue
		}
		vals[c.Key] = c.Value
	}
	return state.ParamsDigest(vals, func(k string) bool {
		sp, ok := m.Config.Lookup(k)
		return ok && sp.Secret != ""
	})
}

// buildSourceDir is the source of record.json in the form installSourceRef
// turns into the registry's source.
func buildSourceDir(rec buildRecord) source.SourceDir {
	typ := rec.SourceType
	if typ == "" {
		typ = source.TypePath
		if rec.Pinned {
			typ = source.TypeArchive
		}
	}
	return source.SourceDir{Source: source.Source{Name: rec.Source, Type: typ},
		Lock: &source.LockEntry{Name: rec.Source, Type: typ, Origin: rec.SourceOrigin,
			Resolved: rec.SourceResolved, CatalogVersion: rec.CatalogVersion}}
}

// buildDropParams removes params.env (--remove-params-file).
func (e *Engine) buildDropParams(p string) {
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		e.warn("build.params_file", "%s не удалён: %v", p, err)
	}
}

// buildDropIncoming removes incoming/<id> after a successful install, when
// the tree came from there (incoming/<id>/<name>); any other --from-dir is
// the caller's and stays.
func (e *Engine) buildDropIncoming(dir string) {
	inc := filepath.Dir(dir)
	if filepath.Dir(inc) != filepath.Clean(e.L.Incoming("")) {
		return
	}
	if err := os.RemoveAll(inc); err != nil {
		e.warn("build.incoming", "%s не удалён: %v", inc, err)
	}
}
