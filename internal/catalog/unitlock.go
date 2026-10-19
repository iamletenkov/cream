package catalog

import (
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/iamletenkov/cream/internal/manifest"
)

// Правило validate.unit_lock (§7.3). Тупик через systemd складывается, когда
// держатель lock/<ext> (хук или действие с lock не none) ждёт задание юнита,
// а Exec* этого юнита ждёт ту же блокировку: юнит не потомок держателя и
// CREAM_LOCK_FD не наследует. Поэтому предупреждение — не на любой вызов
// действия под блокировкой из Exec*, а только при признаке такого ожидания:
//   - в тексте дерева расширения действия синхронный systemctl (без
//     --no-block) start/stop/restart/reload/… или enable/disable --now на этот
//     юнит либо на юнит, упорядоченный после него (After= у того, Before= у
//     этого, с drop-in'ами из files, транзитивно);
//   - юнит упорядочен или связан с cream-configure* (его файл), или configure
//     расширения упорядочен относительно юнита (after/before/wants манифеста):
//     configure держит блокировку домена, пока systemd ведёт задания.
// Анализ консервативный: аргумент systemctl, который не разрешился в имя
// (переменная из for/read, $1, подстановка команды), считается совпавшим.
// Простые присваивания NAME=…, NAME=(…), NAME+=(…) разрешаются по всему
// дереву. Вызов systemctl через переменную ("$SYSTEMCTL start …") не
// распознаётся — это граница эвристики, как у остальных правил валидатора.

// systemctlValueFlags — флаги systemctl со значением отдельным словом.
var systemctlValueFlags = []string{"-p", "--property", "-t", "--type", "-s", "--signal", "--state",
	"-H", "--host", "-M", "--machine", "--job-mode", "--kill-whom", "--kill-who", "--kill-value",
	"-n", "--lines", "-o", "--output", "--root", "--image", "--what", "--when", "--message",
	"--timestamp", "--check-inhibitors", "--drop-in", "--reboot-argument", "--boot-loader-menu",
	"--boot-loader-entry"}

// nowVerbs ставят задание только с --now.
var nowVerbs = []string{"enable", "disable", "reenable", "mask"}

// unitTypes — суффиксы типов юнитов; имя без них systemctl дополняет .service.
var unitTypes = []string{"service", "socket", "device", "mount", "automount", "swap", "target",
	"path", "timer", "slice", "scope"}

var (
	reAssign  = regexp.MustCompile(`^(?:(?:export|readonly|local|declare|typeset)(?:\s+-[A-Za-z]+)*\s+)?([A-Za-z_][A-Za-z0-9_]*)\+?=(.*)$`)
	reDynamic = regexp.MustCompile(`(?:^|[\s;&|(])(?:for\s+([A-Za-z_][A-Za-z0-9_]*)\s|` +
		`(?:read|mapfile|readarray)(?:\s+-[A-Za-z]+(?:\s+\S+)?)*\s+([A-Za-z_][A-Za-z0-9_]*)|` +
		`printf\s+-v\s+([A-Za-z_][A-Za-z0-9_]*)|([A-Za-z_][A-Za-z0-9_]*)\[[^\]]*\]\+?=)`)
	reVarRef = regexp.MustCompile(`\$(?:\{([A-Za-z_][A-Za-z0-9_]*)(?:\[[@*0-9]+\])?\}|([A-Za-z_][A-Za-z0-9_]*))`)
)

// unresolved marks a systemctl argument that did not resolve to a name.
const unresolved = "\x00"

// Пределы анализа: каталог может быть чужим (cream source add), поэтому
// работа ограничена; что не уложилось в предел, считается совпавшим
// (консервативно — предупреждение, а не молчание).
const (
	maxTreeBytes  = 32 << 20 // текста дерева на анализ
	maxTreeLines  = 1 << 20  // логических строк дерева
	maxExpansions = 256      // вариантов разворота одного слова и значений переменной
	maxWordLen    = 1024     // длина аргумента systemctl; имя юнита — до 255
	maxOrderNodes = 4096     // юнитов, упорядоченных после проверяемого
	maxMatchWork  = 1 << 20  // сравнений имён при поиске причины для одного юнита
)

// syncJob is a synchronous systemctl job found in the extension tree.
type syncJob struct {
	where string   // файл:строка
	cmd   string   // команда целиком
	units []string // имена юнитов после разрешения; unresolved — неизвестно
}

