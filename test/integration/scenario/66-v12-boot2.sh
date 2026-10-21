#!/usr/bin/env bash
# it: reboot-before
# cream 1.2.0, вторая загрузка (спека §10.7): отметка cfg-once совпала — хук
# не запускался, юнит успешен, в журнале «пропуск»; every_boot, падающий,
# код 20 и повтор из хука — снова; configure --force — мимо отметки; remove
# снимает отметку, фикстуры 1.2 сняты (80-purge ждёт пустой реестр).
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"
# count_ge <файл счётчика> <N> — для wait_until: счётчик хука ≥ N.
count_ge() { [ "$(counter "$1")" -ge "$2" ]; }
MARKS=/var/lib/cream/configured
mono() { systemctl show -p "$2" --value "$1"; }
settled() { [ "$(mono "cream-configure@$1.service" ActiveState)" != activating ]; }

step "вторая загрузка: configure один раз"
for e in cfg-once cfg-every-boot cfg-failing cfg-skip cfg-again; do
	wait_until 60 settled $e || fail "cream-configure@$e не завершился"
done
wait_until 30 count_ge $STATE/cfg-again/configure.count 6 ||
	fail "cfg-again: повтор на загрузке не пришёл ($(counter $STATE/cfg-again/configure.count))"
sleep 2
eq "$(counter $STATE/cfg-once/configure.count)" 4 "cfg-once: отметка совпала — хук не запускался"
eq "$(mono cream-configure@cfg-once.service ActiveState)" active "cream-configure@cfg-once active"
eq "$(mono cream-configure@cfg-once.service Result)" success "cream-configure@cfg-once: success"
journalctl -b --no-pager -u cream-configure@cfg-once.service >/tmp/cfg-once.journal 2>&1 || true
has /tmp/cfg-once.journal 'configure cfg-once: пропуск, отметка совпала' "журнал: пропуск по отметке"
eq "$(counter $STATE/cfg-every-boot/configure.count)" 3 "every_boot: снова"
eq "$(counter $STATE/cfg-failing/configure.count)" 3 "падающий: снова"
eq "$(counter $STATE/cfg-skip/configure.count)" 3 "код 20: снова"
eq "$(counter $STATE/cfg-again/configure.count)" 6 "cfg-again: загрузка и повтор снова"

step "configure --force: мимо отметки"
at_before=$(jq -r .at $MARKS/cfg-once.json)
sleep 1
c configure --force cfg-once
want_rc 0 "configure --force cfg-once"
wait_until 20 count_ge $STATE/cfg-once/configure.count 5 || fail "--force: configure не запущен"
wait_until 20 settled cfg-once || fail "cream-configure@cfg-once не завершился"
eq "$(counter $STATE/cfg-once/configure.count)" 5 "--force: ровно один прогон"
exists $MARKS/cfg-once.json "--force: отметка есть"
[ "$(jq -r .at $MARKS/cfg-once.json)" != "$at_before" ] || fail "--force: отметка не переписана ($at_before)"
ok "--force: отметка переписана"

step "remove снимает отметку; фикстуры 1.2 сняты"
for e in cfg-once cfg-every-boot cfg-again; do exists $MARKS/$e.json "$e: отметка до remove"; done
for e in cfg-once cfg-every-boot cfg-failing cfg-skip cfg-again build-token; do
	c remove --purge $e
	want_rc 0 "remove --purge $e"
	absent $MARKS/$e.json "$e: отметки нет"
	systemctl reset-failed "cream-configure@$e.service" 2>/dev/null || true
done
c source remove v12
want_rc 0 "source remove v12"
