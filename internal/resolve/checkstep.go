package resolve

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/iamletenkov/cream/internal/catalog"
	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/keytype"
	"github.com/iamletenkov/cream/internal/manifest"
	"github.com/iamletenkov/cream/internal/semver"
)

// Flags of the steps: own flags of install/upgrade/remove/source add in
// the guest plus the bool globals; anything else is 64 usage.flag.
var (
	checkGlobalBools  = map[string]bool{"json": true, "q": true, "v": true}
	checkGlobalValues = map[string]bool{"lock-timeout": true}
)

// install mirrors `cream install` in the build phase (§10.2).
func (c *checker) install(s *CheckStep, args []string, w *cerr.Warnings) ([]string, error) {
	f, pos, err := parseStepFlags(args, map[string]bool{"source": true, "params-file": true, "phase": true},
		map[string]bool{"with-deps": true, "reinstall": true, "accept-risk": true, "allow-skip": true})
	if err != nil {
		return nil, err
	}
	if err := checkPhaseFlag(f); err != nil {
		return nil, err
	}
	var refs []catalog.Ref
	var params []string
	for _, a := range pos {
		if strings.Contains(a, "=") {
			params = append(params, a)
			continue
		}
		ref, err := catalog.ParseRef(a)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	names := make([]string, 0, len(refs))
	for _, r := range refs {
		names = append(names, r.Name)
	}
	if len(refs) == 0 {
		return nil, cerr.New(cerr.ExitUsage, "usage.args", "install: нужна ссылка на расширение")
	}
	if len(refs) > 1 && (len(params) > 0 || f["params-file"] != "") {
		return names, cerr.New(cerr.ExitUsage, "usage.args", "install: несколько ссылок — только без параметров")
	}
	entries := make([]checkEntry, 0, len(refs))
	for i, ref := range refs {
		if src := f["source"]; src != "" {
			if ref.Source != "" && ref.Source != src {
				ce := cerr.New(cerr.ExitUsage, "usage.source", "ссылка %s и --source %s называют разные источники",
					ref, src)
				ce.Details = map[string]any{"ref": ref.String(), "source": src}
				return names, ce
			}
			refs[i].Source = src
		}
		en, err := c.lookup(refs[i], w)
		if err != nil {
			return names, err
		}
		entries = append(entries, en)
	}
	byExt := map[string]map[string]string{}
	if len(params) > 0 {
		p, err := CheckParams(entries[0].m, params)
		if err != nil {
			return names, err
		}
		byExt[entries[0].m.Name] = p
	}
	if pf := f["params-file"]; pf != "" {
		w.AddDetails("check.params_file", map[string]any{"ext": entries[0].m.Name, "file": pf},
			"%s: параметры из %s не видны на хосте — проверит гость", entries[0].m.Name, pf)
	}
	if f["with-deps"] != "" {
		if entries, err = c.withDeps(entries, w); err != nil {
			return names, err
		}
		names = names[:0]
		for _, en := range entries {
			names = append(names, en.m.Name)
		}
	}
	ops := make(map[string]string, len(entries))
	for _, en := range entries {
		op, err := c.decide(en)
		if err != nil {
			return names, err
		}
		ops[en.m.Name] = op
	}
	bo := buildOpts{reinstall: f["reinstall"] != ""}
	if f["params-file"] != "" {
		bo.paramsFile = entries[0].m.Name
	}
	return names, c.apply(s, entries, ops, byExt, bo, w)
}

// buildOpts tell apply how the guest's install would treat the required
// `scope: build` keys: paramsFile — the extension whose values come from
// --params-file (unseen here), reinstall — --reinstall runs the install hook
// of the requested extensions again.
type buildOpts struct {
	paramsFile string
	reinstall  bool
}

// checkBuildRequired runs buildRequired over the layers in install order,
// as the guest's installOne does after the graph: an extension the hook
// install will run for (op install, or --reinstall of a requested one),
// not targets.build: none, not from --params-file. An extension that may
// already be in the guest (unknown base, a skipped step) is a warning: the
// guest skips the hook for the same version installed.
func (c *checker) checkBuildRequired(layers []Layer, orig map[string]checkEntry, ops map[string]string,
	params map[string]map[string]string, requested map[string]bool, bo buildOpts, w *cerr.Warnings) error {
	for _, l := range layers {
		en := orig[l.Name]
		runs := ops[l.Name] == CheckOpInstall || (bo.reinstall && requested[l.Name])
		if !runs || l.Mode == ModeNone || l.Name == bo.paramsFile {
			continue
		}
		ce := buildRequired(en.m, params[l.Name])
		if ce == nil {
			continue
		}
		if _, have := c.nodes[l.Name]; !have {
			if n, maybe := c.maybe(l.Name); maybe {
				w.AddDetails("config.build_required", map[string]any{"ext": l.Name, "key": ce.Details["key"], "step": n},
					"%s, если %s ещё не стоит (возможно, %s) — проверит гость", ce.Message, l.Name, maybeWhere(n))
				continue
			}
		}
		return ce
	}
	return nil
}

// buildRequired mirrors config.BuildValues of the guest's install hook: a
// required `scope: build` key (empty: false, no default, not a pattern) the
// step does not give (KEY=VALUE or KEY=-) is 64 config.build_required, the
// smallest such key named. A --params-file step is not checked: its
// contents are seen only by the guest (check.params_file).
func buildRequired(m *manifest.Manifest, given map[string]string) *cerr.Error {
	missing := ""
	for k, sp := range m.Config {
		if sp.Scope != keytype.ScopeBuild || sp.Pattern || sp.Empty || sp.Default != "" {
			continue
		}
		if _, ok := given[k]; ok {
			continue
		}
		if missing == "" || k < missing {
			missing = k
		}
	}
	if missing == "" {
		return nil
	}
	ce := cerr.New(cerr.ExitUsage, "config.build_required",
		"%s: %s — обязательный ключ только для сборки: передайте его в cream install (%s=…, %s=- со stdin или --params-file)",
		m.Name, missing, missing, missing)
	ce.Details = map[string]any{"ext": m.Name, "key": missing}
	return ce
}

// withDeps adds the depends (by name, transitively) that neither the state
// nor the request has, each from the source of the extension that needs it
// (§10.2 ruling of Task 5). A dependency that source lacks stays missing:
// the graph refuses with 67 deps.missing. requires are not added. An unknown
// name (a skipped or failed step) counts as present.
func (c *checker) withDeps(entries []checkEntry, w *cerr.Warnings) ([]checkEntry, error) {
	have := map[string]bool{}
	for name := range c.nodes {
		have[name] = true
	}
	for name := range c.unknown {
		have[name] = true
	}
	for _, en := range entries {
		have[en.m.Name] = true
	}
	for i := 0; i < len(entries); i++ {
		en := entries[i]
		for _, d := range en.m.Depends {
			if have[d.Name] {
				continue
			}
			m, err := en.cat.find(d.Name, c.req.Cream, w)
			if err != nil {
				return nil, err
			}
			if m == nil {
				continue
			}
			have[d.Name] = true
			entries = append(entries, checkEntry{m: m, cat: en.cat})
		}
	}
	return entries, nil
}

// decide is the operation on en against the state: install, upgrade (a
// higher version), noop (the same) or 65 version.downgrade.
func (c *checker) decide(en checkEntry) (string, error) {
	prev, ok := c.nodes[en.m.Name]
	if !ok {
		return CheckOpInstall, nil
	}
	switch cmp := semver.Compare(en.m.Version, prev.Manifest.Version); {
	case cmp < 0:
		ce := cerr.New(cerr.ExitData, "version.downgrade",
			"%s: установлена %s, в источнике %s — понижение версии не поддерживается (remove, затем install нужного пина)",
			en.m.Name, prev.Manifest.Version, en.m.Version)
		ce.Details = map[string]any{"ext": en.m.Name, "installed": prev.Manifest.Version.String(),
			"found": en.m.Version.String()}
		return "", ce
	case cmp > 0:
		return CheckOpUpgrade, nil
	}
	return CheckOpNoop, nil
}

// apply resolves entries against the state and, on success, records the
// layers in s and the state. A depends on an unknown name (a skipped or
// failed step) is dropped from the check with check.maybe_installed.
func (c *checker) apply(s *CheckStep, entries []checkEntry, ops map[string]string,
	params map[string]map[string]string, bo buildOpts, w *cerr.Warnings) error {
	requested := map[string]bool{}
	for _, en := range entries {
		requested[en.m.Name] = true
	}
	req := make([]Node, 0, len(entries))
	orig := map[string]checkEntry{}
	for _, en := range entries {
		m := en.m
		var keep []manifest.Dep
		for _, d := range m.Depends {
			_, have := c.nodes[d.Name]
			if n, maybe := c.maybe(d.Name); maybe && !have && !requested[d.Name] {
				w.AddDetails("check.maybe_installed", map[string]any{"ext": m.Name, "dep": d.Name, "step": n},
					"%s: зависимость %s, возможно, %s — проверит гость", m.Name, d.Name, maybeWhere(n))
				continue
			}
			keep = append(keep, d)
		}
		var caps []string
		for _, cp := range m.Requires {
			if n, maybe := c.maybeCapability(); maybe && !c.provided(cp, m.Name, entries) {
				w.AddDetails("check.maybe_installed", map[string]any{"ext": m.Name, "capability": cp, "step": n},
					"%s: способность %s, возможно, %s — проверит гость", m.Name, cp, maybeWhere(n))
				continue
			}
			caps = append(caps, cp)
		}
		if len(keep) != len(m.Depends) || len(caps) != len(m.Requires) {
			cp := *m
			cp.Depends, cp.Requires = keep, caps
			m = &cp
		}
		orig[en.m.Name] = en
		req = append(req, Node{Manifest: m, Source: en.ref()})
	}
	layers, err := Resolve(c.installedNodes(), req, params, c.req.Host, w)
	if err != nil {
		return err
	}
	if err := c.checkBuildRequired(layers, orig, ops, params, requested, bo, w); err != nil {
		return err
	}
	for _, l := range layers {
		en := orig[l.Name]
		s.Layers = append(s.Layers, CheckLayer{Name: l.Name, Version: l.Version, Source: l.Source, Op: ops[l.Name],
			Mode: l.Mode, Deps: l.Deps})
		if l.Mode == ModeNone {
			w.AddDetails("install.build_none", map[string]any{"ext": l.Name},
				"%s: targets.build: none — в фазе build не ставится", l.Name)
			continue
		}
		if cmds := en.m.Platform.Commands; len(cmds) > 0 {
			w.AddDetails("check.commands", map[string]any{"ext": l.Name, "commands": cmds},
				"%s: нужны команды %s — их наличие проверит гость", l.Name, strings.Join(cmds, ", "))
		}
		c.nodes[l.Name] = Node{Manifest: en.m, Installed: true, Source: en.ref()}
		delete(c.unknown, l.Name)
	}
	return nil
}

// upgrade mirrors `cream upgrade <ext>… | --all` in the build phase: each
// installed extension to the version in the source it came from.
func (c *checker) upgrade(s *CheckStep, args []string, w *cerr.Warnings) ([]string, error) {
	f, names, err := parseStepFlags(args, map[string]bool{"phase": true, "accept-risk": true},
		map[string]bool{"all": true, "dry-run": true, "y": true, "yes": true, "fail-fast": true})
	if err != nil {
		return nil, err
	}
	if err := checkPhaseFlag(f); err != nil {
		return nil, err
	}
	all := f["all"] != ""
	switch {
	case all && len(names) > 0:
		return names, cerr.New(cerr.ExitUsage, "usage.args", "upgrade: имена расширений и --all несовместимы")
	case !all && len(names) == 0:
		return nil, cerr.New(cerr.ExitUsage, "usage.args", "upgrade: нужны имена расширений или --all")
	}
	if all {
		for _, nd := range c.installedNodes() {
			names = append(names, nd.Manifest.Name)
		}
	}
	var entries []checkEntry
	ops := map[string]string{}
	for _, name := range names {
		if err := checkExtName(name); err != nil {
			return names, err
		}
		nd, ok := c.nodes[name]
		if !ok {
			if n, maybe := c.maybe(name); maybe {
				w.AddDetails("check.maybe_installed", map[string]any{"ext": name, "step": n},
					"%s, возможно, %s — проверит гость", name, maybeWhere(n))
				return names, errSkip
			}
			ce := cerr.New(cerr.ExitNotFound, "not_found.extension", "расширение %s не установлено", name)
			ce.Details = map[string]any{"ext": name}
			return names, ce
		}
		src := nd.Source.Name
		if src != "" && c.isExternal(src) {
			w.AddDetails("check.external", map[string]any{"ext": name, "source": src},
				"%s: внешний каталог %s — проверит гость", name, src)
			if all {
				continue
			}
			return names, errSkip
		}
		if src == "" || !hasSrcIn(c.req.Catalogs, src) {
			if all {
				w.AddDetails("upgrade.no_source", map[string]any{"ext": name},
					"%s установлено не из --catalog — не обновляется", name)
				continue
			}
			ce := cerr.New(cerr.ExitSource, "source.missing", "%s: источника %q нет среди --catalog", name, src)
			ce.Details = map[string]any{"ext": name, "source": src}
			return names, ce
		}
		en, err := c.lookup(catalog.Ref{Source: src, Name: name}, w)
		if err != nil {
			return names, err
		}
		op, err := c.decide(en)
		if err != nil {
			return names, err
		}
		entries = append(entries, en)
		ops[name] = op
	}
	if len(entries) == 0 {
		return names, nil
	}
	if f["dry-run"] != "" {
		saved, savedUnknown := c.snapshot()
		err := c.apply(s, entries, ops, nil, buildOpts{}, w)
		c.nodes, c.unknown = saved, savedUnknown
		return names, err
	}
	return names, c.apply(s, entries, ops, nil, buildOpts{}, w)
}

func hasSrcIn(cats []*CheckCatalog, name string) bool {
	for _, x := range cats {
		if x.name == name {
			return true
		}
	}
	return false
}

// snapshot copies the state (upgrade --dry-run changes nothing).
func (c *checker) snapshot() (map[string]Node, map[string]int) {
	nodes := make(map[string]Node, len(c.nodes))
	for k, v := range c.nodes {
		nodes[k] = v
	}
	unknown := make(map[string]int, len(c.unknown))
	for k, v := range c.unknown {
		unknown[k] = v
	}
	return nodes, unknown
}

// remove mirrors `cream remove <ext>`: 66 when not installed, 67
// deps.dependents while installed extensions need it (unless --force).
func (c *checker) remove(s *CheckStep, args []string, w *cerr.Warnings) ([]string, error) {
	f, names, err := parseStepFlags(args, map[string]bool{"phase": true},
		map[string]bool{"purge": true, "forget": true, "force": true})
	if err != nil {
		return nil, err
	}
	if err := checkPhaseFlag(f); err != nil {
		return nil, err
	}
	if f["purge"] != "" && f["forget"] != "" {
		return names, cerr.New(cerr.ExitUsage, "usage.flag", "--purge и --forget несовместимы")
	}
	if len(names) != 1 {
		return names, cerr.New(cerr.ExitUsage, "usage.args", "remove: нужно одно имя расширения")
	}
	name := names[0]
	if err := checkExtName(name); err != nil {
		return names, err
	}
	if _, ok := c.nodes[name]; !ok {
		if n, maybe := c.maybe(name); maybe {
			w.AddDetails("check.maybe_installed", map[string]any{"ext": name, "step": n},
				"%s, возможно, %s — проверит гость", name, maybeWhere(n))
			return nil, errSkip
		}
		ce := cerr.New(cerr.ExitNotFound, "not_found.extension", "расширение %s не установлено", name)
		ce.Details = map[string]any{"ext": name}
		return nil, ce
	}
	if deps := Dependents(c.installedNodes(), name); len(deps) > 0 {
		if f["force"] == "" {
			ce := cerr.New(cerr.ExitGraph, "deps.dependents",
				"%s нужно установленным расширениям: %s (--force — удалить всё равно)", name, strings.Join(deps, ", "))
			ce.Details = map[string]any{"ext": name, "dependents": deps}
			return nil, ce
		}
		w.AddDetails("deps.dependents_forced", map[string]any{"ext": name, "dependents": deps},
			"%s удалено с --force, хотя его ждут: %s", name, strings.Join(deps, ", "))
	}
	delete(c.nodes, name)
	s.Removed = append(s.Removed, name)
	return nil, nil
}

// checkExtName is 64 usage.args for a word that is not an extension name.
func checkExtName(name string) error {
	if !manifest.ValidName(name) {
		ce := cerr.New(cerr.ExitUsage, "usage.args", "%q: не имя расширения", name)
		ce.Details = map[string]any{"ext": name}
		return ce
	}
	return nil
}

// checkPhaseFlag: cream check checks the build phase; --phase live is 64.
func checkPhaseFlag(f map[string]string) error {
	if p, ok := f["phase"]; ok && p != PhaseBuild {
		ce := cerr.New(cerr.ExitUsage, "usage.phase", "--phase %s: cream check проверяет только фазу build", p)
		ce.Details = map[string]any{"phase": p}
		return ce
	}
	return nil
}

// parseStepFlags splits a step's arguments into flags and positionals with
// the permutation of the cream dispatcher: -x and --x, --x=v and --x v for
// value flags, "--" ends the flags. A bool flag has the value "1" when set
// ("" for --x=false). Unknown flags and a missing value are 64 usage.flag.
func parseStepFlags(args []string, values, bools map[string]bool) (map[string]string, []string, error) {
	f := map[string]string{}
	var pos []string
	bad := func(format string, a ...any) error {
		return cerr.New(cerr.ExitUsage, "usage.flag", format, a...)
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if len(a) < 2 || a[0] != '-' {
			pos = append(pos, a)
			continue
		}
		name, val, hasVal := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(a, "-"), "-"), "=")
		switch {
		case values[name] || checkGlobalValues[name]:
			if !hasVal {
				if i+1 >= len(args) {
					return nil, nil, bad("флагу -%s нужно значение", name)
				}
				i++
				val = args[i]
			}
			if values[name] {
				f[name] = val
			}
		case bools[name] || checkGlobalBools[name]:
			on := "1"
			if hasVal {
				switch val {
				case "true", "1":
				case "false", "0":
					on = ""
				default:
					return nil, nil, bad("флаг -%s: значение %q — не true/false", name, val)
				}
			}
			if bools[name] {
				f[name] = on
			}
		default:
			return nil, nil, bad("неизвестный флаг %s", a)
		}
	}
	return f, pos, nil
}

