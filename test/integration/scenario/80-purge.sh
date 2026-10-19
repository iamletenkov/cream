#!/usr/bin/env bash
# Конец жизненного цикла: Р11 — remove --purge B удаляет каталог B вместе с
# чужим файлом A, запись A становится висячей и remove A это переживает;
# снятие пилота по зависимостям; apt purge cream при пустом реестре удаляет
# /etc/cream, /var/lib/cream и /var/cache/cream (§15).
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"
A=fixture-agent-like B=fixture-teleport-like D=fixture-docker-like

step "Р11: purge B при установленном A"
c remove --purge $B
want_rc 67 "remove B, от которого зависит A"
c remove --purge --force $B
want_rc 0 "remove --purge --force B"
absent /etc/cream/$B "каталог B удалён целиком (с файлом A)"
absent /etc/systemd/system/fixture-teleport.service "юнит B убран"
eq "$(systemctl is-active fixture-teleport.service 2>/dev/null || true)" inactive "служба B остановлена"
c remove $A
want_rc 0 "remove A с висячей записью файла"
absent /var/lib/cream/installed/$A.json "запись A снята"

step "снятие остального"
c remove --purge $D
want_rc 0 "remove --purge $D"
absent /etc/systemd/system/fixture-dockerd.service "юнит из files убран"
absent /etc/systemd/system/multi-user.target.wants/cream-configure@$D.service "configure выключен"
absent "/etc/systemd/system/cream-configure@$D.service.d" "drop-in стадии убран"
c remove --purge cloud-user
want_rc 0 "remove --purge cloud-user"
c remove --purge $A
want_rc 66 "A уже снят"
c list
want_rc 0 "cream list"
eq "$(j '.result.extensions | length')" 0 "реестр пуст"

step "apt purge cream"
apt-get purge -y cream >/tmp/purge.log 2>&1 || {
	cat /tmp/purge.log
	fail "apt-get purge cream"
}
ok "apt-get purge cream"
for p in /usr/bin/cream /usr/lib/systemd/system/cream-configure@.service /etc/cream /var/lib/cream /var/cache/cream; do
	absent "$p" "после purge: $p"
done
