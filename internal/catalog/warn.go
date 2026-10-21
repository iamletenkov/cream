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
	"try-reload-or-restart", "reload-or-try-restart", "force-reload", "condrestart", "condreload", "condstop",
	"isolate"}

// unitExecKeys — строки юнита, конца которых ждёт задание start, stop или
// reload (§7.3).
var unitExecKeys = []string{"ExecCondition", "ExecStartPre", "ExecStart", "ExecStartPost", "ExecReload",
	"ExecStop", "ExecStopPost"}

func warnExtension(r *Report, x loaded, idx map[string]loaded, a *unitLockAnalysis) {
	t := tree{r: r, dir: x.dir, rel: x.rel, ext: x.m.Name}
	t.warnLockNoneSet(x.m)
	t.warnApplyBlocking(x.m)
	t.warnUnitLock(x, idx, a)
	t.warnOpenWrtTmpfs(x.m)
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

// warnApplyBlocking: the apply hook must not wait for systemd jobs (§8.4);
// on OpenWrt — for an init script restarted in the foreground (ADR-0005).
func (t *tree) warnApplyBlocking(m *manifest.Manifest) {
	if _, ok := m.Hooks[manifest.HookApply]; !ok {
		return
	}
	p := "hooks/" + string(manifest.HookApply)
	for n, line := range t.script(p) {
		if m.Platform.Family == manifest.FamilyOpenWrt {
			t.warnInitdBlocking(p, n+1, line)
			continue
		}
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

// warnInitdBlocking: `/etc/init.d/<svc> restart` (or `service <svc>
// restart`) in an apply hook of the openwrt family waits for the service;
// only a line sent to the background (ends with a single &) passes.
func (t *tree) warnInitdBlocking(p string, n int, line string) {
	trimmed := strings.TrimSpace(line)
	if strings.HasSuffix(trimmed, "&") && !strings.HasSuffix(trimmed, "&&") {
		return
	}
	for _, words := range commands(line) {
		for i, w := range words {
			svc := path.Dir(w) == "/etc/init.d" || (path.Base(w) == "service" && i+1 < len(words))
			if !svc {
				continue
			}
			verb := i + 1
			if path.Base(w) == "service" {
				verb = i + 2
			}
			if verb < len(words) && slices.Contains(jobVerbs, words[verb]) {
				t.r.warn(warnApplyBlocking, "%s: %s:%d: `%s` без & — хук apply не должен ждать перезапуска "+
					"службы procd (§8.4); запускайте фоном", t.ext, path.Join(t.rel, p), n, strings.Join(words[i:], " "))
				return
			}
		}
	}
}

// varTmpfs reports a path under /var on OpenWrt — a tmpfs there: what lies
// in it is gone after a reboot. /var/run is meant to be per boot.
func varTmpfs(p string) bool {
	return strings.HasPrefix(p, "/var/") && !strings.HasPrefix(p, "/var/run/")
}

// warnOpenWrtTmpfs: an extension of the openwrt family that places files
// under /var or names /var/lib in a hook or action loses that state on a
// reboot (spec OpenWrt §8 п.2); state belongs under /opt.
func (t *tree) warnOpenWrtTmpfs(m *manifest.Manifest) {
	if m.Platform.Family != manifest.FamilyOpenWrt {
		return
	}
	for _, f := range m.Files {
		if varTmpfs(f.Dst) {
			t.r.warn(warnOpenWrtTmpfs, "%s: files: %s — /var на OpenWrt в tmpfs, после перезагрузки файла не будет; "+
				"состояние — под /opt", t.ext, f.Dst)
		}
	}
	var scripts []string
	for _, h := range []manifest.HookName{manifest.HookInstall, manifest.HookUpgrade, manifest.HookConfigure,
		manifest.HookApply, manifest.HookRemove} {
		if _, ok := m.Hooks[h]; ok {
			scripts = append(scripts, "hooks/"+string(h))
		}
	}
	for _, a := range sortedKeys(m.Actions) {
		scripts = append(scripts, "actions/"+a)
	}
	for _, p := range scripts {
		for n, line := range t.script(p) {
			if strings.HasPrefix(strings.TrimSpace(line), "#") || !strings.Contains(line, "/var/lib/") {
				continue
			}
			t.r.warn(warnOpenWrtTmpfs, "%s: %s:%d: /var/lib на OpenWrt в tmpfs — состояние пропадёт при перезагрузке; "+
				"используйте $CREAM_STATE_DIR или /opt", t.ext, path.Join(t.rel, p), n+1)
			break
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
