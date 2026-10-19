#!/usr/bin/env bash
# it: reboot-before
# §14 шаг 4, §6.4: после docker restart (run.sh) cream-configure@<ext>
# отработал на загрузке стадией system — после своей службы, с CREAM_REASON=
# boot и значением, записанным раньше с --no-apply; служба модели teleport
# поднялась с render из ExecStartPre. Сбой configure — failed юнит и запись
# в history.jsonl, реестр не меняется.
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"
D=fixture-docker-like T=fixture-teleport-like

step "загрузка"
# Упавшие юниты среды (journald под qemu-user и т. п.) — не предмет теста;
# упавших юнитов cream и расширений быть не должно.
systemctl --failed --no-legend --plain >/tmp/failed-boot.txt
hasnt /tmp/failed-boot.txt '(cream-|fixture-)' "на загрузке нет упавших юнитов cream и расширений"
eq "$(systemctl show -p ActiveState --value cream-configure@$D.service)" active "cream-configure@$D active"
eq "$(systemctl show -p Result --value cream-configure@$D.service)" success "cream-configure@$D: success"
eq "$(tail -n 1 $STATE/$D/configure.log)" "reason=boot user=$CLOUD_USER level=warn dockerd=active" \
	"configure на загрузке: reason=boot, значение после --no-apply, служба уже поднята"
absent /etc/systemd/system/multi-user.target.wants/cream-configure@$T.service \
	"у расширения без configure экземпляр не включён"
eq "$(systemctl show -p ActiveState --value cream-configure@$T.service)" inactive "и не запускался"
eq "$(systemctl is-active fixture-teleport.service)" active "служба модели teleport поднялась"
eq "$(tail -n 1 $STATE/$T/render.log)" "phase=live labels=env=stage" "render из ExecStartPre на загрузке"
c run $T managed-status
want_rc 0 "managed-status после загрузки"
eq "$(j '.result.output.rendered')" env=stage "метки пережили перезагрузку"

step "сбой configure — failed юнит и history.jsonl, реестр не меняется"
reg_before=$(sha256sum /var/lib/cream/installed/$D.json)
home=$(getent passwd "$CLOUD_USER" | cut -d: -f6)
mv "$home" "$home.away"
if systemctl restart cream-configure@$D.service 2>/dev/null; then fail "configure без пользователя прошёл"; fi
eq "$(systemctl show -p ActiveState --value cream-configure@$D.service)" failed "cream-configure@$D failed"
systemctl --failed --no-legend --plain >/tmp/failed.txt
has /tmp/failed.txt "cream-configure@$D" "виден в systemctl --failed"
eq "$(hist "[.[] | select(.op == \"configure\" and .ext == \"$D\")] | .[-1].status")" failed \
	"history.jsonl: configure failed"
eq "$(sha256sum /var/lib/cream/installed/$D.json)" "$reg_before" "реестр не изменился"
mv "$home.away" "$home"
systemctl reset-failed cream-configure@$D.service
systemctl restart cream-configure@$D.service
eq "$(systemctl show -p ActiveState --value cream-configure@$D.service)" active "после восстановления — active"
eq "$(tail -n 1 $STATE/$D/configure.log)" "reason=boot user=$CLOUD_USER level=warn dockerd=active" \
	"перезапуск юнита без маркера — reason=boot"