// CheckParams checks the KEY=VALUE of one install as the guest's
// `cream install` takes them: KEY=VALUE only (64 usage.assignment), each
// key once (64 usage.duplicate_key) and known (65 config.unknown_key), at
// most one KEY=- from stdin (64 usage.stdin_twice), a secret never valued
// in argv (64 secret.in_argv: KEY=- or KEY= only), a value of its type (65
// config.invalid). The value from stdin is unknown here; secrets and stdin
// values enter the result as "".
func CheckParams(m *manifest.Manifest, args []string) (map[string]string, error) {
	out := make(map[string]string, len(args))
	stdin := ""
	for _, a := range args {
		k, v, ok := strings.Cut(a, "=")
		fail := func(ce *cerr.Error) (map[string]string, error) {
			ce.Details = map[string]any{"ext": m.Name, "key": k}
			return nil, ce
		}
		if !ok || k == "" {
			return nil, cerr.New(cerr.ExitUsage, "usage.assignment", "ожидали KEY=VALUE, получили %q", "=…")
		}
		if _, dup := out[k]; dup {
			return fail(cerr.New(cerr.ExitUsage, "usage.duplicate_key", "%s: ключ %s задан дважды", m.Name, k))
		}
		sp, known := m.Config.Lookup(k)
		if !known {
			return fail(cerr.New(cerr.ExitData, "config.unknown_key", "%s: ключа %s нет в config.keys", m.Name, k))
		}
		switch {
		case v == "-":
			if stdin != "" {
				return fail(cerr.New(cerr.ExitUsage, "usage.stdin_twice",
					"со stdin читается только одно значение (%s=-)", k))
			}
			stdin = k
			out[k] = ""
			continue
		case sp.Secret != "" && v != "":
			return fail(cerr.New(cerr.ExitUsage, "secret.in_argv",
				"%s: %s — секрет: значение в аргументах запрещено, передайте %s=- со stdin или --params-file",
				m.Name, k, k))
		case sp.Secret != "":
			out[k] = ""
			continue
		}
		if err := sp.CheckValue(v); err != nil {
			return fail(cerr.New(cerr.ExitData, "config.invalid", "%s: %s: %v", m.Name, k, err))
		}
		out[k] = v
	}
	return out, nil
}

