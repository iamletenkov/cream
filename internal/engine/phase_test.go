package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/paths"
)

// fakePID1 кладёт под root подменённые /proc/1/comm (comm == "" — /proc нет)
// и /run/systemd/system (run — каталог есть).
func fakePID1(t *testing.T, root, comm string, run bool) {
	t.Helper()
	l := paths.Layout{Root: root}
	if comm != "" {
		if err := os.MkdirAll(filepath.Dir(l.PID1Comm()), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(l.PID1Comm(), []byte(comm+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if run {
		if err := os.MkdirAll(l.SystemdRunning(), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDetectPhase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		comm       string
		run        bool
		want       PhaseInfo
		wantRefuse bool
	}{
		{name: "systemd и /run/systemd/system — live", comm: "systemd", run: true,
			want: PhaseInfo{Phase: PhaseLive, Reason: "pid1=systemd"}},
		{name: "init appliance libguestfs — build", comm: "init",
			want: PhaseInfo{Phase: PhaseBuild, Reason: "pid1=init"}},
		{name: "не systemd при /run/systemd/system — build", comm: "bash", run: true,
			want: PhaseInfo{Phase: PhaseBuild, Reason: "pid1=bash"}},
		{name: "нет /proc — отказ", wantRefuse: true},
		{name: "нет /proc при /run/systemd/system — отказ", run: true, wantRefuse: true},
		{name: "systemd без /run/systemd/system — отказ, не догадка", comm: "systemd", wantRefuse: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			fakePID1(t, root, tc.comm, tc.run)
			got, err := DetectPhase(paths.Layout{Root: root})
			if tc.wantRefuse {
				wantCode(t, err, cerr.ExitUsage, "phase.undetermined")
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("DetectPhase = %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
}

func TestDetectPhaseEmptyComm(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fakePID1(t, root, "systemd", false)
	l := paths.Layout{Root: root}
	if err := os.WriteFile(l.PID1Comm(), []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := DetectPhase(l)
	wantCode(t, err, cerr.ExitUsage, "phase.undetermined")
}

func TestResolvePhase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		flag, env string
		comm      string
		run       bool
		want      PhaseInfo
		wantCode  string
	}{
		{name: "флаг build побеждает автоопределение live", flag: "build", comm: "systemd", run: true,
			want: PhaseInfo{Phase: PhaseBuild, Reason: PhaseReasonFlag}},
		{name: "флаг live побеждает CREAM_PHASE build", flag: "live", env: "build",
			want: PhaseInfo{Phase: PhaseLive, Reason: PhaseReasonFlag}},
		{name: "флаг побеждает и отсутствие /proc", flag: "live",
			want: PhaseInfo{Phase: PhaseLive, Reason: PhaseReasonFlag}},
		{name: "CREAM_PHASE build побеждает автоопределение live", env: "build", comm: "systemd", run: true,
			want: PhaseInfo{Phase: PhaseBuild, Reason: PhaseReasonEnv}},
		{name: "CREAM_PHASE live побеждает автоопределение build", env: "live", comm: "init",
			want: PhaseInfo{Phase: PhaseLive, Reason: PhaseReasonEnv}},
		{name: "автоопределение", comm: "init",
			want: PhaseInfo{Phase: PhaseBuild, Reason: "pid1=init"}},
		{name: "чужое CREAM_PHASE не в счёт", env: "stage", comm: "systemd", run: true,
			want: PhaseInfo{Phase: PhaseLive, Reason: "pid1=systemd"}},
		{name: "неизвестный флаг — 64", flag: "stage", comm: "systemd", run: true, wantCode: "usage.phase"},
		{name: "нет /proc — 64", wantCode: "phase.undetermined"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			te := newTestEngine(t)
			fakePID1(t, te.Root, tc.comm, tc.run)
			if tc.env != "" {
				te.EnvVars["CREAM_PHASE"] = tc.env
			}
			got, err := te.ResolvePhase(tc.flag)
			if tc.wantCode != "" {
				wantCode(t, err, cerr.ExitUsage, tc.wantCode)
				if te.Phase() != PhaseLive {
					t.Errorf("Phase() после отказа = %q", te.Phase())
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("ResolvePhase(%q) = %+v, %v; want %+v", tc.flag, got, err, tc.want)
			}
			if te.Phase() != tc.want.Phase {
				t.Errorf("Phase() = %q, want %q", te.Phase(), tc.want.Phase)
			}
		})
	}
}

// Без ResolvePhase Phase() — прежнее правило: CREAM_PHASE родителя, иначе live
// (configure при загрузке, действия, вложенный cream set из хука).
func TestPhaseUnresolved(t *testing.T) {
	t.Parallel()
	te := newTestEngine(t)
	fakePID1(t, te.Root, "init", false)
	if got := te.Phase(); got != PhaseLive {
		t.Errorf("Phase() = %q, want live", got)
	}
	te.EnvVars["CREAM_PHASE"] = PhaseBuild
	if got := te.Phase(); got != PhaseBuild {
		t.Errorf("Phase() = %q, want build", got)
	}
}
