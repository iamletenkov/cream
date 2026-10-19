#!/usr/bin/env bash
# it: reboot-before
# §6.4, cream 1.1.0: на загрузке рабочего стола в облаке cloud-init отработал,
# configure стадии cloud-user — после cloud-final, graphical — после менеджера
# входа, «desktop» (system) — до него; в журнале загрузки нет «ordering cycle».
# shellcheck source=../scenario/lib.sh
. /it/scenario/lib.sh
CU=configure-cloud-user G=configure-graphical D=desktop-like
p() { systemctl show -p "$2" --value "$1"; }

step "загрузка"
journalctl -b --no-pager >/tmp/boot.log
eq "$(grep -c 'ordering cycle' /tmp/boot.log || true)" 0 "ordering cycle в journalctl -b"
eq "$(p cloud-final.service ActiveState)" active "cloud-final отработал"
eq "$(p cloud-init.target ActiveState)" active "cloud-init.target достигнута"
eq "$(p cream-configure-cloud-user.service ActiveState)" active "cream-configure-cloud-user.service active"
for e in $CU $G $D; do
	eq "$(p "cream-configure@$e.service" ActiveState)" active "cream-configure@$e active"
	eq "$(tail -n 1 "$STATE/$e/configure.log")" "phase=live reason=boot version=1.0.0" "$e: configure на загрузке"
done

step "порядок"
cf=$(p cloud-final.service ExecMainExitTimestampMonotonic)
cu=$(p "cream-configure@$CU.service" ExecMainStartTimestampMonotonic)
[ "$cf" -gt 0 ] && [ "$cu" -ge "$cf" ] || fail "cloud-user стартовал ($cu) не после cloud-final ($cf)"
ok "cloud-user стартовал после завершения cloud-final"
dm=$(p gdm.service InactiveExitTimestampMonotonic)
gr=$(p "cream-configure@$G.service" ExecMainStartTimestampMonotonic)
dk=$(p "cream-configure@$D.service" ExecMainExitTimestampMonotonic)
[ "$dm" -gt 0 ] || fail "gdm не запускался"
[ "$gr" -ge "$dm" ] || fail "graphical стартовал ($gr) раньше менеджера входа ($dm)"
ok "graphical стартовал после запуска менеджера входа"
[ "$dk" -gt 0 ] && [ "$dk" -le "$dm" ] || fail "desktop завершился ($dk) не до менеджера входа ($dm)"
ok "desktop (system, before DM) завершился до менеджера входа"
