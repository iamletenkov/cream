package catalog

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iamletenkov/cream/internal/cerr"
)

// writeExt lays out an extension tree under a temp dir: files maps a path in
// the tree to its text; hooks/ and actions/ get the execute bit.
func writeExt(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "unit-ext")
	for p, text := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o644)
		if strings.HasPrefix(p, "hooks/") || strings.HasPrefix(p, "actions/") {
			mode = 0o755
		}
		if err := os.WriteFile(full, []byte(text), mode); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// unitExt is an extension with the unit join.service calling the action
// retry (lock: exclusive) from ExecStart; the case adds the rest.
func unitExt(manifestTail string, extra map[string]string) map[string]string {
	files := map[string]string{
		"extension.yaml": `schema: cream/v1
name: unit-ext
version: 1.0.0
summary: фикстура правила validate.unit_lock
cream: ">=1.0.0"
platform: {family: deb, arch: [amd64, arm64]}
hooks:
  install: {}
` + manifestTail + `actions:
  retry: {summary: "повтор под блокировкой", lock: exclusive}
  restart-main: {summary: "перезапуск main под блокировкой", lock: exclusive}
files:
  - {src: files/join.service, dst: /etc/systemd/system/join.service}
  - {src: files/join.timer, dst: /etc/systemd/system/join.timer}
  - {src: files/main.service, dst: /etc/systemd/system/main.service}
`,
		"hooks/install":        "#!/bin/sh\nexit 0\n",
		"actions/retry":        "#!/bin/sh\nexit 0\n",
		"actions/restart-main": "#!/bin/sh\nsystemctl restart main.service\n",
		"files/join.service": `[Unit]
Description=повтор регистрации
After=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/bin/cream run --lock-timeout 6m unit-ext retry
`,
		"files/join.timer": "[Timer]\nOnCalendar=minutely\n",
		"files/main.service": `[Unit]
After=network-online.target

[Service]
ExecStart=/bin/true
`,
	}
	for k, v := range extra {
		files[k] = v
		base := path.Base(k)
		if strings.HasPrefix(k, "files/") && isUnitName(base) && !strings.Contains(files["extension.yaml"], k) {
			files["extension.yaml"] += "  - {src: " + k + ", dst: /etc/systemd/system/" + base + "}\n"
		}
	}
	return files
}

