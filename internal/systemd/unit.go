// Package systemd places the configure unit of cream (spec §6.4): the
// built-in template cream-configure@.service, the per-extension stage drop-in
// and the link in the target's .wants directory. All of it works on files
// under paths.Layout.Root, without a running systemd, so the build phase
// enables instances the same way a live machine does. Talking to a running
// systemd goes through Systemctl, which only calls an injected ExecFunc.
package systemd

import (
	_ "embed" // the unit template is part of the binary (spec §11.2, system init)
)

//go:embed cream-configure@.service
var unitTemplate []byte

//go:embed cream-configure-cloud-user.service
var cloudUserUnit []byte

// CloudUserUnit pulls the configure instances of the cloud-user stage (§6.4,
// cream 1.1.1): they are linked into its .wants, and it into
// multi-user.target.wants.
const CloudUserUnit = "cream-configure-cloud-user.service"

// CloudUserUnitFile returns a copy of the built-in cream-configure-cloud-user.service.
func CloudUserUnitFile() []byte {
	return append([]byte(nil), cloudUserUnit...)
}

// cloudUserLink is where the multi-user.target.wants link of CloudUserUnit
// points (on the machine itself, as templateTarget).
const cloudUserLink = "/usr/lib/systemd/system/" + CloudUserUnit

// UnitTemplate returns a copy of the built-in cream-configure@.service.
func UnitTemplate() []byte {
	return append([]byte(nil), unitTemplate...)
}

// UnitName is the configure instance of ext, cream-configure@<ext>.service,
// as passed to Systemctl.Restart.
func UnitName(ext string) string { return "cream-configure@" + ext + ".service" }

// templateTarget is where an instance link points: the template on the
// machine itself, never a path under --root (the link is read by the guest).
const templateTarget = "/usr/lib/systemd/system/cream-configure@.service"
