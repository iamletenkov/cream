package catalog

import (
	"bytes"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/iamletenkov/cream/internal/manifest"
)

// Правило validate.unit_lock (§7.3). Тупик через systemd складывается, когда
// держатель lock/<ext> (хук или действие с lock не none) ждёт задание юнита,
// а Exec* этого юнита ждёт ту же блокировку: юнит не потомок держателя и
// CREAM_LOCK_FD не наследует. Поэтому предупреждение — не на любой вызов
// действия под блокировкой из Exec*, а только при признаке такого ожидания:
//   - в тексте дерева расширения действия синхронный вызов (systemctl или
//     deb-systemd-invoke без --no-block: start/stop/restart/reload/… или
//     enable/disable/reenable/mask --now; service и invoke-rc.d) на этот
//     юнит либо на юнит, упорядоченный после него: After= у того, Before= у
//     этого, неявный After= цели на юнит из WantedBy=/RequiredBy= и из
//     Wants=/Requires= файла цели; с drop-in'ами из files, транзитивно;
//   - юнит упорядочен или связан с cream-configure* (его файл), или configure
//     расширения упорядочен относительно юнита (after/before/wants манифеста):
//     configure держит блокировку домена, пока systemd ведёт задания.
//
// Анализ консервативный: совпавшим считается аргумент или подкоманда, которые
// не разрешились (переменная из for/read/mapfile/printf -v, $1, "$@" обёртки
// sc(){ systemctl "$@"; }, подстановка команды, вариант длиннее имени юнита),
// и вызов без имени юнита (xargs systemctl restart). Простые присваивания
// NAME=…, NAME=(…), NAME+=(…) разрешаются по всему дереву. Текст берётся
// байтами в любой кодировке; двоичный файл (с NUL), где встречается
// systemctl/deb-systemd-invoke/invoke-rc.d, не разбирается — анализ неполон.
//
// Не распознаётся (граница эвристики, §7.3 спеки): вызов через переменную
// или путь из подстановки ("$SYSTEMCTL start …", "$(command -v systemctl)"
// start …), вызов из другого языка (subprocess Python, D-Bus), юниты и цели,
// которых нет в files расширений (их порядок неизвестен), неявные
// зависимости systemd, кроме целей из WantedBy=/RequiredBy=.
//
// Работа ограничена пределами unitLockLimits — общими на весь анализ
// каталога, потому что каталог может быть чужим (cream source add). Что не
// уложилось в предел, даёт предупреждение «анализ неполон», а не молчание.

// systemctlValueFlags — флаги systemctl со значением отдельным словом.
var systemctlValueFlags = []string{"-p", "--property", "-t", "--type", "-s", "--signal", "--state",
	"-H", "--host", "-M", "--machine", "--job-mode", "--kill-whom", "--kill-who", "--kill-value",
	"-n", "--lines", "-o", "--output", "--root", "--image", "--what", "--when", "--message",
	"--timestamp", "--check-inhibitors", "--drop-in", "--reboot-argument", "--boot-loader-menu",
	"--boot-loader-entry"}

// nowVerbs ставят задание только с --now.
var nowVerbs = []string{"enable", "disable", "reenable", "mask"}

// serviceVerbs — действия service/invoke-rc.d, которые ждут задание.
var serviceVerbs = []string{"start", "stop", "restart", "reload", "force-reload", "try-restart",
	"condrestart", "reload-or-restart"}

// unitTypes — суффиксы типов юнитов; имя без них systemctl дополняет .service.
var unitTypes = []string{"service", "socket", "device", "mount", "automount", "swap", "target",
	"path", "timer", "slice", "scope"}

// binaryMarkers — слова, при которых двоичный файл дерева не пропускается.
var binaryMarkers = [][]byte{[]byte("systemctl"), []byte("deb-systemd-invoke"), []byte("invoke-rc.d")}

// orderKeys — зависимости юнита, дающие ребро порядка (см. addUnits).
var orderKeys = []string{"After", "Before", "WantedBy", "RequiredBy", "UpheldBy", "Wants", "Requires", "Upholds"}