// splitStep splits a step into words like a POSIX shell does for one simple
// command: blanks separate, '…' is literal, "…" allows \" \\ \$ \` escapes,
// a backslash outside quotes escapes the next character. Anything the
// shell would run or substitute — ; & | < > ( ) $ ` outside single quotes,
// a word starting with # — is 64 usage.step: such a line is not the
// standard form (§7.2) and only the guest can tell what it does.
func splitStep(line string) ([]string, error) {
	bad := func(format string, a ...any) ([]string, error) {
		ce := cerr.New(cerr.ExitUsage, "usage.step", "шаг %q: %s", line, fmt.Sprintf(format, a...))
		ce.Details = map[string]any{"step": line}
		return nil, ce
	}
	var words []string
	var cur strings.Builder
	inWord := false
	rs := []rune(line)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == ' ' || r == '\t' || r == '\n':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		case r == '\'':
			j := i + 1
			for j < len(rs) && rs[j] != '\'' {
				j++
			}
			if j >= len(rs) {
				return bad("незакрытая кавычка '")
			}
			cur.WriteString(string(rs[i+1 : j]))
			i, inWord = j, true
		case r == '"':
			j := i + 1
			for ; j < len(rs) && rs[j] != '"'; j++ {
				switch rs[j] {
				case '\\':
					if j+1 < len(rs) && strings.ContainsRune("\"\\$`", rs[j+1]) {
						j++
					}
					cur.WriteRune(rs[j])
				case '$', '`':
					return bad("подстановка %c — нестандартная форма", rs[j])
				default:
					cur.WriteRune(rs[j])
				}
			}
			if j >= len(rs) {
				return bad("незакрытая кавычка \"")
			}
			i, inWord = j, true
		case r == '\\':
			if i+1 >= len(rs) {
				return bad("\\ в конце строки")
			}
			i++
			cur.WriteRune(rs[i])
			inWord = true
		case strings.ContainsRune(";&|<>()$`", r):
			return bad("%c — нестандартная форма: проверит только гость", r)
		case r == '#' && !inWord:
			return bad("комментарий — нестандартная форма")
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words, nil
}

