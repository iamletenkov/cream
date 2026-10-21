package manifest

import (
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/keytype"
	"github.com/iamletenkov/cream/internal/semver"
)

var (
	// reName — имя расширения, способности, каталога и ID ОС (§4.2).
	reName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	// reAction — имя действия (§7.1).
	reAction = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	// reKey — имя ключа настроек и параметра действия: имя переменной окружения;
	// шаблон — то же с хвостом "_*".
	reKey     = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`)
	rePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,126}_\*$`)
	// reUnit — имя юнита systemd в after/before/wants: попадает в drop-in, поэтому
	// без пробелов и переводов строк, с суффиксом типа.
	reUnit = regexp.MustCompile(`^[A-Za-z0-9:_.\\@-]+\.(service|socket|target|mount|automount|path|timer|device|swap|slice|scope)$`)
	// reCommand — имя команды из PATH.
	reCommand = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,127}$`)
	// reMode — восьмеричный режим файла.
	reMode = regexp.MustCompile(`^0?[0-7]{3}$`)
)

// reservedNames — имена, занятые раскладкой cream (§4.2, §5) и lock/global (Р15).
var reservedNames = []string{"cream", "lib", "ext", "installed", "lock", "global"}

// reservedKeys — переменные окружения хука, которые ключ домена не может занять (§6.3).
// Загрузчик и оболочки читают LD_PRELOAD, LD_LIBRARY_PATH, BASH_ENV, ENV, IFS
// до кода хука — ключ домена с таким именем исполнил бы чужой код.
var reservedKeys = []string{"PATH", "LANG", "HOME", "PWD", "LD_PRELOAD", "LD_LIBRARY_PATH", "BASH_ENV", "ENV", "IFS"}

// reservedName сообщает, занято ли имя ключа домена или параметра действия
// окружением хука и действия (CREAM_*, reservedKeys, рулинг A10).
func reservedName(name string) bool {
	return strings.HasPrefix(name, "CREAM_") || slices.Contains(reservedKeys, name)
}

// ValidKey сообщает, годится ли s в имена ключей домена и параметров
// действий: [A-Z][A-Z0-9_]*, не занятое окружением хука (CREAM_*,
// reservedKeys). Тот же список держит runner, пропуская ключи в окружение.
func ValidKey(s string) bool {
	return reKey.MatchString(s) && !reservedName(s)
}

// ValidName сообщает, годится ли s в имена расширений: регэксп §4.2, кроме
// зарезервированных имён и префикса "cream-" (рулинг Р15).
func ValidName(s string) bool {
	return reName.MatchString(s) && !slices.Contains(reservedNames, s) && !strings.HasPrefix(s, "cream-")
}

// CanonArch приводит имя архитектуры к виду Debian: x86_64 → amd64,
// aarch64 → arm64. ok = false для архитектур, которых cream не знает.
func CanonArch(s string) (string, bool) {
	switch s {
	case "amd64", "x86_64":
		return "amd64", true
	case "arm64", "aarch64":
		return "arm64", true
	}
	return "", false
}

func invalid(field, format string, a ...any) *cerr.Error {
	e := cerr.New(cerr.ExitData, codeInvalid, "%s: %s", field, fmt.Sprintf(format, a...))
	e.Details = map[string]any{"field": field}
	return e
}

func reserved(field string) *cerr.Error {
	e := cerr.New(cerr.ExitData, codeReserved, "%s: %s", field, reservedText)
	e.Details = map[string]any{"field": field}
	return e
}

func validate(r *rawManifest) (*Manifest, error) {
	m := &Manifest{Schema: SchemaV1}
	if !ValidName(r.Name) {
		return nil, invalid("name", "%q не подходит: [a-z0-9][a-z0-9._-]{0,63}, кроме %s и префикса cream-",
			r.Name, strings.Join(reservedNames, ", "))
	}
	m.Name = r.Name
	if r.Version == "" {
		return nil, invalid("version", "обязательное поле")
	}
	v, err := semver.Parse(r.Version)
	if err != nil {
		return nil, invalid("version", "%v", err)
	}
	m.Version = v
	if err := checkSummary("summary", r.Summary, true); err != nil {
		return nil, err
	}
	m.Summary = r.Summary

	switch Kind(r.Kind) {
	case "", KindExtension:
		m.Kind = KindExtension
	case KindLibrary:
		m.Kind = KindLibrary
	default:
		return nil, invalid("kind", "%q: допустимо extension или library", r.Kind)
	}

	if m.Platform, err = validatePlatform(r.Platform); err != nil {
		return nil, err
	}
	if m.Targets, err = validateTargets(r.Targets); err != nil {
		return nil, err
	}
	if m.Provides, err = tokens("provides", r.Provides); err != nil {
		return nil, err
	}
	if m.Requires, err = tokens("requires", r.Requires); err != nil {
		return nil, err
	}
	if m.Depends, err = validateDepends(m.Name, r.Depends); err != nil {
		return nil, err
	}
	if m.Conflicts, err = validateConflicts(m.Name, r.Conflicts); err != nil {
		return nil, err
	}
	if m.Hooks, err = validateHooks(m.Kind, r.Hooks); err != nil {
		return nil, err
	}
	if m.Config, err = validateKeys(r.Config.Keys); err != nil {
		return nil, err
	}
	if m.Actions, err = validateActions(r.Actions, m.Config); err != nil {
		return nil, err
	}
	if m.Files, err = validateFiles(r.Files); err != nil {
		return nil, err
	}
	return m, nil
}

func checkSummary(field, s string, required bool) error {
	switch {
	case s == "" && required:
		return invalid(field, "обязательное поле")
	case utf8.RuneCountInString(s) > 120:
		return invalid(field, "длиннее 120 знаков")
	case strings.ContainsAny(s, "\r\n"):
		return invalid(field, "должно быть одной строкой")
	}
	return nil
}

func validatePlatform(r *rawPlatform) (Platform, error) {
	if r == nil {
		return Platform{}, invalid("platform", "обязательное поле")
	}
	p := Platform{Family: r.Family}
	if r.Family != "deb" {
		return p, invalid("platform.family", "%q: cream/v1 знает только deb", r.Family)
	}
	if len(r.Arch) == 0 {
		return p, invalid("platform.arch", "непустой список из amd64, arm64")
	}
	for i, a := range r.Arch {
		c, ok := CanonArch(a)
		if !ok {
			return p, invalid(fmt.Sprintf("platform.arch[%d]", i), "%q: допустимо amd64, arm64 (x86_64, aarch64)", a)
		}
		if slices.Contains(p.Arch, c) {
			return p, invalid(fmt.Sprintf("platform.arch[%d]", i), "%s повторяется", c)
		}
		p.Arch = append(p.Arch, c)
	}
	for i, o := range r.OS {
		field := fmt.Sprintf("platform.os[%d]", i)
		if !reName.MatchString(o.ID) {
			return p, invalid(field+".id", "%q: ожидается ID из os-release (debian, ubuntu)", o.ID)
		}
		req := OSReq{ID: o.ID}
		if o.Version != nil {
			rg, err := semver.ParseRange(*o.Version)
			if err != nil {
				return p, invalid(field+".version", "%v", err)
			}
			req.Version = &rg
		}
		p.OS = append(p.OS, req)
	}
	for i, c := range r.Commands {
		if !reCommand.MatchString(c) {
			return p, invalid(fmt.Sprintf("platform.commands[%d]", i), "%q: ожидается имя команды без пути", c)
		}
	}
	p.Commands = r.Commands
	return p, nil
}

func validateTargets(r rawTargets) (Targets, error) {
	t := Targets{LiveNote: r.LiveNote}
	switch BuildMode(r.Build) {
	case "", BuildExec:
		t.Build = BuildExec
	case BuildNone:
		t.Build = BuildNone
	case "files":
		return t, reserved("targets.build: files")
	default:
		return t, invalid("targets.build", "%q: допустимо exec или none", r.Build)
	}
	switch LiveMode(r.Live) {
	case "", LiveYes:
		t.Live = LiveYes
	case LiveConfirm, LiveNo:
		t.Live = LiveMode(r.Live)
		if strings.TrimSpace(r.LiveNote) == "" {
			return t, invalid("targets.live_note", "обязательно при live: %s", r.Live)
		}
	default:
		return t, invalid("targets.live", "%q: допустимо yes, confirm, no", r.Live)
	}
	return t, nil
}

func tokens(field string, in []string) ([]string, error) {
	for i, s := range in {
		if !reName.MatchString(s) {
			return nil, invalid(fmt.Sprintf("%s[%d]", field, i), "%q: ожидается [a-z0-9][a-z0-9._-]{0,63}", s)
		}
		if slices.Contains(in[:i], s) {
			return nil, invalid(fmt.Sprintf("%s[%d]", field, i), "%q повторяется", s)
		}
	}
	return in, nil
}

func validateDepends(self string, in []rawDep) ([]Dep, error) {
	var out []Dep
	for i, d := range in {
		field := fmt.Sprintf("depends[%d]", i)
		if !ValidName(d.Name) {
			return nil, invalid(field+".name", "%q: не имя расширения", d.Name)
		}
		if d.Name == self {
			return nil, invalid(field+".name", "расширение не может зависеть от себя")
		}
		if slices.ContainsFunc(out, func(x Dep) bool { return x.Name == d.Name }) {
			return nil, invalid(field+".name", "%q повторяется", d.Name)
		}
		dep := Dep{Name: d.Name}
		if d.Version != nil {
			rg, err := semver.ParseRange(*d.Version)
			if err != nil {
				return nil, invalid(field+".version", "%v", err)
			}
			dep.Version = &rg
		}
		out = append(out, dep)
	}
	return out, nil
}

func validateConflicts(self string, r rawConflicts) (Conflicts, error) {
	for i, n := range r.Extensions {
		field := fmt.Sprintf("conflicts.extensions[%d]", i)
		if !ValidName(n) {
			return Conflicts{}, invalid(field, "%q: не имя расширения", n)
		}
		if n == self {
			return Conflicts{}, invalid(field, "расширение не может конфликтовать с собой")
		}
	}
	if _, err := tokens("conflicts.extensions", r.Extensions); err != nil {
		return Conflicts{}, err
	}
	caps, err := tokens("conflicts.capabilities", r.Capabilities)
	if err != nil {
		return Conflicts{}, err
	}
	return Conflicts{Extensions: r.Extensions, Capabilities: caps}, nil
}

func validateHooks(kind Kind, in map[string]rawHook) (map[HookName]Hook, error) {
	if kind == KindLibrary {
		if len(in) > 0 {
			return nil, invalid("hooks", "у kind: library хуков нет (§4.5)")
		}
		return nil, nil
	}
	out := map[HookName]Hook{}
	for _, name := range sortedNames(in) {
		r := in[name]
		field := "hooks." + name
		h := HookName(name)
		switch h {
		case HookInstall, HookUpgrade, HookConfigure, HookApply, HookRemove:
		case "secret", "check":
			return nil, reserved(field)
		default:
			return nil, invalid(field, "неизвестный хук (install, upgrade, configure, apply, remove)")
		}
		var hk Hook
		if r.Timeout != "" {
			d, err := parseTimeout(field+".timeout", r.Timeout)
			if err != nil {
				return nil, err
			}
			hk.Timeout = d
		}
		if h != HookConfigure {
			if r.Stage != "" || r.After != nil || r.Before != nil || r.Wants != nil || r.EveryBoot != nil {
				return nil, invalid(field, "stage, after, before, wants, every_boot — только у configure")
			}
		} else {
			switch st := Stage(r.Stage); st {
			case StageSystem, StageEarly, StageCloudUser, StageGraphical:
				hk.Stage = st
			case "":
				return nil, invalid(field+".stage", "обязательно у configure (early, system, cloud-user, graphical)")
			default:
				return nil, invalid(field+".stage", "%q: неизвестная стадия", r.Stage)
			}
			for _, l := range []struct {
				name string
				in   []string
				out  *[]string
			}{{"after", r.After, &hk.After}, {"before", r.Before, &hk.Before}, {"wants", r.Wants, &hk.Wants}} {
				for i, u := range l.in {
					if !reUnit.MatchString(u) {
						return nil, invalid(fmt.Sprintf("%s.%s[%d]", field, l.name, i), "%q: не имя юнита systemd", u)
					}
				}
				*l.out = l.in
			}
			hk.EveryBoot = r.EveryBoot != nil && *r.EveryBoot
		}
		out[h] = hk
	}
	if _, ok := out[HookInstall]; !ok {
		return nil, invalid("hooks.install", "обязателен у kind: extension")
	}
	// Таймауты по умолчанию §6.1.
	for h, hk := range out {
		if hk.Timeout != 0 {
			continue
		}
		switch h {
		case HookUpgrade:
			hk.Timeout = out[HookInstall].Timeout
		case HookConfigure:
			hk.Timeout = DefaultConfigureTimeout
			if hk.Stage == StageEarly {
				hk.Timeout = MaxEarlyConfigureTimeout
			}
		case HookApply:
			hk.Timeout = DefaultApplyTimeout
		case HookRemove:
			hk.Timeout = DefaultRemoveTimeout
		}
		out[h] = hk
	}
	return out, nil
}

func parseTimeout(field, s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, invalid(field, "%q: ожидается длительность вида 30s, 5m, 1h", s)
	}
	if d <= 0 {
		return 0, invalid(field, "%q: должен быть больше нуля", s)
	}
	return d, nil
}

func validateKeys(in map[string]rawKey) (keytype.Schema, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := keytype.Schema{}
	for _, name := range sortedNames(in) {
		r := in[name]
		field := "config.keys." + name
		isPattern := strings.HasSuffix(name, "*")
		switch {
		case isPattern && !rePattern.MatchString(name):
			return nil, invalid(field, "шаблон ключа — PREFIX_* из A-Z, 0-9, _")
		case !isPattern && !reKey.MatchString(name):
			return nil, invalid(field, "имя ключа — [A-Z][A-Z0-9_]*")
		case reservedName(name):
			return nil, invalid(field, "имя занято окружением хука (CREAM_*, %s)", strings.Join(reservedKeys, ", "))
		case isPattern != r.Pattern:
			return nil, invalid(field, "pattern: true ставится ровно у ключей вида PREFIX_*")
		}
		if r.Type == "" {
			return nil, invalid(field+".type", "обязательное поле")
		}
		typ, err := keytype.ParseType(r.Type)
		if err != nil {
			return nil, invalid(field+".type", "%v", err)
		}
		if strings.TrimSpace(r.Description) == "" {
			return nil, invalid(field+".description", "обязательное поле")
		}
		sp := keytype.Spec{Type: typ, Empty: r.Empty, Readonly: r.Readonly, Description: r.Description, Pattern: r.Pattern}
		switch keytype.SecretMode(r.Secret) {
		case keytype.SecretNone, keytype.SecretFile:
			sp.Secret = keytype.SecretMode(r.Secret)
		case keytype.SecretHook:
			return nil, reserved(field + ".secret: hook")
		default:
			return nil, invalid(field+".secret", "%q: допустимо file", r.Secret)
		}
		switch keytype.Scope(r.Scope) {
		case "", keytype.ScopeConfig:
			sp.Scope = keytype.ScopeConfig
		case keytype.ScopeInstall, keytype.ScopeBuild:
			sp.Scope = keytype.Scope(r.Scope)
		default:
			return nil, invalid(field+".scope", "%q: допустимо config, install или build", r.Scope)
		}
		if r.Default != nil {
			sp.Default = *r.Default
		}
		if r.Pattern {
			if sp.Default != "" {
				return nil, invalid(field+".default", "у шаблона умолчания нет")
			}
		} else if err := sp.CheckValue(sp.Default); err != nil {
			hint := ""
			if typ.Kind == keytype.KindBool {
				hint = " (bool в cream — строка 0 или 1, не true/false)"
			}
			return nil, invalid(field+".default", "%q: %v%s", sp.Default, err, hint)
		}
		out[name] = sp
	}
	return out, nil
}

func validateActions(in map[string]rawAction, keys keytype.Schema) (map[string]Action, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := map[string]Action{}
	for _, name := range sortedNames(in) {
		r := in[name]
		field := "actions." + name
		if !reAction.MatchString(name) {
			return nil, invalid(field, "имя действия — [a-z0-9][a-z0-9-]{0,63}")
		}
		if err := checkSummary(field+".summary", r.Summary, false); err != nil {
			return nil, err
		}
		a := Action{Summary: r.Summary, Timeout: DefaultActionTimeout, Root: true}
		var err error
		if a.Args, err = oneOf(field+".args", r.Args, ArgsNone, ArgsPairs, ArgsFree); err != nil {
			return nil, err
		}
		if a.Stdin, err = oneOf(field+".stdin", r.Stdin, StdinNone, StdinSecret, StdinPass); err != nil {
			return nil, err
		}
		if a.Lock, err = oneOf(field+".lock", r.Lock, LockExclusive, LockShared, LockNone); err != nil {
			return nil, err
		}
		if a.Output, err = oneOf(field+".output", r.Output, OutputText, OutputJSON); err != nil {
			return nil, err
		}
		if r.Timeout != "" {
			if a.Timeout, err = parseTimeout(field+".timeout", r.Timeout); err != nil {
				return nil, err
			}
		}
		if r.Root != nil {
			a.Root = *r.Root
		}
		if r.Phases == nil {
			a.Phases = []Phase{PhaseLive}
		} else {
			if len(r.Phases) == 0 {
				return nil, invalid(field+".phases", "пустой список (умолчание — [live])")
			}
			for i, p := range r.Phases {
				ph, err := oneOf(fmt.Sprintf("%s.phases[%d]", field, i), p, PhaseBuild, PhaseLive)
				if err != nil {
					return nil, err
				}
				if slices.Contains(a.Phases, ph) {
					return nil, invalid(fmt.Sprintf("%s.phases[%d]", field, i), "%s повторяется", ph)
				}
				a.Phases = append(a.Phases, ph)
			}
		}
		if len(r.Params) > 0 && a.Args != ArgsPairs {
			return nil, invalid(field+".params", "params допустимы только при args: pairs")
		}
		hasSecret := false
		for _, pname := range sortedNames(r.Params) {
			p, err := validateParam(field+".params."+pname, pname, r.Params[pname], keys)
			if err != nil {
				return nil, err
			}
			if a.Params == nil {
				a.Params = map[string]Param{}
			}
			a.Params[pname] = p
			if p.Secret && hasSecret {
				return nil, invalid(field+".params."+pname, "не более одного секретного параметра: stdin несёт одно значение")
			}
			hasSecret = hasSecret || p.Secret
		}
		if a.Stdin == StdinSecret && !hasSecret {
			return nil, invalid(field+".stdin", "stdin: secret требует секретного параметра в params")
		}
		if hasSecret && a.Stdin != StdinSecret {
			return nil, invalid(field+".stdin", "секретный параметр передаётся только через stdin: secret (KEY=-)")
		}
		out[name] = a
	}
	return out, nil
}

func validateParam(field, name string, r rawParam, keys keytype.Schema) (Param, error) {
	if !reKey.MatchString(name) {
		return Param{}, invalid(field, "имя параметра — [A-Z][A-Z0-9_]*")
	}
	if reservedName(name) {
		return Param{}, invalid(field, "имя занято окружением действия (CREAM_*, %s)", strings.Join(reservedKeys, ", "))
	}
	p := Param{Required: r.Required, Secret: r.Secret}
	spec, inKeys := keys.Lookup(name)
	switch {
	case r.Type != "":
		t, err := keytype.ParseType(r.Type)
		if err != nil {
			return Param{}, invalid(field+".type", "%v", err)
		}
		if inKeys && t.Raw != spec.Type.Raw {
			return Param{}, invalid(field+".type", "%q расходится с типом ключа config.keys (%s)", r.Type, spec.Type.Raw)
		}
		p.Type = &t
	case inKeys:
		t := spec.Type
		p.Type = &t
	default:
		return Param{}, invalid(field+".type", "нет ни type, ни ключа %s в config.keys", name)
	}
	if inKeys && spec.Secret != keytype.SecretNone {
		p.Secret = true
	}
	return p, nil
}

// oneOf проверяет значение по перечню; пустое — первое значение (умолчание).
func oneOf[T ~string](field, s string, allowed ...T) (T, error) {
	if s == "" {
		return allowed[0], nil
	}
	for _, a := range allowed {
		if string(a) == s {
			return a, nil
		}
	}
	names := make([]string, len(allowed))
	for i, a := range allowed {
		names[i] = string(a)
	}
	return "", invalid(field, "%q: допустимо %s", s, strings.Join(names, ", "))
}

// creamOwned — пути cream, куда files раскладывать нельзя (§5).
var creamOwned = []string{"/opt/cream", "/var/lib/cream", "/var/cache/cream", "/usr/lib/cream", "/run/cream"}

// creamBinaries — бинарь cream; /bin и /sbin при usrmerge — те же каталоги.
var creamBinaries = []string{"/usr/bin/cream", "/usr/sbin/cream", "/bin/cream", "/sbin/cream"}

func validateFiles(in []rawFile) ([]File, error) {
	var out []File
	for i, r := range in {
		field := fmt.Sprintf("files[%d]", i)
		if r.Src == "" || path.IsAbs(r.Src) || path.Clean(r.Src) != r.Src || r.Src == "." ||
			r.Src == ".." || strings.HasPrefix(r.Src, "../") {
			return nil, invalid(field+".src", "%q: ожидается чистый относительный путь внутри дерева расширения", r.Src)
		}
		if !path.IsAbs(r.Dst) || path.Clean(r.Dst) != r.Dst || r.Dst == "/" || strings.ContainsAny(r.Dst, "\x00\n") {
			return nil, invalid(field+".dst", "%q: ожидается чистый абсолютный путь файла", r.Dst)
		}
		if slices.ContainsFunc(strings.Split(r.Dst, "/"), func(c string) bool { return strings.HasPrefix(c, "cream-") }) {
			return nil, invalid(field+".dst", "%q: префикс cream- в компонентах пути зарезервирован за cream (§4.1)", r.Dst)
		}
		if err := checkDstNotCream(field+".dst", r.Dst); err != nil {
			return nil, err
		}
		if slices.ContainsFunc(out, func(f File) bool { return f.Dst == r.Dst }) {
			return nil, invalid(field+".dst", "%q повторяется", r.Dst)
		}
		f := File{Src: r.Src, Dst: r.Dst}
		if r.Mode != "" {
			if !reMode.MatchString(r.Mode) {
				return nil, invalid(field+".mode", "%q: ожидается восьмеричный режим вида \"0644\"", r.Mode)
			}
			n, _ := strconv.ParseUint(r.Mode, 8, 32) // формат проверен reMode
			if n&0o002 != 0 {
				return nil, invalid(field+".mode", "%q: запись для всех запрещена", r.Mode)
			}
			f.Mode = fs.FileMode(n)
		}
		out = append(out, f)
	}
	return out, nil
}

// checkDstNotCream отвергает dst в каталогах cream и файлы домена настроек
// (/etc/cream/sources.yaml, /etc/cream/<ext>/config|secrets). Прочие пути в
// /etc/cream/<любое расширение>/ допустимы (рулинг Р11).
func checkDstNotCream(field, dst string) error {
	if slices.Contains(creamBinaries, dst) {
		return invalid(field, "%q: бинарь cream перезаписывать нельзя", dst)
	}
	for _, p := range creamOwned {
		if dst == p || strings.HasPrefix(dst, p+"/") {
			return invalid(field, "%q: каталог %s принадлежит cream", dst, p)
		}
	}
	if rest, ok := strings.CutPrefix(dst, "/etc/cream/"); ok {
		parts := strings.Split(rest, "/")
		if len(parts) == 1 || (len(parts) == 2 && (parts[1] == "config" || parts[1] == "secrets")) {
			return invalid(field, "%q: файлы cream в /etc/cream раскладывать нельзя", dst)
		}
	}
	return nil
}

func sortedNames[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