// lockScan is what the rule knows about the extension whose action a unit
// calls: its synchronous systemctl jobs and the ordering of the units it and
// the calling extension lay out.
type lockScan struct {
	jobs   []syncJob
	after  map[string][]string // юнит → те, после которых он упорядочен
	unitOf map[string]string   // текст юнита (с drop-in'ами) по имени
	conf   *manifest.Hook      // hooks.configure, если есть
	ext    string
	// truncated — дерево больше пределов, анализ неполон; globOrder — первый
	// юнит с glob в After=/Before= (systemd их не разворачивает, порядок не
	// анализируется).
	truncated, globOrder string
	waiters              map[string]map[string]bool // юнит → кто упорядочен после него (кеш)
}

func newLockScan(x loaded) *lockScan {
	s := &lockScan{after: map[string][]string{}, unitOf: map[string]string{}, ext: x.m.Name,
		waiters: map[string]map[string]bool{}}
	if h, ok := x.m.Hooks[manifest.HookConfigure]; ok {
		s.conf = &h
	}
	s.addUnits(x)
	s.scanTree(x.dir)
	return s
}

// addUnits records the unit files (and drop-ins) x lays out.
func (s *lockScan) addUnits(x loaded) {
	for _, f := range x.m.Files {
		name := path.Base(f.Dst)
		if d := path.Base(path.Dir(f.Dst)); strings.HasSuffix(d, ".d") && isUnitName(strings.TrimSuffix(d, ".d")) {
			name = strings.TrimSuffix(d, ".d")
		} else if !isUnitName(name) {
			continue
		}
		data, err := readHead(filepath.Join(x.dir, filepath.FromSlash(f.Src)), maxScript)
		if err != nil {
			continue
		}
		text := string(data)
		s.unitOf[name] += text + "\n"
		for _, line := range strings.Split(text, "\n") {
			key, val, ok := strings.Cut(strings.TrimSpace(line), "=")
			if !ok {
				continue
			}
			for _, u := range strings.Fields(val) {
				k := strings.TrimSpace(key)
				if (k == "After" || k == "Before") && isGlob(u) {
					if s.globOrder == "" {
						s.globOrder = name + ": " + k + "=" + u
					}
					continue
				}
				switch k {
				case "After":
					s.after[name] = append(s.after[name], unitName(u))
				case "Before":
					s.after[unitName(u)] = append(s.after[unitName(u)], name)
				}
			}
		}
	}
}