var (
	reAssign  = regexp.MustCompile(`^(?:(?:export|readonly|local|declare|typeset)(?:\s+-[A-Za-z]+)*\s+)?([A-Za-z_][A-Za-z0-9_]*)\+?=(.*)$`)
	reDynamic = regexp.MustCompile(`(?:^|[\s;&|(])(?:for\s+([A-Za-z_][A-Za-z0-9_]*)\s|` +
		`(?:read|mapfile|readarray)(?:\s+-[A-Za-z]+(?:\s+\S+)?)*\s+([A-Za-z_][A-Za-z0-9_]*)|` +
		`printf\s+-v\s+([A-Za-z_][A-Za-z0-9_]*)|([A-Za-z_][A-Za-z0-9_]*)\[[^\]]*\]\+?=)`)
	reVarRef = regexp.MustCompile(`\$(?:\{([A-Za-z_][A-Za-z0-9_]*)(?:\[[@*0-9]+\])?\}|([A-Za-z_][A-Za-z0-9_]*))`)
)

// unresolved marks a unit argument that did not resolve to a name.
const unresolved = "\x00"

// unitLockLimits — пределы анализа, общие на весь каталог.
type unitLockLimits struct {
	treeBytes  int // байт, прочитанных из деревьев
	treeLines  int // строк текста
	expansions int // вариантов разворота одного слова
	wordLen    int // длина слова и каждого варианта; имя юнита — до 255 байт
	orderNodes int // юнитов, упорядоченных после одного проверяемого
	work       int // шагов обхода порядка и сравнений имён
	names      int // байт имён юнитов во всех найденных вызовах
}

var defaultUnitLockLimits = unitLockLimits{treeBytes: 64 << 20, treeLines: 1 << 20, expansions: 256,
	wordLen: 1024, orderNodes: 4096, work: 1 << 22, names: 16 << 20}

// unitLockAnalysis — состояние правила на один прогон валидатора: разборы
// деревьев по расширению и общий счёт пределов.
type unitLockAnalysis struct {
	lim                       unitLockLimits
	scans                     map[string]*lockScan
	bytes, lines, work, names int
	incomplete                string // первый превышенный предел
}

func newUnitLockAnalysis(lim unitLockLimits) *unitLockAnalysis {
	return &unitLockAnalysis{lim: lim, scans: map[string]*lockScan{}}
}

// fail records the first exceeded limit; the analysis is incomplete since.
func (a *unitLockAnalysis) fail(why string) {
	if a.incomplete == "" {
		a.incomplete = why
	}
}

// charge spends n steps of the work budget; false once the analysis is
// incomplete.
func (a *unitLockAnalysis) charge(n int) bool {
	if a.work += n; a.work > a.lim.work {
		a.fail("больше " + strconv.Itoa(a.lim.work) + " шагов анализа")
	}
	return a.incomplete == ""
}

// scan returns the analysis of the tree of x, made once per run.
func (a *unitLockAnalysis) scan(x loaded) *lockScan {
	if s, ok := a.scans[x.m.Name]; ok {
		return s
	}
	s := &lockScan{a: a, after: map[string][]string{}, unitOf: map[string]string{}, ext: x.m.Name,
		added: map[string]bool{}}
	if h, ok := x.m.Hooks[manifest.HookConfigure]; ok {
		s.conf = &h
	}
	s.addUnits(x)
	s.scanTree(x.dir)
	a.scans[x.m.Name] = s
	return s
}

// syncJob is a synchronous systemd job found in the extension tree.
type syncJob struct {
	where string   // файл:строка
	cmd   string   // команда целиком
	units []string // имена юнитов после разрешения; unresolved — неизвестно
}

