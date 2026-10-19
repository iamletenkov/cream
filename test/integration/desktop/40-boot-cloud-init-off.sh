#!/usr/bin/env bash
# it: reboot-before
# §6.4, cream 1.1.1 (блокер ревью 1.1.0): загрузка с установленным, но
# выключенным cloud-init — cloud-final не запускается, cloud-init.target не
# достигается, а configure стадии cloud-user всё равно отрабатывает на загрузке
# (CREAM_REASON=boot), без «ordering cycle»; прочие стадии — как обычно.
# shellcheck source=../scenario/lib.sh
. /it/scenario/lib.sh
p() { systemctl show -p "$2" --value "$1"; }

step "загрузка без cloud-init"
journalctl -b --no-pager >/tmp/boot.log
eq "$(grep -c 'ordering cycle' /tmp/boot.log || true)" 0 "ordering cycle в journalctl -b"
eq "$(p cloud-final.service ActiveState)" inactive "cloud-final не запускался"
eq "$(p cloud-init.target ActiveState)" inactive "cloud-init.target не достигнута"
eq "$(p cream-configure-cloud-user.service ActiveState)" active "cream-configure-cloud-user.service active"
for e in configure-cloud-user configure-graphical desktop-like; do
	eq "$(p "cream-configure@$e.service" ActiveState)" active "cream-configure@$e active"
	eq "$(p "cream-configure@$e.service" Result)" success "cream-configure@$e: success"
	before=$(cat "/var/tmp/it-$e.lines")
	[ "$(count "$STATE/$e/configure.log")" -gt "$before" ] || fail "$e: configure на этой загрузке не запускался"
	eq "$(tail -n 1 "$STATE/$e/configure.log")" "phase=live reason=boot version=1.0.0" "$e: configure на загрузке"
done
