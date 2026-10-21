#!/usr/bin/env bash
# cream 1.2.0 (спека 2026-10-21 §10.1, §10.7–§10.9, §8.2; план Task 15): на
# живой машине фаза определяется сама (live, pid1=systemd); install пишет
# отметку выполнения configure; every_boot, падающий configure, код 20 и
# повтор из хука (cream configure --again); ключ scope: build — только в
# окружении хука install; /etc/image-labels.d/cream.json по реестру.
# Перезагрузки — 65-v12-boot1 и 66-v12-boot2.
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"
# count_ge <файл счётчика> <N> — для wait_until: счётчик хука ≥ N.
count_ge() { [ "$(counter "$1")" -ge "$2" ]; }
MARKS=/var/lib/cream/configured
LABELS=/etc/image-labels.d/cream.json

c source add v12 --path "$IT/src/catalog-1.2" --sync
want_rc 0 "source add v12 (фикстуры cream 1.2)"

step "фаза определяется сама: live по PID 1"
c install v12/cfg-once
want_rc 0 "install cfg-once без --phase"
eq "$(j .phase)" live "конверт: phase"
eq "$(j .reason)" pid1=systemd "конверт: reason"
eq "$(reg cfg-once .phase)" live "реестр: phase live"

step "отметка выполнения после install"
eq "$(counter $STATE/cfg-once/configure.count)" 1 "cfg-once: configure один раз"
eq "$(tail -n 1 $STATE/cfg-once/configure.log)" "reason=install phase=live" "cfg-once: причина install"
exists $MARKS/cfg-once.json "отметка cfg-once записана"
eq "$(jq -r .version $MARKS/cfg-once.json)" "$(reg cfg-once .version)" "отметка: version = реестр"
eq "$(jq -r .ext_digest $MARKS/cfg-once.json)" "$(reg cfg-once .ext_digest)" "отметка: ext_digest = реестр"
eq "$(jq -r .machine_id $MARKS/cfg-once.json)" "sha256:$(tr -d '[:space:]' </etc/machine-id | sha256sum | cut -d' ' -f1)" \
	"отметка: machine_id — sha256 /etc/machine-id"
eq "$(stat -c %a $MARKS/cfg-once.json)" 644 "отметка 0644"
hasnt $MARKS/cfg-once.json hello "в отметке нет значений настроек"
c show cfg-once
want_rc 0 "show cfg-once"
eq "$(j '.result.configured | "\(.matches) \(.every_boot)"')" "true false" "show: configured.matches, every_boot"

step "every_boot, падающий configure, код 20"
c install v12/cfg-every-boot
want_rc 0 "install cfg-every-boot"
exists $MARKS/cfg-every-boot.json "every_boot: отметка пишется и так"
c show cfg-every-boot
eq "$(j .result.configured.every_boot)" true "show: every_boot true"
c install v12/cfg-failing
want_rc 79 "install cfg-failing: configure упал — 79"
want_code hook.failed "install cfg-failing"
eq "$(reg cfg-failing .status)" installed "cfg-failing в реестре installed"
absent $MARKS/cfg-failing.json "сбой — отметки нет"
eq "$(hist '[.[] | select(.op == "configure" and .ext == "cfg-failing")] | .[-1].status')" failed \
	"history.jsonl: configure cfg-failing failed"
# Код 20 configure (§10.8): «пропущено» — не сбой, отметка снимается,
# запись skipped в history.jsonl.
c install v12/cfg-skip
want_rc 0 "install cfg-skip: configure 20 — не сбой"
eq "$(reg cfg-skip .status)" installed "cfg-skip в реестре installed"
eq "$(systemctl show -p Result --value cream-configure@cfg-skip.service)" success "cream-configure@cfg-skip: success"
eq "$(hist '[.[] | select(.op == "configure" and .ext == "cfg-skip")] | map(.status) | join(",")')" skipped \
	"history.jsonl: configure cfg-skip skipped"
eq "$(counter $STATE/cfg-skip/configure.count)" 1 "cfg-skip: configure вызван"
absent $MARKS/cfg-skip.json "код 20 — отметки нет"
# cream configure --wait при работающем systemd: исход юнита из
# /run/cream/configure/<ext>.result — status skipped, код 0.
c configure --wait cfg-skip
want_rc 0 "configure --wait cfg-skip: код 20 — не сбой"
eq "$(j '.result | "\(.mode) \(.status)"')" "restart skipped" "configure --wait: mode restart, status skipped"
eq "$(jq -r .status /run/cream/configure/cfg-skip.result)" skipped "исход юнита записан"
eq "$(counter $STATE/cfg-skip/configure.count)" 2 "cfg-skip: configure вызван ещё раз"
for e in cfg-failing cfg-skip; do systemctl reset-failed "cream-configure@$e.service" 2>/dev/null || true; done