// lockScan is what the rule knows about the extension whose action a unit
// calls: its synchronous jobs and the ordering of the units it and the
// calling extensions lay out.
type lockScan struct {
	a      *unitLockAnalysis
	jobs   []syncJob
	after  map[string][]string // юнит → те, после которых он упорядочен
	unitOf map[string]string   // текст юнита (с drop-in'ами) по имени
	conf   *manifest.Hook      // hooks.configure, если есть
	ext    string
	added  map[string]bool // чьи юниты уже в графе
	// globOrder — первый glob в зависимостях юнита: systemd их не
	// разворачивает, порядок с ним не анализируется.
	globOrder string
	// Кеши, сбрасываются при addUnits: обратные рёбра, юниты после юнита
	// (nil — предел превышен), причина по юниту.
	before   map[string][]string
	waiters  map[string]map[string]bool
	evidence map[string]string
}

// addUnits records the unit files (and drop-ins) x lays out.
func (s *lockScan) addUnits(x loaded) {
	if s.added[x.m.Name] {
		return
	}
	s.added[x.m.Name] = true
	s.before, s.waiters, s.evidence = nil, nil, nil
	for _, f := range x.m.Files {
		name := path.Base(f.Dst)
		if d := path.Base(path.Dir(f.Dst)); strings.HasSuffix(d, ".d") && isUnitName(strings.TrimSuffix(d, ".d")) {
			name = strings.TrimSuffix(d, ".d")
		} else if !isUnitName(name) {
			continue
		}
		data, ok := s.read(filepath.Join(x.dir, filepath.FromSlash(f.Src)), path.Join(x.rel, f.Src))
		if !ok {
			continue
		}
		text := string(data)
		s.unitOf[name] += text + "\n"
		for _, line := range strings.Split(text, "\n") {
			key, val, ok := strings.Cut(strings.TrimSpace(line), "=")
			key = strings.TrimSpace(key)
			if !ok || !slices.Contains(orderKeys, key) {
				continue
			}
			for _, u := range strings.Fields(val) {
				if isGlob(u) {
					if s.globOrder == "" {
						s.globOrder = name + ": " + key + "=" + u
					}
					continue
				}
				u = unitName(u)
				switch {
				case key == "After":
					s.after[name] = append(s.after[name], u)
				case key == "Before":
					s.after[u] = append(s.after[u], name)
				case strings.HasSuffix(key, "By") && strings.HasSuffix(u, ".target"):
					// цель упорядочена после юнитов, которые она тянет
					s.after[u] = append(s.after[u], name)
				case !strings.HasSuffix(key, "By") && strings.HasSuffix(name, ".target"):
					s.after[name] = append(s.after[name], u)
				}
			}
		}
	}
}

// read reads a file of a tree within the byte budget; ok is false when it
// is unreadable or over the budget (the analysis is incomplete then).
func (s *lockScan) read(p, rel string) ([]byte, bool) {
	st, err := os.Stat(p)
	if err != nil || !st.Mode().IsRegular() {
		return nil, false
	}
	if s.a.bytes+int(st.Size()) > s.a.lim.treeBytes {
		s.a.fail("деревья больше " + strconv.Itoa(s.a.lim.treeBytes) + " байт")
		return nil, false
	}
	data, err := os.ReadFile(p)
	if err != nil {
		s.a.fail(rel + ": " + err.Error())
		return nil, false
	}
	s.a.bytes += len(data)
	return data, true
}

