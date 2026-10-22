package resolve

import (
	"errors"
	"regexp"
	"slices"

	"github.com/iamletenkov/cream/internal/catalog"
	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/semver"
	"github.com/iamletenkov/cream/internal/state"
)

// Statuses of a step of cream check.
const (
	CheckOK      = "ok"
	CheckError   = "error"
	CheckSkipped = "skipped"
)

// Operations of a layer of a check step.
const (
	CheckOpInstall = "install"
	CheckOpUpgrade = "upgrade"
	CheckOpNoop    = "noop"
)

// CheckRequest is one run of cream check (spec 2026-10-21 §7.3, §10.3).
type CheckRequest struct {
	// Catalogs are the --catalog sources in priority order.
	Catalogs []*CheckCatalog
	// Host is the build-phase target: arch, family and OS from the flags
	// (empty — not checked); HasCommand is ignored, platform.commands only
	// warns (the guest's PATH is unknown here).
	Host Host
	// Installed is the base image (--installed): its extensions close
	// depends and requires of the steps; the sources they came from exist
	// in the guest (sources.yaml of the base), so steps from those not
	// among Catalogs are skipped like external ones.
	Installed []Node
	// BaseUnknown: the base image is not known here (no --installed, §7.3):
	// a dependency, capability or name nothing in the steps provides may be
	// in the base — a warning (check.maybe_installed, step 0), not an error;
	// a source not among Catalogs may be in its sources.yaml — skipped.
	BaseUnknown bool
	// Steps are the cream command lines of the image, top to bottom.
	Steps []string
	// Cream is the cream the image will run (manifest cream: ranges).
	Cream manifest.CreamBuild
}

// CheckResult is the answer of cream check: one entry per step and the
// extensions installed after the last one (the form of the cream.extensions
// label without digests).
type CheckResult struct {
	Phase     string           `json:"phase"`
	Steps     []CheckStep      `json:"steps"`
	Installed []CheckInstalled `json:"installed"`
}

// CheckStep is the outcome of one --step. Layers are the extensions the
// step installs or upgrades, in install order; Removed — what it removes.
// Warnings belong to this step (skipped steps carry their note there).
type CheckStep struct {
	Step     int            `json:"step"`
	Command  string         `json:"command"`
	Status   string         `json:"status"`
	Layers   []CheckLayer   `json:"layers"`
	Removed  []string       `json:"removed"`
	Error    *CheckErr      `json:"error"`
	Warnings []cerr.Warning `json:"warnings"`
}

// CheckLayer is one extension a step installs (op install), upgrades (op
// upgrade) or leaves as it is (op noop); mode is that of resolve (exec, or
// none for targets.build: none — not installed).
type CheckLayer struct {
	Name    string   `json:"name"`
	Version string   `json:"version"`
	Source  string   `json:"source"`
	Op      string   `json:"op"`
	Mode    string   `json:"mode"`
	Deps    []string `json:"deps"`
}

