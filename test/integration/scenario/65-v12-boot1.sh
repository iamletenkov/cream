#!/usr/bin/env bash
# it: reboot-before
# cream 1.2.0, первая загрузка после 64-v12-install (спека §10.7, §10.8): отметка
# cfg-once не совпала (set --no-apply) — configure прошёл с reason=boot;
# every_boot, падающий, код 20 — хук прошёл; cfg-again — прогон загрузки и
# повтор followup в ту же загрузку. Затем ротация секрета (--no-apply) и
# cream configure --again вручную — мимо совпавшей отметки.
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"
# count_ge <файл счётчика> <N> — для wait_until: счётчик хука ≥ N.
count_ge() { [ "$(counter "$1")" -ge "$2" ]; }
MARKS=/var/lib/cream/configured
mono() { systemctl show -p "$2" --value "$1"; }
settled() { [ "$(mono "cream-configure@$1.service" ActiveState)" != activating ]; }

step "загрузка: configure по отметке"
for e in cfg-once cfg-every-boot cfg-failing cfg-skip cfg-again; do
	wait_until 60 settled $e || fail "cream-configure@$e не завершился"
done
wait_until 30 count_ge $STATE/cfg-again/configure.count 4 ||
	fail "cfg-again: повтор на загрузке не пришёл ($(counter $STATE/cfg-again/configure.count))"
sleep 2
eq "$(counter $STATE/cfg-once/configure.count)" 3 "cfg-once: отметка не совпала — configure на загрузке"
eq "$(tail -n 1 $STATE/cfg-once/configure.log)" "reason=boot phase=live" "cfg-once: причина boot"
eq "$(cat $STATE/cfg-once/message)" bye "cfg-once: значение после --no-apply"
eq "$(mono cream-configure@cfg-once.service Result)" success "cream-configure@cfg-once: success"
c show cfg-once
eq "$(j .result.configured.matches)" true "отметка переписана и совпала"
eq "$(counter $STATE/cfg-every-boot/configure.count)" 2 "every_boot: configure на загрузке"
eq "$(counter $STATE/cfg-failing/configure.count)" 2 "падающий: повтор на загрузке"
eq "$(mono cream-configure@cfg-failing.service Result)" exit-code "cream-configure@cfg-failing упал"
absent $MARKS/cfg-failing.json "падающий: отметки нет"
eq "$(hist '[.[] | select(.op == "configure" and .ext == "cfg-failing" and .status == "failed")] | length')" 2 \
	"history.jsonl: каждая попытка cfg-failing — failed"
eq "$(counter $STATE/cfg-skip/configure.count)" 2 "код 20: повтор на загрузке"
absent $MARKS/cfg-skip.json "код 20: отметки нет"
eq "$(mono cream-configure@cfg-skip.service Result)" success "код 20: юнит успешен"
eq "$(hist '[.[] | select(.op == "configure" and .ext == "cfg-skip" and .status == "skipped")] | length')" 2 \
	"history.jsonl: каждый пропуск cfg-skip — skipped"
eq "$(counter $STATE/cfg-again/configure.count)" 4 "cfg-again: загрузка и повтор"
eq "$(tail -n 2 $STATE/cfg-again/configure.log | paste -sd' ')" "reason=boot phase=live reason=followup phase=live" \
	"cfg-again: boot, затем followup в ту же загрузку"
systemctl reset-failed cream-configure@cfg-failing.service 2>/dev/null || true

step "ротация секрета: secrets_rev, отметка расходится"
rev=$(reg cfg-once '.secrets_rev // 0')
c set --no-apply cfg-once PASS=- < <(printf 'it-pass-%s\n' "$(od -An -N6 -tx1 /dev/urandom | tr -d ' \n')")
want_rc 0 "set --no-apply PASS=-"
eq "$(reg cfg-once .secrets_rev)" "$((rev + 1))" "secrets_rev вырос"
c show cfg-once
eq "$(j .result.configured.matches)" false "после ротации отметка не совпадает"
eq "$(counter $STATE/cfg-once/configure.count)" 3 "--no-apply — configure не запущен"

step "cream configure --again вручную: мимо отметки"
c configure --again cfg-once
want_rc 0 "configure --again cfg-once"
eq "$(j .result.reason)" followup "причина followup"
wait_until 20 count_ge $STATE/cfg-once/configure.count 4 || fail "--again: configure не запущен"
wait_until 20 settled cfg-once || fail "cream-configure@cfg-once не завершился"
eq "$(tail -n 1 $STATE/cfg-once/configure.log)" "reason=followup phase=live" "--again: reason=followup"
c show cfg-once
eq "$(j .result.configured.matches)" true "после followup отметка совпала"