// TestUnitLockNarrow: validate.unit_lock warns only when a holder of the
// domain lock can wait for the unit's job (§7.3), not on every locked action
// called from Exec*.
func TestUnitLockNarrow(t *testing.T) {
	t.Parallel()
	// Образец teleport (cream-extensions): под блокировкой юнит только
	// включают и выключают, таймер стартует --no-block, синхронно
	// перезапускается другой юнит, не упорядоченный после этого.
	teleportLike := map[string]string{
		"files/lib.sh": `#!/usr/bin/env bash
JOIN_SERVICE=join.service
JOIN_TIMER=join.timer
PATH_UNITS=(apps.path token.path)
enable_join(){
    systemctl enable "$JOIN_SERVICE" "$JOIN_TIMER" >/dev/null 2>&1 \
        || warn "не включены"
    systemctl start --no-block "$JOIN_TIMER" >/dev/null 2>&1 || true
}
disable_join(){
    systemctl disable "$JOIN_SERVICE" >/dev/null 2>&1 || true
    systemctl disable --now "$JOIN_TIMER" >/dev/null 2>&1 || true
}
systemctl enable --now "${PATH_UNITS[@]}" >/dev/null 2>&1 || true
systemctl restart main.service >/dev/null 2>&1 || return 1
systemctl stop main >/dev/null 2>&1
systemctl --no-block reload-or-restart join.service
# systemctl restart join.service — в комментарии не считается
`,
		"actions/retry": "#!/usr/bin/env bash\nexec \"${CREAM_EXT_DIR:-$(dirname \"$0\")/..}/files/lib.sh\" join-retry \"$@\"\n",
	}
	tests := []struct {
		name    string
		tail    string
		extra   map[string]string
		mention string // "" — предупреждения нет
	}{
		{name: "teleport-like", extra: teleportLike},
		{name: "action restarts the unit", extra: map[string]string{
			"actions/restart-main": "#!/bin/sh\nset -eu\nsystemctl daemon-reload\nsystemctl restart join.service\n"},
			mention: "actions/restart-main:4"},
		{name: "hook starts the unit without suffix", extra: map[string]string{
			"hooks/install": "#!/bin/sh\nsystemctl -q start join\n"}, mention: "hooks/install:2"},
		{name: "unit through a variable", extra: map[string]string{
			"files/lib.sh": "#!/bin/bash\nUNIT=\"join.service\"\nsystemctl try-restart \"${UNIT}\" || true\n"},
			mention: "files/lib.sh:3"},
		{name: "unit through an array", extra: map[string]string{
			"files/lib.sh": "#!/bin/bash\nUNITS=(main.service)\nUNITS+=(join.service)\nsystemctl stop \"${UNITS[@]}\"\n"},
			mention: "files/lib.sh:4"},
		{name: "unknown unit argument", extra: map[string]string{
			"files/lib.sh": "#!/bin/bash\nfor u in a b; do systemctl restart \"$u\"; done\n"},
			mention: "files/lib.sh:2"},
		{name: "positional argument", extra: map[string]string{
			"files/lib.sh": "#!/bin/sh\nsystemctl reload \"$1\"\n"}, mention: "files/lib.sh:2"},
		{name: "glob", extra: map[string]string{
			"files/lib.sh": "#!/bin/sh\nsystemctl restart 'jo*'\n"}, mention: "files/lib.sh:2"},
		{name: "continuation line", extra: map[string]string{
			"files/lib.sh": "#!/bin/sh\nsystemctl restart \\\n    join.service\n"}, mention: "files/lib.sh:2"},
		{name: "job mode value flag", extra: map[string]string{
			"files/lib.sh": "#!/bin/sh\nsystemctl --job-mode replace restart join.service\n"}, mention: "files/lib.sh:2"},
		{name: "enable --now", extra: map[string]string{
			"files/lib.sh": "#!/bin/sh\nsystemctl enable --now join.service\n"}, mention: "files/lib.sh:2"},
		{name: "no-block is fine", extra: map[string]string{
			"files/lib.sh": "#!/bin/sh\nsystemctl --no-block restart join.service\nsystemctl enable join.service\n"}},
		{name: "unit is ordered before the restarted one", extra: map[string]string{
			"files/main.service": "[Unit]\nAfter=network-online.target join.service\n[Service]\nExecStart=/bin/true\n"},
			mention: "actions/restart-main:2"},
		{name: "ordering through Before= of the unit", extra: map[string]string{
			"files/join.service": "[Unit]\nBefore=main.service\n[Service]\nType=oneshot\n" +
				"ExecStart=/usr/bin/cream run unit-ext retry\n"},
			mention: "actions/restart-main:2"},
		{name: "ordering through a drop-in", extra: map[string]string{
			"files/main.conf": "[Unit]\nAfter=join.service\n"},
			tail:    "",
			mention: "actions/restart-main:2"},
		{name: "unit ordered against cream-configure", extra: map[string]string{
			"files/join.service": "[Unit]\nAfter=cream-configure@unit-ext.service\n[Service]\nType=oneshot\n" +
				"ExecStart=/usr/bin/cream run unit-ext retry\n"},
			mention: "cream-configure"},
		{name: "configure ordered after the unit", tail: "  configure: {stage: system, after: [join.service]}\n",
			extra: map[string]string{"hooks/configure": "#!/bin/sh\nexit 0\n"}, mention: "cream-configure"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			files := unitExt(tc.tail, tc.extra)
			if _, ok := tc.extra["files/main.conf"]; ok {
				files["extension.yaml"] = strings.Replace(files["extension.yaml"], "files:\n",
					"files:\n  - {src: files/main.conf, dst: /etc/systemd/system/main.service.d/10-order.conf}\n", 1)
			}
			r, err := ValidateDir(writeExt(t, files), cream100(t))
			if err != nil {
				t.Fatalf("ValidateDir: %v (%+v)", err, r.Errors)
			}
			var got []string
			for _, w := range r.Warnings {
				if w.Code == warnUnitLock {
					got = append(got, w.Message)
				}
			}
			if tc.mention == "" {
				if len(got) != 0 {
					t.Fatalf("ложное предупреждение: %q", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("предупреждений unit_lock %d, ждали 1: %q (все: %+v)", len(got), got, r.Warnings)
			}
			for _, want := range []string{"files/join.service", "ExecStart", "retry", "exclusive", tc.mention} {
				if !strings.Contains(got[0], want) {
					t.Errorf("в %q нет %q", got[0], want)
				}
			}
		})
	}
}

// TestUnitLockBombs: a hostile catalog (cream source add) must not blow up
// the analysis; what it cannot analyse is warned about (conservative).
func TestUnitLockBombs(t *testing.T) {
	t.Parallel()
	var many strings.Builder
	for i := range 1000 {
		fmt.Fprintf(&many, " u%d.service", i)
	}
	var chain strings.Builder
	for i := range 2000 {
		fmt.Fprintf(&chain, "After=g%d*.service g%d?.service [g%d].service\n", i, i+1, i)
	}
	tests := []struct {
		name  string
		extra map[string]string
	}{
		{"glob chain and cycle in ordering", map[string]string{
			"files/main.service": "[Unit]\n" + chain.String() + "Before=*.service *\n[Service]\nExecStart=/bin/true\n",
			"files/join.service": "[Unit]\nAfter=m*.service *\nBefore=j*\n[Service]\nType=oneshot\n" +
				"ExecStart=/usr/bin/cream run unit-ext retry\n",
			"files/lib.sh": "#!/bin/sh\nsystemctl restart 'g*'\n"}},
		{"array of 1000 values repeated 10 times", map[string]string{
			"files/lib.sh": "#!/bin/bash\nU=(" + many.String() + ")\nsystemctl restart " +
				strings.Repeat("${U[@]}", 10) + "\n"}},
		{"huge word of references", map[string]string{
			"files/lib.sh": "#!/bin/bash\nA=a\nsystemctl restart " + strings.Repeat("$A", 50000) + "\n"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := writeExt(t, unitExt("", tc.extra))
			cream := cream100(t)
			done := make(chan Report, 1)
			go func() {
				r, _ := ValidateDir(dir, cream)
				done <- r
			}()
			select {
			case r := <-done:
				if !slices.ContainsFunc(r.Warnings, func(w cerr.Warning) bool { return w.Code == warnUnitLock }) {
					t.Fatalf("нет консервативного предупреждения: %+v (ошибки %+v)", r.Warnings, r.Errors)
				}
			case <-time.After(20 * time.Second):
				t.Fatal("анализ не уложился в 20 с")
			}
		})
	}
}

// runWithin runs ValidateDir on dir and fails the test past d.
func runWithin(t *testing.T, dir string, d time.Duration) Report {
	t.Helper()
	cream := cream100(t)
	done := make(chan Report, 1)
	go func() {
		r, _ := ValidateDir(dir, cream)
		done <- r
	}()
	select {
	case r := <-done:
		return r
	case <-time.After(d):
		t.Fatalf("анализ не уложился в %s", d)
	}
	return Report{}
}

// TestUnitLockBudgetShared: the work budget and the evidence are per
// analysis, not per Exec line — many Exec lines of one unit do not multiply
// the work.
func TestUnitLockBudgetShared(t *testing.T) {
	t.Parallel()
	unit := "[Unit]\nAfter=network-online.target\n[Service]\nType=oneshot\n" +
		strings.Repeat("ExecStartPre=/usr/bin/cream run unit-ext retry\n", 20000)
	lib := "#!/bin/sh\n" + strings.Repeat("systemctl restart 'zz*'\n", 100000)
	r := runWithin(t, writeExt(t, unitExt("", map[string]string{"files/join.service": unit, "files/lib.sh": lib})),
		20*time.Second)
	if len(r.Errors) != 0 || len(r.Warnings) != 0 {
		t.Fatalf("ошибки %+v, предупреждения %+v (анализ уложился бы в пределы)", r.Errors, r.Warnings)
	}
}

// TestUnitLockLongVariants: variants longer than a unit name after the
// substitution are unresolved (a conservative warning), not kept.
func TestUnitLockLongVariants(t *testing.T) {
	t.Parallel()
	vals := make([]string, 16)
	for i := range vals {
		vals[i] = fmt.Sprintf("v%d", i) + strings.Repeat("x", 900)
	}
	lib := "#!/bin/bash\nV=(" + strings.Join(vals, " ") + ")\n" + strings.Repeat("systemctl restart $V$V\n", 10)
	r := runWithin(t, writeExt(t, unitExt("", map[string]string{"files/lib.sh": lib})), 20*time.Second)
	if !slices.ContainsFunc(r.Warnings, func(w cerr.Warning) bool { return w.Code == warnUnitLock }) {
		t.Fatalf("нет консервативного предупреждения: %+v", r.Warnings)
	}
}

// unitLockWarnings validates the unit-ext tree with the limits lim and
// returns the validate.unit_lock messages.
func unitLockWarnings(t *testing.T, tail string, extra map[string]string, lim unitLockLimits) []string {
	t.Helper()
	r, err := validateDir(writeExt(t, unitExt(tail, extra)), cream100(t), lim)
	if err != nil {
		t.Fatalf("validateDir: %v (%+v)", err, r.Errors)
	}
	var got []string
	for _, w := range r.Warnings {
		if w.Code == warnUnitLock {
			got = append(got, w.Message)
		}
	}
	return got
}

// TestUnitLockCoverage: forms of a synchronous call, ordering and limits;
// each case kills a mutation of the rule (the comment names it).
func TestUnitLockCoverage(t *testing.T) {
	t.Parallel()
	def := defaultUnitLockLimits
	small := func(f func(*unitLockLimits)) unitLockLimits { l := def; f(&l); return l }
	var many strings.Builder
	for i := range 5000 {
		fmt.Fprintf(&many, " n%d.service", i)
	}
	joinWith := func(unitLines string) string {
		return "[Unit]\n" + unitLines + "\n[Service]\nType=oneshot\nExecStart=/usr/bin/cream run unit-ext retry\n"
	}
	tests := []struct {
		name    string
		tail    string
		extra   map[string]string
		lim     unitLockLimits
		mention string // "" — предупреждения нет
	}{
		{name: "unresolved verb", extra: map[string]string{"files/lib.sh": "#!/bin/sh\nsystemctl \"$verb\" join\n"},
			mention: "files/lib.sh:2"},
		{name: "wrapper with $@", extra: map[string]string{
			"files/lib.sh": "#!/bin/bash\nsc(){ systemctl \"$@\"; }\nsc enable join\n"}, mention: "files/lib.sh:2"},
		{name: "xargs without units", extra: map[string]string{
			"files/lib.sh": "#!/bin/sh\necho join | xargs systemctl restart\n"}, mention: "files/lib.sh:2"},
		{name: "condrestart", extra: map[string]string{"files/lib.sh": "#!/bin/sh\nsystemctl condrestart join\n"},
			mention: "files/lib.sh:2"},
		{name: "reload-or-try-restart", extra: map[string]string{
			"files/lib.sh": "#!/bin/sh\nsystemctl reload-or-try-restart join\n"}, mention: "files/lib.sh:2"},
		{name: "service", extra: map[string]string{"files/lib.sh": "#!/bin/sh\nservice join force-reload\n"},
			mention: "files/lib.sh:2"},
		{name: "service status is fine", extra: map[string]string{"files/lib.sh": "#!/bin/sh\nservice join status\n"}},
		{name: "invoke-rc.d", extra: map[string]string{"files/lib.sh": "#!/bin/sh\ninvoke-rc.d join restart\n"},
			mention: "files/lib.sh:2"},
		{name: "deb-systemd-invoke", extra: map[string]string{
			"files/lib.sh": "#!/bin/sh\ndeb-systemd-invoke restart join.service\n"}, mention: "files/lib.sh:2"},
		{name: "ExecStop is checked", extra: map[string]string{
			"files/join.service": "[Service]\nType=oneshot\nExecStart=/bin/true\nExecStop=/usr/bin/cream run unit-ext retry\n",
			"files/lib.sh":       "#!/bin/sh\nsystemctl stop join\n"}, mention: "ExecStop"},
		{name: "target pulls the unit through WantedBy", extra: map[string]string{
			"files/join.service": joinWith("[Install]\nWantedBy=multi-user.target"),
			"files/lib.sh":       "#!/bin/sh\nsystemctl start multi-user.target\n"}, mention: "files/lib.sh:2"},
		{name: "target file Wants the unit", extra: map[string]string{
			"files/app.target": "[Unit]\nWants=join.service\n",
			"files/lib.sh":     "#!/bin/sh\nsystemctl start app.target\n"}, mention: "files/lib.sh:2"},
		// мутация: dynamic в expand не учитывается — u разрешилась бы в zz.service
		{name: "loop variable also assigned", extra: map[string]string{
			"files/lib.sh": "#!/bin/bash\nu=zz.service\nfor u in $LIST; do systemctl restart \"$u\"; done\n"},
			mention: "files/lib.sh:3"},
		{name: "not UTF-8 text", extra: map[string]string{
			"files/lib.sh": "#!/bin/sh\n# \xff\xfe латиница\nsystemctl restart join.service # \xe9\n"},
			mention: "files/lib.sh:3"},
		{name: "binary mentioning systemctl", extra: map[string]string{
			"files/helper.bin": "\x7fELF\x00\x00/bin/systemctl\x00restart\x00"}, mention: "двоичный файл"},
		{name: "binary without systemctl is skipped", extra: map[string]string{
			"files/logo.bin": "\x89PNG\x00\x00 restart join.service"}},
		// мутации: ветки conf.Before и conf.Wants
		{name: "configure before the unit", tail: "  configure: {stage: system, before: [join.service]}\n",
			extra: map[string]string{"hooks/configure": "#!/bin/sh\nexit 0\n"}, mention: "cream-configure@unit-ext"},
		{name: "configure wants the unit", tail: "  configure: {stage: system, wants: [join.service]}\n",
			extra: map[string]string{"hooks/configure": "#!/bin/sh\nexit 0\n"}, mention: "cream-configure@unit-ext"},
		// мутация: без проверки посещённых обход цикла a↔b не кончается до предела работы
		{name: "ordering cycle without globs", extra: map[string]string{
			"files/a.service": "[Unit]\nAfter=join.service b.service\n",
			"files/b.service": "[Unit]\nAfter=a.service\n",
			"files/lib.sh":    "#!/bin/sh\nsystemctl restart zz.service\n"}},
		{name: "ordering cycle reaches the restarted unit", extra: map[string]string{
			"files/a.service": "[Unit]\nAfter=join.service b.service\n",
			"files/b.service": "[Unit]\nAfter=a.service\n",
			"files/lib.sh":    "#!/bin/sh\nsystemctl restart b\n"}, mention: "files/lib.sh:2"},
		// мутация: предел orderNodes
		{name: "more units ordered after than orderNodes", extra: map[string]string{
			"files/join.service": joinWith("Before=" + many.String()),
			"files/lib.sh":       "#!/bin/sh\nsystemctl restart zz.service\n"}, mention: "упорядочено больше 4096"},
		// мутации: ветки пределов байт и строк дерева
		{name: "tree bytes limit", lim: small(func(l *unitLockLimits) { l.treeBytes = 600 }),
			extra:   map[string]string{"files/lib.sh": "#!/bin/sh\n" + strings.Repeat("# заполнитель\n", 100)},
			mention: "анализ неполон: деревья больше 600 байт"},
		{name: "tree lines limit", lim: small(func(l *unitLockLimits) { l.treeLines = 20 }),
			extra:   map[string]string{"files/lib.sh": "#!/bin/sh\n" + strings.Repeat("true\n", 30)},
			mention: "анализ неполон: в деревьях больше 20 строк"},
		// мутация: предел работы
		{name: "work limit", lim: small(func(l *unitLockLimits) { l.work = 50 }),
			extra:   map[string]string{"files/lib.sh": "#!/bin/sh\n" + strings.Repeat("systemctl restart zz\n", 100)},
			mention: "анализ неполон: больше 50 шагов"},
		{name: "names limit", lim: small(func(l *unitLockLimits) { l.names = 100 }),
			extra:   map[string]string{"files/lib.sh": "#!/bin/sh\n" + strings.Repeat("systemctl restart zz\n", 100)},
			mention: "анализ неполон: имён юнитов"},
		{name: "long value is unresolved", extra: map[string]string{
			"files/lib.sh": "#!/bin/sh\nU=" + strings.Repeat("z", 2000) + "\nsystemctl restart \"$U\"\n"},
			mention: "files/lib.sh:3"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			lim := tc.lim
			if lim == (unitLockLimits{}) {
				lim = def
			}
			got := unitLockWarnings(t, tc.tail, tc.extra, lim)
			if tc.mention == "" {
				if len(got) != 0 {
					t.Fatalf("ложное предупреждение: %q", got)
				}
				return
			}
			if len(got) != 1 || !strings.Contains(got[0], tc.mention) {
				t.Fatalf("ждали одно предупреждение про %q, получили %q", tc.mention, got)
			}
		})
	}
}

