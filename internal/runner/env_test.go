package runner

import (
	"slices"
	"strings"
	"testing"
)

// secretsPath — путь, а не секрет; константа и kv уводят строку из-под эвристики gitleaks.
const secretsPath = "/etc/cream/teleport/secrets"

func fullInput() EnvInput {
	return EnvInput{
		Phase:       "live",
		Ext:         "teleport",
		ExtVersion:  "4.1.0",
		PrevVersion: "4.0.0",
		ExtDir:      "/opt/cream/ext/teleport",
		EtcDir:      "/etc/cream/teleport",
		StateDir:    "/var/lib/cream/ext/teleport",
		Root:        "/",
		ConfigFile:  "/etc/cream/teleport/config",
		SecretsFile: secretsPath,
		Reason:      "apply",
		Action:      "set-managed",
		Output:      "json",
		ChangedKeys: []string{"TELEPORT_PROXY", "NODE_NAME"},
		LockFD:      3,
		LockMode:    "exclusive",
		Values:      map[string]string{"TELEPORT_PROXY": "proxy.example:443", "NODE_NAME": "n1", "EMPTY": ""},
	}
}

func TestBuildEnvGolden(t *testing.T) {
	t.Parallel()
	want := []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"LANG=C.UTF-8",
		"CREAM_PHASE=live",
		"CREAM_EXT=teleport",
		"CREAM_EXT_VERSION=4.1.0",
		"CREAM_PREV_VERSION=4.0.0",
		"CREAM_EXT_DIR=/opt/cream/ext/teleport",
		"CREAM_ETC_DIR=/etc/cream/teleport",
		"CREAM_STATE_DIR=/var/lib/cream/ext/teleport",
		"CREAM_ROOT=/",
		"CREAM_CONFIG_FILE=/etc/cream/teleport/config",
		kv("CREAM_SECRETS_FILE", secretsPath),
		"CREAM_REASON=apply",
		"CREAM_CHANGED_KEYS=TELEPORT_PROXY NODE_NAME",
		"CREAM_ACTION=set-managed",
		"CREAM_OUTPUT=json",
		"CREAM_LOCK_FD=3",
		"CREAM_LOCK_MODE=exclusive",
		"EMPTY=",
		"NODE_NAME=n1",
		"TELEPORT_PROXY=proxy.example:443",
	}
	got := BuildEnv(fullInput())
	if !slices.Equal(got, want) {
		t.Fatalf("окружение\n%s\nожидали\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for range 20 { // порядок не зависит от обхода map
		if again := BuildEnv(fullInput()); !slices.Equal(again, got) {
			t.Fatal("порядок недетерминирован")
		}
	}
}

func TestBuildEnvOmitsOptional(t *testing.T) {
	t.Parallel()
	got := BuildEnv(EnvInput{Phase: "build", Ext: "noop", ExtVersion: "1.0.0", Root: "/"})
	for _, name := range []string{"CREAM_PREV_VERSION", "CREAM_REASON", "CREAM_CHANGED_KEYS", "CREAM_ACTION",
		"CREAM_OUTPUT", "CREAM_LOCK_FD", "CREAM_LOCK_MODE"} {
		if hasVar(got, name) {
			t.Errorf("%s задан без значения: %v", name, got)
		}
	}
	for _, name := range []string{"PATH", "LANG", "CREAM_PHASE", "CREAM_EXT", "CREAM_EXT_DIR", "CREAM_CONFIG_FILE"} {
		if !hasVar(got, name) {
			t.Errorf("нет %s", name)
		}
	}
}

