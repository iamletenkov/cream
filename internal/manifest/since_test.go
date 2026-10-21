package manifest

import (
	"errors"
	"strings"
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/keytype"
)

// TestSinceTable закрепляет таблицу «поле → версия» checkSince (ADR-0004,
// спека bisquite-bundled-cream §10.9): стадии cream 1.1, every_boot и
// scope: build cream 1.2. Стадия system и scope config/install — с cream 1.0,
// в таблице их нет.
func TestSinceTable(t *testing.T) {
	t.Parallel()
	want := map[string]string{
		"hooks.configure.stage: early":      "1.1.0",
		"hooks.configure.stage: cloud-user": "1.1.0",
		"hooks.configure.stage: graphical":  "1.1.0",
		"hooks.configure.every_boot":        "1.2.0",
		"config.keys.*.scope: build":        "1.2.0",
	}
	got := map[string]string{}
	for _, f := range sinceFields {
		if _, dup := got[f.feature]; dup {
			t.Errorf("поле %q в таблице дважды", f.feature)
		}
		got[f.feature] = f.since.String()
	}
	if len(got) != len(want) {
		t.Errorf("таблица %v, ждали %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q, ждали %q", k, got[k], v)
		}
	}
}

// TestParseSince12: configure.every_boot (любое значение) и ключ scope: build
// появились в cream 1.2.0 — манифест обязан объявить cream: ">=1.2.0", иначе
// 65 manifest.invalid с details.field и details.since; cream 1.1.x отказывает
// такому манифесту по cream: (68), а не по полю.
func TestParseSince12(t *testing.T) {
	t.Parallel()
	v12 := ParseCreamBuild("1.2.0")
	v114 := ParseCreamBuild("1.1.4")
	const (
		everyBoot   = "hooks: {install: {}, configure: {stage: system, every_boot: %s}}"
		scopeBuild  = "config: {keys: {MIRROR_TOKEN: {type: str, default: \"\", empty: true, secret: file, scope: build, description: токен зеркала}}}\n"
		earlyAndEvB = "hooks: {install: {}, configure: {stage: early, every_boot: true}}"
	)
	tests := []struct {
		name       string
		rng        string
		hooks      string // вместо hooks: {install: {}}; "" — без замены
		extra      string
		cream      CreamBuild
		wantExit   int
		wantCode   string
		wantField  string
		wantSince  string
		wantEvery  bool
		wantBuildK string // ключ, у которого ждём Scope == build
	}{
		{name: "every_boot true при 1.2.0", rng: ">=1.2.0", hooks: strings.Replace(everyBoot, "%s", "true", 1), cream: v12, wantEvery: true},
		{name: "every_boot false при 1.2.0", rng: ">=1.2.0", hooks: strings.Replace(everyBoot, "%s", "false", 1), cream: v12},
		{name: "every_boot на dev при 1.2.0", rng: ">=1.2.0", hooks: strings.Replace(everyBoot, "%s", "true", 1), cream: ParseCreamBuild("dev"), wantEvery: true},
		{name: "every_boot true при >=1.1.0", rng: ">=1.1.0", hooks: strings.Replace(everyBoot, "%s", "true", 1), cream: v12,
			wantExit: 65, wantCode: "manifest.invalid", wantField: "hooks.configure.every_boot", wantSince: "1.2.0"},
		{name: "every_boot false при >=1.1.0 — любое значение", rng: ">=1.1.0", hooks: strings.Replace(everyBoot, "%s", "false", 1), cream: v12,
			wantExit: 65, wantCode: "manifest.invalid", wantField: "hooks.configure.every_boot", wantSince: "1.2.0"},
		{name: "every_boot на dev при >=1.0.0", rng: ">=1.0.0", hooks: strings.Replace(everyBoot, "%s", "true", 1), cream: ParseCreamBuild("dev"),
			wantExit: 65, wantCode: "manifest.invalid", wantField: "hooks.configure.every_boot", wantSince: "1.2.0"},
		{name: "every_boot при >=1.2.0 на cream 1.1.4", rng: ">=1.2.0", hooks: strings.Replace(everyBoot, "%s", "true", 1), cream: v114,
			wantExit: 68, wantCode: "platform.cream_version"},
		{name: "every_boot у install", rng: ">=1.2.0", hooks: "hooks: {install: {every_boot: true}}", cream: v12,
			wantExit: 65, wantCode: "manifest.invalid"},
		{name: "every_boot не bool", rng: ">=1.2.0", hooks: strings.Replace(everyBoot, "%s", "yes please", 1), cream: v12,
			wantExit: 65, wantCode: "manifest.invalid"},
		{name: "every_boot: ~ при >=1.1.0", rng: ">=1.1.0", hooks: strings.Replace(everyBoot, "%s", "~", 1), cream: v12,
			wantExit: 65, wantCode: "manifest.invalid"},
		{name: "every_boot: null при 1.2.0", rng: ">=1.2.0", hooks: strings.Replace(everyBoot, "%s", "null", 1), cream: v12,
			wantExit: 65, wantCode: "manifest.invalid"},
		{name: "пустое every_boot у install", rng: ">=1.0.0", hooks: "hooks:\n  install:\n    every_boot:\n", cream: v12,
			wantExit: 65, wantCode: "manifest.invalid"},
		{name: "early и every_boot при >=1.1.0 — называется 1.2.0", rng: ">=1.1.0", hooks: earlyAndEvB, cream: v12,
			wantExit: 65, wantCode: "manifest.invalid", wantField: "hooks.configure.every_boot", wantSince: "1.2.0"},
		{name: "scope build при 1.2.0", rng: ">=1.2.0", extra: scopeBuild, cream: v12, wantBuildK: "MIRROR_TOKEN"},
		{name: "scope build при >=1.1.0", rng: ">=1.1.0", extra: scopeBuild, cream: v12,
			wantExit: 65, wantCode: "manifest.invalid", wantField: "config.keys.MIRROR_TOKEN.scope", wantSince: "1.2.0"},
		{name: "scope build при >=1.2.0 на cream 1.1.4", rng: ">=1.2.0", extra: scopeBuild, cream: v114,
			wantExit: 68, wantCode: "platform.cream_version"},
		{name: "scope install при >=1.0.0", rng: ">=1.0.0", cream: v12,
			extra: "config: {keys: {V: {type: str, default: x, scope: install, description: v}}}\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pairs := []string{`cream: ">=1.0.0"`, `cream: "` + tc.rng + `"`}
			if tc.hooks != "" {
				pairs = append(pairs, "hooks: {install: {}}", tc.hooks)
			}
			doc := mk(t, tc.extra, pairs...)
			m, err := Parse([]byte(doc), tc.cream, &cerr.Warnings{})
			if tc.wantExit == 0 {
				if err != nil {
					t.Fatalf("Parse: %v\n%s", err, doc)
				}
				if hk, ok := m.Hooks[HookConfigure]; ok && hk.EveryBoot != tc.wantEvery {
					t.Errorf("EveryBoot = %v, ждали %v", hk.EveryBoot, tc.wantEvery)
				}
				if tc.wantBuildK != "" {
					if sp := m.Config[tc.wantBuildK]; sp.Scope != keytype.ScopeBuild {
						t.Errorf("%s.Scope = %q, ждали build", tc.wantBuildK, sp.Scope)
					}
				}
				return
			}
			if cerr.ExitCode(err) != tc.wantExit || cerr.CodeOf(err) != tc.wantCode {
				t.Fatalf("err = %v (exit %d, code %q), ждали %d %s\n%s", err, cerr.ExitCode(err), cerr.CodeOf(err),
					tc.wantExit, tc.wantCode, doc)
			}
			msg := err.Error()
			if tc.wantExit == 68 {
				if !strings.Contains(msg, "требует cream >=1.2.0, а это cream 1.1.4") {
					t.Errorf("сообщение 68: %s", msg)
				}
				return
			}
			if tc.wantField == "" {
				return
			}
			var e *cerr.Error
			if !errors.As(err, &e) {
				t.Fatalf("не *cerr.Error: %T", err)
			}
			if e.Details["field"] != tc.wantField || e.Details["since"] != tc.wantSince {
				t.Errorf("details = %v, ждали field=%s since=%s", e.Details, tc.wantField, tc.wantSince)
			}
			if !strings.Contains(msg, tc.wantField) || !strings.Contains(msg, "cream "+tc.wantSince) ||
				!strings.Contains(msg, `">=`+tc.wantSince+`"`) {
				t.Errorf("сообщение не называет поле и версию: %s", msg)
			}
		})
	}
}

// TestParseScopeEnum: scope — перечисление config|install|build, сообщение об
// ошибке называет все три значения.
func TestParseScopeEnum(t *testing.T) {
	t.Parallel()
	doc := mk(t, "config: {keys: {A: {type: str, default: x, scope: bogus, description: a}}}\n",
		`cream: ">=1.0.0"`, `cream: ">=1.2.0"`)
	_, err := Parse([]byte(doc), ParseCreamBuild("1.2.0"), nil)
	if cerr.ExitCode(err) != 65 || cerr.CodeOf(err) != "manifest.invalid" {
		t.Fatalf("err = %v, ждали 65 manifest.invalid", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "config.keys.A.scope") ||
		!strings.Contains(msg, "допустимо config, install или build") {
		t.Errorf("сообщение: %s", msg)
	}
}