// scanTree reads every text file of the tree (scripts, libraries, units;
// not extension.yaml and markdown), resolves simple assignments across the
// tree and collects the synchronous systemctl jobs.
func (s *lockScan) scanTree(dir string) {
	type logical struct {
		where, text string
	}
	var lines []logical
	total := 0
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		if s.truncated != "" {
			return fs.SkipAll
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if rel == manifest.ManifestFile || strings.HasSuffix(rel, ".md") {
			return nil
		}
		data, err := readHead(p, maxScript)
		if err != nil || !utf8.Valid(data) || slices.Contains(data, 0) {
			return nil
		}
		if total += len(data); total > maxTreeBytes {
			s.truncated = "текст дерева больше " + strconv.Itoa(maxTreeBytes>>20) + " МиБ"
			return fs.SkipAll
		}
		raw := strings.Split(string(data), "\n")
		if len(lines)+len(raw) > maxTreeLines {
			s.truncated = "в дереве больше " + strconv.Itoa(maxTreeLines) + " строк"
			return fs.SkipAll
		}
		for i := 0; i < len(raw); i++ {
			start, text := i, raw[i]
			for strings.HasSuffix(text, `\`) && i+1 < len(raw) {
				i++
				text = strings.TrimSuffix(text, `\`) + " " + raw[i]
			}
			lines = append(lines, logical{where: rel + ":" + strconv.Itoa(start+1), text: text})
		}
		return nil
	})
	vars := map[string][]string{}
	dynamic := map[string]bool{}
	for _, l := range lines {
		text := strings.TrimSpace(l.text)
		if strings.HasPrefix(text, "#") {
			continue
		}
		for _, m := range reDynamic.FindAllStringSubmatch(text, -1) {
			for _, name := range m[1:] {
				if name != "" {
					dynamic[name] = true
				}
			}
		}
		for _, stmt := range strings.FieldsFunc(text, func(r rune) bool { return r == ';' || r == '&' || r == '|' }) {
			if m := reAssign.FindStringSubmatch(strings.TrimSpace(stmt)); m != nil {
				vars[m[1]] = append(vars[m[1]], assignedValues(m[2])...)
				if len(vars[m[1]]) > maxExpansions {
					dynamic[m[1]] = true // слишком много значений — неразрешимо
					vars[m[1]] = vars[m[1]][:0]
				}
			}
		}
	}
	for _, l := range lines {
		for _, words := range commands(l.text) {
			if units, ok := syncSystemctl(words); ok {
				var names []string
				for _, w := range units {
					names = append(names, expand(w, vars, dynamic)...)
				}
				i := slices.IndexFunc(words, func(w string) bool { return path.Base(w) == "systemctl" })
				s.jobs = append(s.jobs, syncJob{where: l.where, cmd: strings.Join(words[i:], " "), units: names})
			}
		}
	}
}

// syncSystemctl returns the unit arguments of a systemctl call in words that
// queues a job and waits for it (no --no-block).
func syncSystemctl(words []string) ([]string, bool) {
	i := slices.IndexFunc(words, func(w string) bool { return path.Base(w) == "systemctl" })
	if i < 0 || slices.Contains(words, "--no-block") {
		return nil, false
	}
	j := i + 1
	for j < len(words) && strings.HasPrefix(words[j], "-") {
		if slices.Contains(systemctlValueFlags, words[j]) {
			j++
		}
		j++
	}
	if j >= len(words) {
		return nil, false
	}
	verb := words[j]
	if !slices.Contains(jobVerbs, verb) && (!slices.Contains(nowVerbs, verb) || !slices.Contains(words, "--now")) {
		return nil, false
	}
	var units []string
	for k := j + 1; k < len(words); k++ {
		w := words[k]
		switch {
		case slices.Contains(systemctlValueFlags, w):
			k++
		case strings.HasPrefix(w, "-"), strings.ContainsAny(w, "<>"):
		default:
			units = append(units, w)
		}
	}
	return units, true
}

// assignedValues splits the right side of an assignment into values: an
// array (a b c) into its words, otherwise the first shell word.
func assignedValues(v string) []string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "(") {
		body, _, _ := strings.Cut(v[1:], ")")
		return strings.Fields(strings.NewReplacer(`"`, "", `'`, "").Replace(body))
	}
	switch {
	case strings.HasPrefix(v, `"`):
		body, _, _ := strings.Cut(v[1:], `"`)
		return []string{body}
	case strings.HasPrefix(v, `'`):
		body, _, _ := strings.Cut(v[1:], `'`)
		return []string{body}
	}
	if i := strings.IndexAny(v, " \t;"); i >= 0 {
		v = v[:i]
	}
	return []string{v}
}

// expand substitutes the variables of w by every value assigned in the tree;
// a variable assigned dynamically, unknown or with a value that is not a
// literal gives unresolved, and so does a word longer than maxWordLen or
// with more than maxExpansions variants.
func expand(w string, vars map[string][]string, dynamic map[string]bool) []string {
	if len(w) > maxWordLen {
		return []string{unresolved}
	}
	out := []string{""}
	for w != "" {
		i := strings.IndexByte(w, '$')
		if i < 0 {
			for k := range out {
				out[k] += w
			}
			break
		}
		m := reVarRef.FindStringSubmatchIndex(w[i:])
		if m == nil || m[0] != 0 {
			return []string{unresolved}
		}
		var name string
		if m[2] >= 0 {
			name = w[i+m[2] : i+m[3]] // ${NAME}
		} else {
			name = w[i+m[4] : i+m[5]] // $NAME
		}
		vals, ok := vars[name]
		if !ok || dynamic[name] || len(vals) == 0 || len(out)*len(vals) > maxExpansions {
			return []string{unresolved}
		}
		next := make([]string, 0, len(out)*len(vals))
		for _, prefix := range out {
			for _, v := range vals {
				if strings.Contains(v, "$") {
					return []string{unresolved}
				}
				next = append(next, prefix+w[:i]+v)
			}
		}
		out, w = next, w[i+m[1]:]
	}
	return out
}

// waitsFor reports whether a synchronous job on target waits for a job of
// unit: target is unit, is ordered after it, or matches such a unit as a
// glob. ok is false when the units ordered after unit exceed maxOrderNodes
// (the caller then warns conservatively).
func (s *lockScan) waitsFor(target, unit string) (wait, ok bool) {
	if target == unresolved {
		return true, true
	}
	w, ok := s.waitersOf(unit)
	if !ok {
		return false, false
	}
	if !isGlob(target) {
		return w[unitName(target)], true
	}
	for cand := range w {
		if m, _ := path.Match(target, cand); m {
			return true, true
		}
	}
	return false, true
}

// waitersOf returns unit and every unit ordered after it (transitively), by
// a breadth-first walk over the reversed After= edges; ok is false past
// maxOrderNodes.
func (s *lockScan) waitersOf(unit string) (map[string]bool, bool) {
	if w, ok := s.waiters[unit]; ok {
		return w, w != nil
	}
	before := map[string][]string{} // юнит → кто упорядочен после него
	for u, deps := range s.after {
		for _, d := range deps {
			before[d] = append(before[d], u)
		}
	}
	w := map[string]bool{unit: true}
	queue := []string{unit}
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		for _, next := range before[u] {
			if w[next] {
				continue
			}
			if len(w) >= maxOrderNodes {
				s.waiters[unit] = nil
				return nil, false
			}
			w[next] = true
			queue = append(queue, next)
		}
	}
	s.waiters[unit] = w
	return w, true
}

// evidence explains why a holder of the lock can wait for a job of unit;
// "" when nothing in the tree points to it.
func (s *lockScan) evidence(unit string) string {
	if s.truncated != "" {
		return "анализ неполон: " + s.truncated
	}
	work := 0
	for _, j := range s.jobs {
		for _, u := range j.units {
			if work += 1 + len(s.waiters[unit]); work > maxMatchWork {
				return "анализ неполон: больше " + strconv.Itoa(maxMatchWork) + " сравнений имён юнитов"
			}
			wait, ok := s.waitsFor(u, unit)
			switch {
			case !ok:
				return "после " + unit + " упорядочено больше " + strconv.Itoa(maxOrderNodes) + " юнитов"
			case wait:
				return j.where + ": `" + j.cmd + "` без --no-block"
			case s.globOrder != "":
				return j.where + ": `" + j.cmd + "` без --no-block, а порядок с glob (" + s.globOrder +
					") не анализируется"
			}
		}
	}
	for _, line := range strings.Split(s.unitOf[unit], "\n") {
		key, val, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && !strings.HasPrefix(strings.TrimSpace(line), "#") && strings.Contains(val, "cream-configure") &&
			slices.Contains([]string{"After", "Before", "Requires", "Wants", "BindsTo", "Requisite", "PartOf",
				"Upholds", "WantedBy", "RequiredBy"}, strings.TrimSpace(key)) {
			return unit + " упорядочен относительно cream-configure (" + strings.TrimSpace(line) + ")"
		}
	}
	if s.conf != nil {
		for _, list := range [][]string{s.conf.After, s.conf.Before, s.conf.Wants} {
			if slices.ContainsFunc(list, func(u string) bool { return unitName(u) == unit }) {
				return "configure расширения " + s.ext + " (cream-configure@" + s.ext +
					") упорядочен относительно " + unit
			}
		}
	}
	return ""
}

// warnUnitLock: an action called from Exec* of a unit the extension lays
// out under a lock is a deadlock when a holder of that lock can wait for the
// unit's job (§7.3). Actions of extensions outside idx are not checked.
func (t *tree) warnUnitLock(x loaded, idx map[string]loaded) {
	scans := map[string]*lockScan{}
	for _, f := range x.m.Files {
		if !isServiceUnit(f.Dst) {
			continue
		}
		unit := path.Base(f.Dst)
		if d := path.Dir(f.Dst); strings.HasSuffix(d, ".service.d") {
			unit = strings.TrimSuffix(path.Base(d), ".d")
		}
		data, err := readHead(t.path(f.Src), maxScript)
		if err != nil {
			continue
		}
		for n, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			key, val, ok := strings.Cut(line, "=")
			if !ok || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") ||
				!slices.Contains(unitExecKeys, strings.TrimSpace(key)) {
				continue
			}
			sub, rest, ok := creamCall(strings.Fields(val))
			if !ok || sub != "run" {
				continue
			}
			j := skipFlags(rest, 0)
			if j+1 >= len(rest) {
				continue
			}
			ext, action := rest[j], rest[j+1]
			target, ok := idx[ext]
			if !ok {
				continue
			}
			a, ok := target.m.Actions[action]
			if !ok || a.Lock == manifest.LockNone {
				continue
			}
			s := scans[ext]
			if s == nil {
				s = newLockScan(target)
				if ext != x.m.Name {
					s.addUnits(x)
				}
				scans[ext] = s
			}
			why := s.evidence(unit)
			if why == "" {
				continue
			}
			t.r.warn(warnUnitLock, "%s: %s:%d: %s вызывает cream run %s %s с lock: %s, а держатель lock/%s может "+
				"ждать задание %s: %s — тупик (§7.3); действие из Exec* такого юнита объявляется lock: none",
				t.ext, path.Join(t.rel, f.Src), n+1, strings.TrimSpace(key), ext, action, a.Lock, ext, unit, why)
		}
	}
}

// isUnitName: name ends with a known unit type.
func isUnitName(name string) bool {
	i := strings.LastIndexByte(name, '.')
	return i > 0 && slices.Contains(unitTypes, name[i+1:])
}

func isGlob(w string) bool { return strings.ContainsAny(w, "*?[") }

// unitName completes a name without a unit type with .service, as systemctl.
func unitName(w string) string {
	if isUnitName(w) || isGlob(w) {
		return w
	}
	return w + ".service"
}