// TestUnitLockForeignUnits: a unit of one extension calls a locked action of
// another; the ordering of the caller's units is part of the analysis.
func TestUnitLockForeignUnits(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	write := func(p, text string, mode os.FileMode) {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("cream-catalog.yaml", "schema: cream/v1\nname: fx\nversion: 1.0.0\nroots: [extensions]\n", 0o644)
	head := "schema: cream/v1\nversion: 1.0.0\nsummary: фикстура\ncream: \">=1.0.0\"\n" +
		"platform: {family: deb, arch: [amd64, arm64]}\nhooks:\n  install: {}\n"
	write("extensions/caller/extension.yaml", "name: caller\n"+head+
		"files:\n  - {src: files/c-join.service, dst: /etc/systemd/system/c-join.service}\n"+
		"  - {src: files/c-main.service, dst: /etc/systemd/system/c-main.service}\n", 0o644)
	write("extensions/caller/hooks/install", "#!/bin/sh\nexit 0\n", 0o755)
	write("extensions/caller/files/c-join.service",
		"[Service]\nType=oneshot\nExecStart=/usr/bin/cream run owner retry\n", 0o644)
	// c-main упорядочен после c-join — знать это можно только из файлов caller
	write("extensions/caller/files/c-main.service", "[Unit]\nAfter=c-join.service\n", 0o644)
	write("extensions/owner/extension.yaml", "name: owner\n"+head+
		"actions:\n  retry: {summary: \"повтор\", lock: exclusive}\n", 0o644)
	write("extensions/owner/hooks/install", "#!/bin/sh\nsystemctl restart c-main.service\n", 0o755)
	write("extensions/owner/actions/retry", "#!/bin/sh\nexit 0\n", 0o755)
	r, err := Validate(root, cream100(t))
	if err != nil {
		t.Fatalf("Validate: %v (%+v)", err, r.Errors)
	}
	if len(r.Warnings) != 1 || r.Warnings[0].Code != warnUnitLock ||
		!strings.Contains(r.Warnings[0].Message, "hooks/install:2") {
		t.Fatalf("предупреждения %+v, ждали unit_lock про hooks/install:2", r.Warnings)
	}
}
