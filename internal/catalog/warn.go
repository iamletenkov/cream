package catalog

import (
	"bytes"
	"path"
	"slices"
	"strings"

	"github.com/iamletenkov/cream/internal/manifest"
)

// Эвристики предупреждений: валидатор не исполняет shell, он ищет вызовы по
// словам строки. Пропустить хитро составленный вызов он может, ложных
// срабатываний на комментариях и чужих словах быть не должно.

// valueFlags — глобальные флаги cream со значением отдельным словом (§11.2).
var valueFlags = []string{"--lock-timeout", "--root", "--sources", "--cache"}

// jobVerbs — подкоманды systemctl, которые ставят задание и без --no-block
// ждут его завершения.
var jobVerbs = []string{"start", "stop", "restart", "reload", "try-restart", "reload-or-restart",
	"try-reload-or-restart", "isolate"}

// unitExecKeys — строки юнита, которые выполняются при старте и reload (§7.3).
var unitExecKeys = []string{"ExecStartPre", "ExecStart", "ExecReload"}

func warnExtension(r *Report, x loaded, idx map[string]*manifest.Manifest) {
	t := tree{r: r, dir: x.dir, rel: x.rel, ext: x.m.Name}
	t.warnLockNoneSet(x.m)
	t.warnApplyBlocking(x.m)
	t.warnUnitLock(x.m, idx)
}

// script reads an executable file of the tree as text lines; nil for ELF
// or unreadable files.
func (t *tree) script(p string) []string {
	data, err := readHead(t.path(p), maxScript)
	if err != nil || !bytes.HasPrefix(data, []byte("#!")) {
		return nil
	}
	return strings.Split(string(data), "\n")
}

// commands splits a shell line into simple commands (by ; | & ( ) ` and
// $( ), each into words without quotes. A comment is dropped.
func commands(line string) [][]string {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "#") {
		return nil
	}
	if i := strings.Index(line, " #"); i >= 0 {
		line = line[:i]
	}
	parts := strings.FieldsFunc(line, func(r rune) bool { return strings.ContainsRune(";|&()`", r) })
	var out [][]string
	for _, p := range parts {
		p = strings.NewReplacer(`"`, "", `'`, "").Replace(p)
		if words := strings.Fields(p); len(words) > 0 {
			out = append(out, words)
		}
	}
	return out
}

// skipFlags returns the index of the first non-flag word at or after i.
func skipFlags(words []string, i int) int {
	for i < len(words) && strings.HasPrefix(words[i], "-") {
		if slices.Contains(valueFlags, words[i]) {
			i++
		}
		i++
	}
	return i
}

// creamCall finds a call of cream in words and returns its subcommand and
// the words after it (flags of cream before the subcommand skipped).
func creamCall(words []string) (sub string, rest []string, ok bool) {
	for i, w := range words {
		if path.Base(strings.TrimLeft(w, "-@:+!|$")) != "cream" {
			continue
		}
		j := skipFlags(words, i+1)
		if j >= len(words) {
			return "", nil, false
		}
		return words[j], words[j+1:], true
	}
	return "", nil, false
}

// warnLockNoneSet: an action with lock: none only reads the domain (§6.3).
func (t *tree) warnLockNoneSet(m *manifest.Manifest) {
	for _, name := range sortedKeys(m.Actions) {
		if m.Actions[name].Lock != manifest.LockNone {
			continue
		}
		p := "actions/" + name
		for n, line := range t.script(p) {
			if hasCall(line, func(sub string, _ []string) bool { return sub == "set" }) {
				t.r.warn(warnLockNoneSet, "%s: %s:%d: действие с lock: none вызывает cream set — писать домен "+
					"без блокировки нельзя (§6.3); нужен lock: exclusive", t.ext, path.Join(t.rel, p), n+1)
				break
			}
		}
	}
}

func hasCall(line string, match func(sub string, rest []string) bool) bool {
	for _, words := range commands(line) {
		if sub, rest, ok := creamCall(words); ok && match(sub, rest) {
			return true
		}
	}
	return false
}

// warnApplyBlocking: the apply hook must not wait for systemd jobs (§8.4).
func (t *tree) warnApplyBlocking(m *manifest.Manifest) {
	if _, ok := m.Hooks[manifest.HookApply]; !ok {
		return
	}
	p := "hooks/" + string(manifest.HookApply)
	for n, line := range t.script(p) {
		for _, words := range commands(line) {
			i := slices.IndexFunc(words, func(w string) bool { return path.Base(w) == "systemctl" })
			if i < 0 || slices.Contains(words, "--no-block") {
				continue
			}
			if j := skipFlags(words, i+1); j < len(words) && slices.Contains(jobVerbs, words[j]) {
				t.r.warn(warnApplyBlocking, "%s: %s:%d: `%s` без --no-block — хук apply не должен ждать заданий "+
					"systemd (§8.4)", t.ext, path.Join(t.rel, p), n+1, strings.Join(words[i:], " "))
			}
		}
	}
}

// warnUnitLock: an action called from Exec* of a unit the extension lays
// out must be lock: none, or a restart under the domain lock deadlocks
// (§7.3). Actions of extensions outside idx are not known and not checked.
func (t *tree) warnUnitLock(m *manifest.Manifest, idx map[string]*manifest.Manifest) {
	for _, f := range m.Files {
		if !isServiceUnit(f.Dst) {
			continue
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
			if a, ok := target.Actions[action]; ok && a.Lock != manifest.LockNone {
				t.r.warn(warnUnitLock, "%s: %s:%d: %s вызывает cream run %s %s с lock: %s — действие из Exec* юнита "+
					"объявляется lock: none, иначе restart под блокировкой домена ждёт сам себя (§7.3)",
					t.ext, path.Join(t.rel, f.Src), n+1, strings.TrimSpace(key), ext, action, a.Lock)
			}
		}
	}
}

// isServiceUnit: a service unit or its drop-in (only they have Exec* lines).
func isServiceUnit(dst string) bool {
	return strings.HasSuffix(dst, ".service") || strings.HasSuffix(path.Dir(dst), ".service.d")
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