// scanTree reads every file of the tree (scripts, libraries, units; not
// extension.yaml and markdown), resolves simple assignments across the tree
// and collects the synchronous jobs. Text is taken as bytes, whatever its
// encoding; a binary file mentioning systemctl makes the analysis incomplete.
func (s *lockScan) scanTree(dir string) {
	type logical struct {
		where, text string
	}
	var lines []logical
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if s.a.incomplete != "" {
			return fs.SkipAll
		}
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if rel == manifest.ManifestFile || strings.HasSuffix(rel, ".md") {
			return nil
		}
		data, ok := s.read(p, rel)
		if !ok {
			return nil
		}
		if bytes.IndexByte(data, 0) >= 0 {
			if slices.ContainsFunc(binaryMarkers, func(m []byte) bool { return bytes.Contains(data, m) }) {
				s.a.fail(s.ext + ": " + rel + ": двоичный файл упоминает systemctl")
			}
			return nil
		}
		raw := strings.Split(string(data), "\n")
		if s.a.lines += len(raw); s.a.lines > s.a.lim.treeLines {
			s.a.fail("в деревьях больше " + strconv.Itoa(s.a.lim.treeLines) + " строк")
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
			m := reAssign.FindStringSubmatch(strings.TrimSpace(stmt))
			if m == nil || dynamic[m[1]] {
				continue
			}
			for _, v := range assignedValues(m[2]) {
				if len(v) > s.a.lim.wordLen {
					dynamic[m[1]] = true // не имя юнита — неразрешимо
					delete(vars, m[1])
					break
				}
				vars[m[1]] = append(vars[m[1]], v)
			}
		}
	}
	for _, l := range lines {
		for _, words := range commands(l.text) {
			cmd, units, ok := syncCall(words)
			if !ok {
				continue
			}
			var names []string
			for _, w := range units {
				for _, n := range s.expand(w, vars, dynamic) {
					if s.a.names += len(n) + 1; s.a.names > s.a.lim.names {
						s.a.fail("имён юнитов в вызовах больше " + strconv.Itoa(s.a.lim.names) + " байт")
						return
					}
					names = append(names, n)
				}
			}
			s.jobs = append(s.jobs, syncJob{where: l.where, cmd: cmd, units: names})
		}
	}
}

