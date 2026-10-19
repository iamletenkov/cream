package catalog

import (
	"fmt"
	"os"
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