// CheckErr is the error of a step: the exit code and error.code of cream
// (spec cream-core §11.3) the same command would give in the guest.
type CheckErr struct {
	Exit    int            `json:"exit"`
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

// CheckInstalled is one extension after the last step.
type CheckInstalled struct {
	Name     string   `json:"name"`
	Version  string   `json:"version"`
	Source   string   `json:"source"`
	Provides []string `json:"provides"`
}

// FirstError returns the exit code and the error of the first failed step
// (0, nil when every step is ok or skipped).
func (r CheckResult) FirstError() (int, *CheckErr) {
	for _, s := range r.Steps {
		if s.Error != nil {
			return s.Error.Exit, s.Error
		}
	}
	return 0, nil
}

// reSecretStdin is the standard tail `< /run/secrets/<id>` that feeds the
// value of KEY=- (§7.2): stripped before the words are split.
var reSecretStdin = regexp.MustCompile(`^(.*\S)\s*<\s*/run/secrets/[A-Za-z0-9._-]+\s*$`)

// checkFromBase marks an external source known only from the base image.
const checkFromBase = "base"

// errSkip ends a step as skipped; the note is already a warning.
var errSkip = errors.New("skipped")

// checker carries the state between steps: the installed extensions, the
// names a skipped or failed step may have installed (unknown, by step) and
// the external sources (`source add` — not checked on the host, §7.4).
type checker struct {
	req      CheckRequest
	nodes    map[string]Node
	unknown  map[string]int
	external map[string]string
	// wild is the last step skipped as non-standard: it may have done
	// anything, so names and sources the check has not seen may exist.
	wild int
}

// Check runs the steps against the catalogs and the base image, carrying
// the state from step to step, without hooks, root, network or writes
// (§10.3). Each step mirrors the command in the guest: install (with the
// --with-deps of §10.2: depends by name from the source of the extension
// that needs them, requires never), upgrade, remove, source add. A step
// whose extension comes from an external source is skipped with a note;
// names of a skipped or failed step are unknown afterwards, so a later
// dependency on them only warns (check.maybe_installed). Other cream
// subcommands are skipped (check.unsupported).
func Check(r CheckRequest) CheckResult {
	c := &checker{req: r, nodes: map[string]Node{}, unknown: map[string]int{}, external: map[string]string{}}
	c.req.Host.Phase = PhaseBuild
	c.req.Host.HasCommand = nil
	for _, n := range r.Installed {
		n.Installed = true
		c.nodes[n.Manifest.Name] = n
		if src := n.Source.Name; src != "" && !hasSrcIn(r.Catalogs, src) {
			c.external[src] = checkFromBase
		}
	}
	res := CheckResult{Phase: PhaseBuild, Steps: make([]CheckStep, 0, len(r.Steps))}
	for i, line := range r.Steps {
		res.Steps = append(res.Steps, c.step(i+1, line))
	}
	res.Installed = c.installed()
	return res
}

func (c *checker) step(n int, line string) CheckStep {
	s := CheckStep{Step: n, Command: line, Status: CheckOK, Layers: []CheckLayer{}, Removed: []string{}}
	var w cerr.Warnings
	cmd := line
	if m := reSecretStdin.FindStringSubmatch(line); m != nil {
		cmd = m[1]
	}
	words, err := splitStep(cmd)
	switch {
	case cerr.CodeOf(err) == "usage.step":
		w.AddDetails("check.nonstandard", map[string]any{"step": n},
			"шаг %d: %s — нестандартная форма, проверит только гость", n, err.Error())
		c.wild = n
		err = errSkip
	case err == nil && len(words) == 0:
		err = cerr.New(cerr.ExitUsage, "usage.step", "шаг пуст")
	}
	var names []string
	if err == nil {
		names, err = c.run(n, &s, words, &w)
	}
	s.Warnings = w.List()
	switch {
	case errors.Is(err, errSkip):
		s.Status = CheckSkipped
		c.forget(n, names)
	case err != nil:
		s.Status = CheckError
		s.Error = checkErr(err)
		c.forget(n, names)
	}
	return s
}

// forget marks names of a skipped or failed step that are not installed
// as unknown: the guest may have them after this step.
func (c *checker) forget(n int, names []string) {
	for _, name := range names {
		if _, ok := c.nodes[name]; !ok {
			c.unknown[name] = n
		}
	}
}

func checkErr(err error) *CheckErr {
	var ce *cerr.Error
	if errors.As(err, &ce) {
		details := ce.Details
		if details == nil {
			details = map[string]any{}
		}
		return &CheckErr{Exit: ce.Exit, Code: ce.Code, Message: ce.Error(), Details: details}
	}
	return &CheckErr{Exit: cerr.ExitInternal, Code: "internal", Message: err.Error(), Details: map[string]any{}}
}

// run dispatches one step; names are the extensions it is about (for
// forget on a failure).
func (c *checker) run(n int, s *CheckStep, words []string, w *cerr.Warnings) ([]string, error) {
	sub, args := words[0], words[1:]
	switch sub {
	case "install":
		return c.install(s, args, w)
	case "upgrade":
		return c.upgrade(s, args, w)
	case "remove":
		return c.remove(s, args, w)
	case "source":
		return nil, c.source(args, w)
	}
	w.Add("check.unsupported", "шаг %d: cream %s не проверяется на хосте — проверит гость", n, sub)
	return nil, errSkip
}

// installed is the state after the last step, sorted by name.
func (c *checker) installed() []CheckInstalled {
	names := make([]string, 0, len(c.nodes))
	for name := range c.nodes {
		names = append(names, name)
	}
	slices.Sort(names)
	out := make([]CheckInstalled, 0, len(names))
	for _, name := range names {
		nd := c.nodes[name]
		prov := append([]string{}, nd.Manifest.Provides...)
		out = append(out, CheckInstalled{Name: name, Version: nd.Manifest.Version.String(), Source: nd.Source.Name,
			Provides: prov})
	}
	return out
}

// installedNodes are the installed nodes in name order (Resolve is
// deterministic over them).
func (c *checker) installedNodes() []Node {
	names := make([]string, 0, len(c.nodes))
	for name := range c.nodes {
		names = append(names, name)
	}
	slices.Sort(names)
	out := make([]Node, 0, len(names))
	for _, name := range names {
		out = append(out, c.nodes[name])
	}
	return out
}

// checkEntry is an extension found in a check catalog.
type checkEntry struct {
	m   *manifest.Manifest
	cat *CheckCatalog
}

func (e checkEntry) ref() state.SourceRef {
	return state.SourceRef{Name: e.cat.name, Type: e.cat.typ, CatalogVersion: e.cat.Version()}
}

// lookup finds ref like catalog.Lookup over the check catalogs: a named
// source, else the first catalog that has the name (later ones warn
// source.shadowed). A ref into an external source, or a name found nowhere
// while external sources exist, is skipped with a note.
func (c *checker) lookup(ref catalog.Ref, w *cerr.Warnings) (checkEntry, error) {
	cats := c.req.Catalogs
	if ref.Source != "" {
		if c.isExternal(ref.Source) {
			w.AddDetails("check.external", map[string]any{"ext": ref.Name, "source": ref.Source},
				"%s: внешний каталог %s — проверит гость", ref.Name, ref.Source)
			return checkEntry{}, errSkip
		}
		i := slices.IndexFunc(cats, func(x *CheckCatalog) bool { return x.name == ref.Source })
		if i < 0 {
			e := cerr.New(cerr.ExitNotFound, "not_found.source", "источника %s нет среди --catalog", ref.Source)
			e.Details = map[string]any{"source": ref.Source}
			return checkEntry{}, e
		}
		cats = cats[i : i+1]
	}
	var found checkEntry
	for _, cat := range cats {
		if _, replaced := c.external[cat.name]; replaced {
			continue
		}
		if found.m != nil {
			if m, err := cat.find(ref.Name, c.req.Cream, &cerr.Warnings{}); err == nil && m != nil {
				w.Add("source.shadowed", "%s из источника %s затеняет %s из %s", ref.Name, found.cat.name, ref.Name, cat.name)
			}
			continue
		}
		m, err := cat.find(ref.Name, c.req.Cream, w)
		if err != nil {
			return checkEntry{}, err
		}
		if m != nil {
			found = checkEntry{m: m, cat: cat}
		}
	}
	if found.m == nil {
		if ref.Source == "" && (len(c.external) > 0 || c.req.BaseUnknown || c.wild > 0) {
			w.AddDetails("check.external", map[string]any{"ext": ref.Name},
				"%s нет в --catalog — возможно, во внешнем каталоге; проверит гость", ref.Name)
			return checkEntry{}, errSkip
		}
		e := cerr.New(cerr.ExitNotFound, "not_found.extension", "расширения %s нет в источниках", ref.Name)
		e.Details = map[string]any{"ext": ref.Name}
		return checkEntry{}, e
	}
	if ref.Version != nil && semver.Compare(*ref.Version, found.m.Version) != 0 {
		e := cerr.New(cerr.ExitNotFound, "version.mismatch", "%s: в источнике %s версия %s, а не %s",
			ref.Name, found.cat.name, found.m.Version, ref.Version)
		e.Details = map[string]any{"ext": ref.Name, "source": found.cat.name,
			"found": found.m.Version.String(), "want": ref.Version.String()}
		return checkEntry{}, e
	}
	return found, nil
}

// reSourceName is a source name (§9.1).
var reSourceName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// reSHA256 is the hex sha256 of an archive.
var reSHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// source checks `source add <name> --archive URL --sha256 H`: the source
// becomes external (its archive is not fetched here, §7.4). `--path` is
// the non-standard form — skipped; sync is a no-op here; the rest is
// skipped as unsupported.
func (c *checker) source(args []string, w *cerr.Warnings) error {
	if len(args) == 0 {
		return cerr.New(cerr.ExitUsage, "usage.args", "source: нужна подкоманда")
	}
	switch args[0] {
	case "sync":
		return nil
	case "add":
	default:
		w.Add("check.unsupported", "cream source %s не проверяется на хосте — проверит гость", args[0])
		return errSkip
	}
	f, pos, err := parseStepFlags(args[1:], map[string]bool{"archive": true, "sha256": true, "path": true},
		map[string]bool{"sync": true, "replace": true})
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return cerr.New(cerr.ExitUsage, "usage.args", "source add: нужно одно имя источника")
	}
	name := pos[0]
	if !reSourceName.MatchString(name) {
		return cerr.New(cerr.ExitUsage, "usage.source", "имя источника %q: ожидается [a-z0-9][a-z0-9._-]{0,63}", name)
	}
	replace := f["replace"] != ""
	if !replace && slices.ContainsFunc(c.req.Catalogs, func(x *CheckCatalog) bool { return x.name == name }) {
		e := cerr.New(cerr.ExitData, "source.conflict", "источник %s уже есть (--catalog)", name)
		e.Details = map[string]any{"source": name}
		return e
	}
	switch {
	case f["path"] != "" && f["archive"] == "":
		c.external[name] = "path " + f["path"]
		w.AddDetails("check.nonstandard", map[string]any{"source": name},
			"source add %s --path: нестандартная форма — проверит гость", name)
		return errSkip
	case f["archive"] == "" || f["path"] != "":
		return cerr.New(cerr.ExitUsage, "usage.args", "source add %s: нужен ровно один из --archive и --path", name)
	case f["sha256"] == "":
		return cerr.New(cerr.ExitUsage, "usage.args", "source add %s --archive: нужен --sha256", name)
	case !reSHA256.MatchString(f["sha256"]):
		return cerr.New(cerr.ExitUsage, "usage.args", "source add %s: --sha256 — 64 шестнадцатеричных знака", name)
	}
	fields := f["archive"] + " " + f["sha256"]
	if prev, ok := c.external[name]; ok && !replace && prev != checkFromBase && prev != fields {
		e := cerr.New(cerr.ExitData, "source.conflict", "источник %s уже добавлен с другими полями", name)
		e.Details = map[string]any{"source": name}
		return e
	}
	c.external[name] = fields
	w.AddDetails("check.external", map[string]any{"source": name},
		"внешний каталог %s на хост не скачивается — его расширения проверит гость", name)
	return nil
}

// isExternal: steps from the source are checked by the guest — added by
// `source add`, known from the base, or any source when the base is unknown.
func (c *checker) isExternal(name string) bool {
	if _, ok := c.external[name]; ok {
		return true
	}
	return (c.req.BaseUnknown || c.wild > 0) && !hasSrcIn(c.req.Catalogs, name)
}
