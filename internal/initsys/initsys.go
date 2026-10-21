// Package initsys picks how configure is wired into boot (spec OpenWrt
// §4.1, ADR-0005): systemd units on the Debian layout, procd init scripts on
// OpenWrt. The choice follows the layout family, not a flag.
package initsys

import (
	"github.com/iamletenkov/cream/internal/paths"
	"github.com/iamletenkov/cream/internal/procd"
	"github.com/iamletenkov/cream/internal/systemd"
)

// StageSpec is the configure hook's stage and extra ordering from the manifest.
type StageSpec = systemd.StageSpec

// System is the boot side of configure.
type System interface {
	// Name is the init system: systemd or procd.
	Name() string
	// Unit is what `cream system init` reports as the configure unit.
	Unit() string
	// Init prepares the machine: directories, the unit or the init scripts.
	Init(l paths.Layout) error
	// Target is where the configure of stage is hooked (configure.target).
	Target(l paths.Layout, stage string) (string, error)
	// Enable hooks the configure of ext into boot and returns the target.
	Enable(l paths.Layout, ext string, st StageSpec) (string, error)
	// Disable takes the configure of ext out of boot.
	Disable(l paths.Layout, ext string) error
}

// For returns the init system of the layout.
func For(l paths.Layout) System {
	if l.OpenWrt() {
		return procdSys{}
	}
	return systemdSys{}
}

type systemdSys struct{}

func (systemdSys) Name() string                                    { return "systemd" }
func (systemdSys) Unit() string                                    { return "cream-configure@.service" }
func (systemdSys) Init(l paths.Layout) error                       { return systemd.Init(l) }
func (systemdSys) Target(l paths.Layout, s string) (string, error) { return systemd.Target(l, s) }
func (systemdSys) Disable(l paths.Layout, ext string) error        { return systemd.Disable(l, ext) }
func (systemdSys) Enable(l paths.Layout, ext string, st StageSpec) (string, error) {
	return systemd.Enable(l, ext, st)
}

type procdSys struct{}

func (procdSys) Name() string                                    { return "procd" }
func (procdSys) Unit() string                                    { return procd.Script }
func (procdSys) Init(l paths.Layout) error                       { return procd.Init(l) }
func (procdSys) Target(_ paths.Layout, s string) (string, error) { return procd.Target(s) }
func (procdSys) Disable(l paths.Layout, ext string) error        { return procd.Disable(l, ext) }
func (procdSys) Enable(l paths.Layout, ext string, st StageSpec) (string, error) {
	return procd.Enable(l, ext, st.Stage, len(st.After)+len(st.Before)+len(st.Wants) > 0)
}