// syncCall recognises in words a call that queues a systemd job and waits
// for it: systemctl or deb-systemd-invoke without --no-block, service and
// invoke-rc.d. It returns the command and its unit arguments; a verb that
// did not resolve counts as a job, a job without units gets unresolved.
func syncCall(words []string) (cmd string, units []string, ok bool) {
	i := slices.IndexFunc(words, func(w string) bool {
		switch path.Base(w) {
		case "systemctl", "deb-systemd-invoke", "service", "invoke-rc.d":
			return true
		}
		return false
	})
	if i < 0 {
		return "", nil, false
	}
	cmd = strings.Join(words[i:], " ")
	j := i + 1
	for j < len(words) && strings.HasPrefix(words[j], "-") {
		if slices.Contains(systemctlValueFlags, words[j]) {
			j++
		}
		j++
	}
	if j >= len(words) {
		return "", nil, false
	}
	if b := path.Base(words[i]); b == "service" || b == "invoke-rc.d" {
		if j+1 >= len(words) {
			return "", nil, false
		}
		if verb := words[j+1]; !strings.Contains(verb, "$") && !slices.Contains(serviceVerbs, verb) {
			return "", nil, false
		}
		return cmd, []string{words[j]}, true
	}
	if slices.Contains(words, "--no-block") {
		return "", nil, false
	}
	verb := words[j]
	if !strings.Contains(verb, "$") && !slices.Contains(jobVerbs, verb) &&
		(!slices.Contains(nowVerbs, verb) || !slices.Contains(words, "--now")) {
		return "", nil, false
	}
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
	if strings.Contains(verb, "$") || len(units) == 0 {
		units = append(units, unresolved) // "$@" обёртки, xargs: юниты неизвестны
	}
	return cmd, units, true
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

// expand substitutes the variables of w by every value assigned in the tree.
// A variable assigned dynamically, unknown or with a value that is not a
// literal gives unresolved, and so does a word or a variant longer than
// wordLen or more than expansions variants.
func (s *lockScan) expand(w string, vars map[string][]string, dynamic map[string]bool) []string {
	lim := s.a.lim
	if len(w) > lim.wordLen {
		return []string{unresolved}
	}
	out := []string{""}
	for w != "" {
		i := strings.IndexByte(w, '$')
		if i < 0 {
			for k := range out {
				if out[k] += w; len(out[k]) > lim.wordLen {
					return []string{unresolved}
				}
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
		if !ok || dynamic[name] || len(vals) == 0 || len(out)*len(vals) > lim.expansions {
			return []string{unresolved}
		}
		next := make([]string, 0, len(out)*len(vals))
		for _, prefix := range out {
			for _, v := range vals {
				variant := prefix + w[:i] + v
				if strings.Contains(v, "$") || len(variant) > lim.wordLen {
					return []string{unresolved}
				}
				next = append(next, variant)
			}
		}
		out, w = next, w[i+m[1]:]
	}
	return out
}

// waitersOf returns unit and every unit ordered after it (transitively), by
// a breadth-first walk over the reversed edges; nil past orderNodes or the
// work budget (the analysis is incomplete then).
func (s *lockScan) waitersOf(unit string) map[string]bool {
	if w, ok := s.waiters[unit]; ok {
		return w
	}
	if s.waiters == nil {
		s.waiters = map[string]map[string]bool{}
	}
	if s.before == nil {
		s.before = map[string][]string{}
		for u, deps := range s.after {
			if !s.a.charge(len(deps) + 1) {
				return nil
			}
			for _, d := range deps {
				s.before[d] = append(s.before[d], u)
			}
		}
	}
	w := map[string]bool{unit: true}
	queue := []string{unit}
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		for _, next := range s.before[u] {
			if !s.a.charge(1) {
				return nil
			}
			if w[next] {
				continue
			}
			if len(w) >= s.a.lim.orderNodes {
				s.a.fail("после " + unit + " упорядочено больше " + strconv.Itoa(s.a.lim.orderNodes) + " юнитов")
				s.waiters[unit] = nil
				return nil
			}
			w[next] = true
			queue = append(queue, next)
		}
	}
	s.waiters[unit] = w
	return w
}

// why explains why a holder of the lock can wait for a job of unit; "" when
// nothing in the tree points to it. The answer is cached per unit; once a
// limit is exceeded it is "анализ неполон: …".
func (s *lockScan) why(unit string) string {
	if e, ok := s.evidence[unit]; ok {
		return e
	}
	e := s.findWhy(unit)
	if s.a.incomplete != "" {
		e = "анализ неполон: " + s.a.incomplete
	}
	if s.evidence == nil {
		s.evidence = map[string]string{}
	}
	s.evidence[unit] = e
	return e
}

func (s *lockScan) findWhy(unit string) string {
	w := s.waitersOf(unit)
	if w == nil {
		return ""
	}
	for _, j := range s.jobs {
		for _, u := range j.units {
			if !s.a.charge(1) {
				return ""
			}
			wait := u == unresolved || w[unitName(u)]
			if !wait && isGlob(u) {
				for cand := range w {
					if !s.a.charge(1) {
						return ""
					}
					if m, _ := path.Match(u, cand); m {
						wait = true
						break
					}
				}
			}
			switch {
			case wait:
				return j.where + ": `" + j.cmd + "` ждёт задание"
			case s.globOrder != "":
				return j.where + ": `" + j.cmd + "` ждёт задание, а порядок с glob (" + s.globOrder +
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
func (t *tree) warnUnitLock(x loaded, idx map[string]loaded, a *unitLockAnalysis) {
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
			act, ok := target.m.Actions[action]
			if !ok || act.Lock == manifest.LockNone {
				continue
			}
			s := a.scan(target)
			s.addUnits(x)
			why := s.why(unit)
			if why == "" {
				continue
			}
			t.r.warn(warnUnitLock, "%s: %s:%d: %s вызывает cream run %s %s с lock: %s, а держатель lock/%s может "+
				"ждать задание %s: %s — тупик (§7.3); действие из Exec* такого юнита объявляется lock: none",
				t.ext, path.Join(t.rel, f.Src), n+1, strings.TrimSpace(key), ext, action, act.Lock, ext, unit, why)
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
	if isUnitName(w) || isGlob(w) || w == unresolved {
		return w
	}
	return w + ".service"
}