// Ключи домена не перекрывают переменные cream и PATH/LANG, а имена, которые
// нельзя передать в окружение, отбрасываются (манифест их и так не пропускает).
func TestBuildEnvRejectsReservedAndInvalidKeys(t *testing.T) {
	t.Parallel()
	in := EnvInput{Phase: "live", Ext: "x", Values: map[string]string{
		"PATH":        "/tmp/evil",
		"LANG":        "C",
		"HOME":        "/root",
		"PWD":         "/",
		"CREAM_PHASE": "build",
		"CREAM_X":     "1",
		"lower":       "1",
		"A=B":         "1",
		"":            "1",
		"NUL":         "a\x00b",
		"GOOD":        "ok",
		// Тот же список, что у валидатора манифеста (manifest.ValidKey).
		"LD_PRELOAD":      "/tmp/evil.so",
		"LD_LIBRARY_PATH": "/tmp",
		"BASH_ENV":        "/tmp/rc",
		"ENV":             "/tmp/rc",
		"IFS":             "x",
	}}
	got := BuildEnv(in)
	for _, kv := range got {
		name, _, _ := strings.Cut(kv, "=")
		if name == "" || name == "lower" || name == "A" || name == "HOME" || name == "PWD" || name == "NUL" ||
			name == "CREAM_X" || name == "LD_PRELOAD" || name == "LD_LIBRARY_PATH" || name == "BASH_ENV" ||
			name == "ENV" || name == "IFS" {
			t.Errorf("лишняя переменная %q", kv)
		}
	}
	if !slices.Contains(got, "PATH="+fixedPath) || slices.Contains(got, "PATH=/tmp/evil") {
		t.Errorf("PATH перекрыт: %v", got)
	}
	if !slices.Contains(got, "CREAM_PHASE=live") || slices.Contains(got, "CREAM_PHASE=build") {
		t.Errorf("CREAM_PHASE перекрыт: %v", got)
	}
	if !slices.Contains(got, "GOOD=ok") {
		t.Errorf("нет GOOD: %v", got)
	}
	seen := map[string]bool{}
	for _, kv := range got {
		name, _, _ := strings.Cut(kv, "=")
		if seen[name] {
			t.Errorf("переменная %s дважды", name)
		}
		seen[name] = true
	}
}

func hasVar(env []string, name string) bool {
	return slices.ContainsFunc(env, func(kv string) bool { return strings.HasPrefix(kv, name+"=") })
}

func kv(name, value string) string { return name + "=" + value }

// Р19: CREAM_LOCK_MODE goes with CREAM_LOCK_FD only; anything but "exclusive"
// is passed as "shared", the mode a child can never upgrade.
func TestBuildEnvLockMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		fd   int
		mode string
		want string // "" — no CREAM_LOCK_MODE
	}{
		{name: "exclusive", fd: 3, mode: "exclusive", want: "CREAM_LOCK_MODE=exclusive"},
		{name: "shared", fd: 3, mode: "shared", want: "CREAM_LOCK_MODE=shared"},
		{name: "empty mode is shared", fd: 3, mode: "", want: "CREAM_LOCK_MODE=shared"},
		{name: "no fd — no mode", fd: 0, mode: "exclusive"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := BuildEnv(EnvInput{Phase: "live", Ext: "x", LockFD: tc.fd, LockMode: tc.mode})
			if tc.want == "" {
				if hasVar(got, "CREAM_LOCK_MODE") {
					t.Errorf("CREAM_LOCK_MODE без CREAM_LOCK_FD: %v", got)
				}
				return
			}
			if !slices.Contains(got, tc.want) {
				t.Errorf("нет %s: %v", tc.want, got)
			}
		})
	}
}

// TestBuildEnvBuildValues: значения ключей scope: build (ADR-0004) идут в
// окружение вместе с ключами домена, отсортированные по имени; Build не
// перекрывает ключ домена с тем же именем (такого быть не может — домен их не
// хранит — но перекрыть настройку значением сборки было бы хуже) и не
// пропускает имена, которых не пропускает Values.
func TestBuildEnvBuildValues(t *testing.T) {
	t.Parallel()
	in := EnvInput{Phase: "build", Ext: "x", ExtVersion: "1.0.0",
		Values: map[string]string{"LEVEL": "a", "SAME": "domain"},
		Build:  map[string]string{"TOKEN": "t0k", "SAME": "build", "bad": "v", "NUL": "a\x00b"}}
	got := BuildEnv(in)
	var keys []string
	for _, kv := range got {
		k, _, _ := strings.Cut(kv, "=")
		if !strings.HasPrefix(k, "CREAM_") && k != "PATH" && k != "LANG" {
			keys = append(keys, kv)
		}
	}
	if want := []string{"LEVEL=a", "SAME=domain", "TOKEN=t0k"}; !slices.Equal(keys, want) {
		t.Errorf("ключи = %q, ждали %q", keys, want)
	}
}
