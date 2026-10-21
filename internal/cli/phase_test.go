package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// phaseEnvelope — поля конверта --json, которые решает фаза (§10.1).
type phaseEnvelope struct {
	OK     bool   `json:"ok"`
	Phase  string `json:"phase"`
	Reason string `json:"reason"`
	Error  *struct {
		Code string `json:"code"`
	} `json:"error"`
}

func decodePhaseEnvelope(t *testing.T, out cliOut) phaseEnvelope {
	t.Helper()
	var env phaseEnvelope
	if err := json.Unmarshal([]byte(out.Stdout), &env); err != nil {
		t.Fatalf("конверт: %v\n%s\n%s", err, out.Stdout, out.Stderr)
	}
	return env
}

// noPhaseEnv — окружение без CREAM_PHASE: фаза определяется сама.
var noPhaseEnv = map[string]string{"CREAM_PHASE": ""}

// pid1 подменяет /proc/1/comm и /run/systemd/system под root.
func pid1(t *testing.T, root, comm string, run bool) {
	t.Helper()
	if comm != "" {
		writeRootFile(t, root, "/proc/1/comm", comm+"\n", 0o644)
	}
	if run {
		writeRootFile(t, root, "/run/systemd/system/.keep", "", 0o644)
	}
}

func TestPhaseAutodetectCLI(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		comm       string
		run        bool
		env        map[string]string
		args       []string
		wantExit   int
		wantPhase  string
		wantReason string
		wantCode   string
	}{
		{name: "живая машина — live", comm: "systemd", run: true, env: noPhaseEnv,
			args: []string{"install", "--json", "noop"}, wantPhase: "live", wantReason: "pid1=systemd"},
		{name: "нет /proc — 64 phase.undetermined", env: noPhaseEnv,
			args: []string{"install", "--json", "noop"}, wantExit: 64, wantCode: "phase.undetermined"},
		{name: "upgrade: нет /proc — 64", env: noPhaseEnv,
			args: []string{"upgrade", "--json", "noop"}, wantExit: 64, wantCode: "phase.undetermined"},
		{name: "remove: нет /proc — 64", env: noPhaseEnv,
			args: []string{"remove", "--json", "noop"}, wantExit: 64, wantCode: "phase.undetermined"},
		{name: "флаг побеждает отсутствие /proc", env: noPhaseEnv,
			args: []string{"install", "--json", "--phase", "live", "noop"}, wantPhase: "live", wantReason: "flag"},
		{name: "флаг live побеждает CREAM_PHASE build", env: map[string]string{"CREAM_PHASE": "build"},
			args: []string{"install", "--json", "--phase", "live", "noop"}, wantPhase: "live", wantReason: "flag"},
		{name: "CREAM_PHASE родителя побеждает автоопределение", comm: "init",
			env:  map[string]string{"CREAM_PHASE": "live"},
			args: []string{"install", "--json", "noop"}, wantPhase: "live", wantReason: "env"},
		{name: "remove: CREAM_PHASE build — фаза в конверте и при ошибке", comm: "systemd", run: true,
			env: map[string]string{"CREAM_PHASE": "build"}, args: []string{"remove", "--json", "noop"},
			wantExit: 66, wantPhase: "build", wantReason: "env", wantCode: "not_found.extension"},
		{name: "upgrade: PID 1 не systemd — build", comm: "init", env: noPhaseEnv,
			args: []string{"upgrade", "--json", "--dry-run", "--all"}, wantPhase: "build", wantReason: "pid1=init"},
		{name: "remove: неизвестная фаза — 64 usage.phase",
			args: []string{"remove", "--json", "--phase", "stage", "noop"}, wantExit: 64, wantCode: "usage.phase"},
		{name: "upgrade: неизвестная фаза — 64 usage.phase",
			args: []string{"upgrade", "--json", "--phase", "stage", "noop"}, wantExit: 64, wantCode: "usage.phase"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := installCLIRoot(t)
			pid1(t, root, tc.comm, tc.run)
			out := runCLI(t, cliIn{Root: root, Env: tc.env}, tc.args...)
			if out.Code != tc.wantExit {
				t.Fatalf("exit = %d, want %d\n%s\n%s", out.Code, tc.wantExit, out.Stdout, out.Stderr)
			}
			env := decodePhaseEnvelope(t, out)
			if env.Phase != tc.wantPhase || env.Reason != tc.wantReason {
				t.Errorf("phase/reason = %q/%q, want %q/%q", env.Phase, env.Reason, tc.wantPhase, tc.wantReason)
			}
			code := ""
			if env.Error != nil {
				code = env.Error.Code
			}
			if code != tc.wantCode {
				t.Errorf("error.code = %q, want %q", code, tc.wantCode)
			}
		})
	}
}

// Отказ 64 phase.undetermined в тексте называет выход — флаг --phase.
func TestPhaseUndeterminedText(t *testing.T) {
	t.Parallel()
	root := installCLIRoot(t)
	out := runCLI(t, cliIn{Root: root, Env: noPhaseEnv}, "install", "noop")
	if out.Code != 64 || !strings.Contains(out.Stderr, "--phase") {
		t.Errorf("%+v", out)
	}
}

// Фаза и причина — строкой в stderr (журнал сборки), stdout не тронут; -q — без строки.
func TestPhaseStderr(t *testing.T) {
	t.Parallel()
	root := installCLIRoot(t)
	pid1(t, root, "systemd", true)
	out := runCLI(t, cliIn{Root: root, Env: noPhaseEnv}, "install", "noop")
	if out.Code != 0 || !strings.Contains(out.Stderr, "cream: фаза live (pid1=systemd)\n") || strings.Contains(out.Stdout, "фаза") {
		t.Errorf("%+v", out)
	}
	out = runCLI(t, cliIn{Root: root, Env: noPhaseEnv}, "remove", "-q", "noop")
	if out.Code != 0 || strings.Contains(out.Stderr, "фаза") {
		t.Errorf("-q: %+v", out)
	}
}

// Команды без фазы не получают phase/reason в конверте.
func TestPhaseNotInOtherEnvelopes(t *testing.T) {
	t.Parallel()
	out := runCLI(t, cliIn{Root: installCLIRoot(t)}, "list", "--json")
	if strings.Contains(out.Stdout, `"phase"`) || strings.Contains(out.Stdout, `"reason"`) {
		t.Errorf("%s", out.Stdout)
	}
}