// CheckHost is the build-phase host of cream check: --os ID:VERSION (""
// — the OS and family are not checked) and --arch (canonicalized). The
// family follows the OS id (debian, ubuntu → deb). A malformed --os or an
// unknown arch is 64 usage.flag.
func CheckHost(osSpec, arch string) (Host, error) {
	h := Host{Phase: PhaseBuild}
	if osSpec != "" {
		id, ver, ok := strings.Cut(osSpec, ":")
		if !ok || !reOSID.MatchString(id) || ver == "" || strings.ContainsAny(ver, " \t\n") {
			return Host{}, cerr.New(cerr.ExitUsage, "usage.flag", "--os %q: ожидается ID:VERSION_ID (debian:12)", osSpec)
		}
		h.OSID, h.OSVersion, h.Family = id, ver, familyOf(id, "")
	}
	if arch != "" {
		a, ok := manifest.CanonArch(arch)
		if !ok {
			return Host{}, cerr.New(cerr.ExitUsage, "usage.flag", "--arch %q: ожидается amd64 или arm64", arch)
		}
		h.TargetArch = a
	}
	return h, nil
}

// reOSID is an os-release ID (os-release(5): lower-case letters, digits, "._-").
var reOSID = regexp.MustCompile(`^[a-z0-9._-]+$`)

// maybe reports a name that may be in the guest though the check has not
// seen it: put by a skipped or failed step n, or in an unknown base (0).
func (c *checker) maybe(name string) (int, bool) {
	if n, ok := c.unknown[name]; ok {
		return n, true
	}
	if c.wild > 0 {
		return c.wild, true
	}
	return 0, c.req.BaseUnknown
}

// maybeCapability: a capability nothing seen provides may still be in the
// guest — after a non-standard step or in an unknown base.
func (c *checker) maybeCapability() (int, bool) {
	if c.wild > 0 {
		return c.wild, true
	}
	return 0, c.req.BaseUnknown
}

func maybeWhere(step int) string {
	if step == 0 {
		return "есть в базовом образе"
	}
	return fmt.Sprintf("поставлено шагом %d", step)
}

// provided reports a provider of capability other than self among the
// installed and the requested.
func (c *checker) provided(capability, self string, entries []checkEntry) bool {
	for name, nd := range c.nodes {
		if name != self && slices.Contains(nd.Manifest.Provides, capability) {
			return true
		}
	}
	for _, en := range entries {
		if en.m.Name != self && slices.Contains(en.m.Provides, capability) {
			return true
		}
	}
	return false
}