step "повтор из хука: cream configure --again → второй прогон"
c install v12/cfg-again
want_rc 0 "install cfg-again"
wait_until 20 count_ge $STATE/cfg-again/configure.count 2 ||
	fail "cfg-again: повтор не пришёл ($(counter $STATE/cfg-again/configure.count) прогонов)"
sleep 2
eq "$(counter $STATE/cfg-again/configure.count)" 2 "cfg-again: ровно два прогона"
eq "$(paste -sd' ' $STATE/cfg-again/configure.log)" "reason=install phase=live reason=followup phase=live" \
	"cfg-again: install, затем followup"
absent /run/cream/cfg-again.reason "маркер причины снят"

step "set: configure снова; --no-apply — отметка расходится"
c set --wait cfg-once MESSAGE=again
want_rc 0 "set --wait MESSAGE=again"
eq "$(counter $STATE/cfg-once/configure.count)" 2 "set с применением — configure ещё раз"
eq "$(tail -n 1 $STATE/cfg-once/configure.log)" "reason=apply phase=live" "причина apply"
c show cfg-once
eq "$(j .result.configured.matches)" true "после apply отметка совпала"
c set --no-apply cfg-once MESSAGE=bye
want_rc 0 "set --no-apply MESSAGE=bye"
eq "$(counter $STATE/cfg-once/configure.count)" 2 "--no-apply — configure не запущен"
c show cfg-once
eq "$(j .result.configured.matches)" false "params_digest разошёлся — отметка не совпадает"

step "scope: build — значение только в окружении хука install"
TOKEN_VALUE=it-build-token-$(od -An -N8 -tx1 /dev/urandom | tr -d ' \n')
(umask 077 && printf 'TOKEN=%s\nLEVEL=high\n' "$TOKEN_VALUE" >/root/build-token.env)
c install v12/build-token
want_rc 64 "install без обязательного TOKEN"
want_code config.build_required "install build-token"
c install --params-file /root/build-token.env v12/build-token
want_rc 0 "install build-token --params-file"
rm -f /root/build-token.env
eq "$(cat $STATE/build-token/token.sha256)" "$(printf '%s' "$TOKEN_VALUE" | sha256sum | cut -d' ' -f1)" \
	"хук install получил TOKEN"
eq "$(tail -n 1 $STATE/build-token/install.log)" "phase=live level=high" "LEVEL из --params-file"
has /etc/cream/build-token/config '^LEVEL=high$' "LEVEL хранится в config"
hasnt /etc/cream/build-token/config '^TOKEN=' "TOKEN нет в config"
hasnt /etc/cream/build-token/secrets '^TOKEN=' "TOKEN нет в secrets"
if grep -rqF -- "$TOKEN_VALUE" /etc/cream /var/lib/cream /etc/image-labels.d /var/cache/cream /run/cream 2>/dev/null; then
	fail "значение TOKEN на диске: $(grep -rlF -- "$TOKEN_VALUE" /etc/cream /var/lib/cream /etc/image-labels.d /var/cache/cream /run/cream)"
fi
ok "значения TOKEN нет в /etc/cream, /var/lib/cream, /var/cache/cream, cream.json"
c get build-token TOKEN
want_rc 64 "get TOKEN"
want_code config.scope_build "get TOKEN"
c set build-token TOKEN=- < <(printf 'x\n')
want_rc 64 "set TOKEN"
want_code config.scope_build "set TOKEN"
c show build-token
hasnt <(printf '%s\n' "$OUT") '"TOKEN"' "show не печатает TOKEN"
journalctl -b --no-pager >/tmp/v12-journal.txt 2>&1 || true
hasnt /tmp/v12-journal.txt "$TOKEN_VALUE" "значения TOKEN нет в журнале загрузки"

step "/etc/image-labels.d/cream.json"
exists $LABELS "cream.json есть"
c version
eq "$(jq -r '."cream.version"' $LABELS)" "$(j .cream | sed 's/^v//')" "cream.version — версия cream"
for e in cfg-once cfg-every-boot cfg-failing cfg-skip cfg-again build-token; do
	eq "$(jq -r '."cream.extensions" | fromjson | map(select(.name == "'$e'")) | .[0].digest' $LABELS)" \
		"$(reg $e .ext_digest)" "cream.extensions: $e с ext_digest реестра"
done
eq "$(jq -r '."cream.sources" | fromjson | map(select(.name == "v12")) | .[0].type' $LABELS)" path "cream.sources: v12 path"
hasnt $LABELS 'params_digest|LEVEL|hello|bye' "в cream.json нет параметров"
