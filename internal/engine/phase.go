package engine

import (
	"os"
	"strings"

	"github.com/iamletenkov/cream/internal/cerr"
	"github.com/iamletenkov/cream/internal/paths"
)

// PhaseInfo is the phase of an invocation and why it was chosen (§10.1): the
// --json envelope and -v name both (phase: build, reason: pid1=init).
type PhaseInfo struct {
	Phase  string
	Reason string
}

// Reasons of a phase that did not come from PID 1.
const (
	// PhaseReasonFlag — the --phase flag.
	PhaseReasonFlag = "flag"
	// PhaseReasonEnv — CREAM_PHASE of a parent cream (a nested call from a hook).
	PhaseReasonEnv = "env"
)

// ResolvePhase decides the phase of install, upgrade and remove (§10.1): the
// flag (--phase, "" — not given), else CREAM_PHASE of a parent cream, else
// DetectPhase. Phase() returns the decision afterwards. An unknown flag value
// is 64 usage.phase; an undecidable machine is 64 phase.undetermined.
func (e *Engine) ResolvePhase(flag string) (PhaseInfo, error) {
	var p PhaseInfo
	switch {
	case flag != "":
		if !validPhase(flag) {
			ce := cerr.New(cerr.ExitUsage, "usage.phase", "--phase %q: ожидается build или live", flag)
			ce.Details = map[string]any{"phase": flag}
			return PhaseInfo{}, ce
		}
		p = PhaseInfo{Phase: flag, Reason: PhaseReasonFlag}
	case e.Env != nil && validPhase(e.Env("CREAM_PHASE")):
		p = PhaseInfo{Phase: e.Env("CREAM_PHASE"), Reason: PhaseReasonEnv}
	default:
		var err error
		if p, err = DetectPhase(e.L); err != nil {
			return PhaseInfo{}, err
		}
	}
	e.phase = p.Phase
	return p, nil
}

func validPhase(p string) bool { return p == PhaseBuild || p == PhaseLive }

// DetectPhase decides the phase by PID 1 (deb family, §10.1): systemd with
// /run/systemd/system is live; any other readable PID 1 is build (in
// virt-customize it is the libguestfs appliance init, /proc bound into the
// chroot; a container without systemd is build too — services do not start
// there). Without a readable /proc/1/comm, and for systemd without
// /run/systemd/system (a chroot with the host's /proc), it refuses with 64
// phase.undetermined instead of guessing.
func DetectPhase(l paths.Layout) (PhaseInfo, error) {
	b, err := os.ReadFile(l.PID1Comm())
	comm := strings.TrimSpace(string(b))
	if err != nil || comm == "" {
		ce := cerr.New(cerr.ExitUsage, "phase.undetermined",
			"фаза не определена: %s не прочитан (/proc не смонтирован?) — задайте --phase build или --phase live", l.PID1Comm())
		ce.Details = map[string]any{"path": l.PID1Comm()}
		return PhaseInfo{}, ce
	}
	reason := "pid1=" + comm
	if comm != "systemd" {
		return PhaseInfo{Phase: PhaseBuild, Reason: reason}, nil
	}
	if fi, err := os.Stat(l.SystemdRunning()); err == nil && fi.IsDir() {
		return PhaseInfo{Phase: PhaseLive, Reason: reason}, nil
	}
	ce := cerr.New(cerr.ExitUsage, "phase.undetermined",
		"фаза не определена: PID 1 — systemd, но %s нет (chroot с /proc хоста?) — задайте --phase build или --phase live",
		l.SystemdRunning())
	ce.Details = map[string]any{"pid1": comm, "path": l.SystemdRunning()}
	return PhaseInfo{}, ce
}
