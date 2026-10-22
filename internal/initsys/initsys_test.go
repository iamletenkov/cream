package initsys

import (
	"testing"

	"github.com/iamletenkov/cream/internal/paths"
)

func TestFor(t *testing.T) {
	t.Parallel()
	if got := For(paths.Layout{}).Name(); got != "systemd" {
		t.Errorf("deb: %s", got)
	}
	ow := paths.Layout{Root: t.TempDir(), Family: paths.FamilyOpenWrt}
	s := For(ow)
	if s.Name() != "procd" || s.Unit() != "cream-configure" {
		t.Fatalf("openwrt: %s %s", s.Name(), s.Unit())
	}
	if _, err := s.Enable(ow, "demo", StageSpec{Stage: "system", After: []string{"x.service"}}); err == nil {
		t.Error("procd Enable with after: want refusal")
	}
	target, err := s.Enable(ow, "demo", StageSpec{Stage: "cloud-user"})
	if err != nil || target != "S95cream-configure" {
		t.Fatalf("Enable = %q, %v", target, err)
	}
	if got, _ := s.Target(ow, "early"); got != "S19cream-configure-early" {
		t.Errorf("Target(early) = %q", got)
	}
}
