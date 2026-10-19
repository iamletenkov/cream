#!/usr/bin/env bash
# §6.4, cream 1.1.0: после перезагрузки (60-after-boot) configure стадий early,
# cloud-user и graphical отработал с CREAM_REASON=boot в порядке своих стадий,
# в журнале загрузки нет «ordering cycle»; remove снимает экземпляры.
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"
W=/etc/systemd/system
E=configure-early CU=configure-cloud-user G=configure-graphical
mono() { systemctl show -p "$2" --value "$1"; }

step "configure стадий на загрузке"
eq "$(systemctl get-default)" graphical.target "цель по умолчанию — graphical.target"
for e in $E $CU $G; do
	eq "$(mono "cream-configure@$e.service" ActiveState)" active "cream-configure@$e active"
	eq "$(mono "cream-configure@$e.service" Result)" success "cream-configure@$e: success"
	eq "$(tail -n 1 "$STATE/$e/configure.log")" "phase=live reason=boot version=1.0.0" "$e: configure на загрузке"
done

step "порядок: early до sysinit.target и network-pre.target"
early_done=$(mono "cream-configure@$E.service" ExecMainExitTimestampMonotonic)
[ "$early_done" -gt 0 ] || fail "early: нет времени завершения"
for t in sysinit.target network-pre.target; do
	at=$(mono $t ActiveEnterTimestampMonotonic)
	[ "$at" -gt 0 ] && [ "$early_done" -le "$at" ] || fail "early завершился ($early_done) не раньше $t ($at)"
	ok "early завершился раньше $t"
done
# Порядок cloud-user и graphical рядом с настоящими cloud-final и менеджером
# входа — набор desktop (ubuntu-24.04): здесь их нет, After= на них пуст.

step "журнал загрузки без циклов"
eq "$(journalctl -b --no-pager | grep -c 'ordering cycle' || true)" 0 "ordering cycle в journalctl -b"

step "remove снимает экземпляры стадий"
for pair in "$E sysinit.target" "$CU multi-user.target" "$G graphical.target"; do
	set -- $pair
	c remove "$1"
	want_rc 0 "remove $1"
	absent "$W/$2.wants/cream-configure@$1.service" "$1: ссылка в $2.wants снята"
	absent "$W/cream-configure@$1.service.d" "$1: drop-in снят"
done
