#!/usr/bin/env bash
# §7, §14 шаг 3: cream actions и cream run — join с секретом со stdin
# (вложенный cream set наследует блокировку), фильтр секретов в выводе,
# output: json с конвертом и без, коды действия как есть, отказы до запуска.
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"
T=fixture-teleport-like

step "cream actions"
c actions $T
want_rc 0 "actions"
eq "$(j '[.result.actions[].name] | join(" ")')" "join managed-status render set-managed status" "действия"
eq "$(j '.result.actions[] | select(.name == "render") | .lock')" none "render — lock: none"
eq "$(j '.result.actions[] | select(.name == "join") | .stdin')" secret "join — stdin: secret"

step "join: секрет только со stdin"
c run $T join FTL_PROXY=proxy.example:443 "FTL_JOIN_SECRET=$JOIN_SECRET"
want_rc 64 "секрет в argv"
want_code secret.in_argv
c run $T join FTL_JOIN_SECRET=- </dev/null
want_rc 64 "нет обязательного FTL_PROXY"
c run $T join FTL_PROXY=proxy.example:443 FTL_JOIN_SECRET=- </dev/null
[ "$RC" != 0 ] || fail "пустой stdin секрета принят"
ok "пустой stdin секрета — отказ (код $RC)"
c run $T join FTL_PROXY=proxy.example:443 FTL_JOIN_SECRET=- < <(printf '%s' "$JOIN_SECRET")
want_rc 0 "join с секретом со stdin"
eq "$(j '.result.action')" "$T/join" "конверт run: action"
eq "$(j '.result.output.joined')" true "ответ действия в result.output"
eq "$(j '.result.output.secret_len')" ${#JOIN_SECRET} "действие получило секрет целиком"
printf '%s\n' "$ERR" >/tmp/join.err
has /tmp/join.err 'secret=\*\*\*' "stderr действия: секрет заменён на ***"
hasnt /tmp/join.err "$JOIN_SECRET" "секрета нет в stderr cream"
has /etc/cream/$T/config '^FTL_PROXY=proxy.example:443$' "вложенный cream set --allow-readonly записал адрес"

step "output: json без конверта и text"
set +e
raw=$(cream run $T managed-status)
rc=$?
set -e
eq "$rc" 0 "managed-status без --json"
eq "$(printf '%s' "$raw" | jq -c 'keys')" '["labels","rendered","service"]' "stdout — ровно объект действия"
set +e
txt=$(cream run $T status)
set -e
eq "$txt" "service=inactive proxy=proxy.example:443" "status (text) насквозь"

step "коды действия и отказы до запуска"
c run $T set-managed BAD-LABEL
want_rc 2 "код действия как есть (set-managed: неверный ввод)"
eq "$(j '.result.exit_code')" 2 "result.exit_code"
eq "$(j '.ok')" false "ok=false при ненулевом коде"
c run $T no-such-action
want_rc 66 "нет действия"
c run no-such-ext status
want_rc 66 "нет расширения"

step "сигнал: SIGTERM cream во время действия"
# join спит 2 с: cream получает SIGTERM, пока действие работает, — группа
# процессов действия снимается, блокировка домена освобождается, адрес не пишется.
cream run $T join FTL_PROXY=interrupted.example:443 FTL_JOIN_SECRET=- \
	< <(printf '%s' "$JOIN_SECRET") >/tmp/sig.out 2>/tmp/sig.err &
cpid=$!
wait_until 10 pgrep -f "/opt/cream/ext/$T/actions/join" || fail "действие join не запустилось"
apid=$(pgrep -f "/opt/cream/ext/$T/actions/join" | head -n 1)
kill -TERM $cpid
set +e
wait $cpid
rc=$?
set -e
[ "$rc" != 0 ] || fail "cream после SIGTERM вернул 0"
ok "cream после SIGTERM — код $rc"
wait_until 5 sh -c "! kill -0 $apid" || fail "процесс действия $apid пережил cream"
ok "процесс действия снят вместе с cream"
if pgrep -f "/opt/cream/ext/$T/actions/join" >/dev/null; then fail "осталось действие join"; fi
ok "дочерних процессов действия не осталось"
hasnt /etc/cream/$T/config 'interrupted' "прерванный join не записал адрес"
hasnt /tmp/sig.err "$JOIN_SECRET" "stderr прерванного вызова без секрета"
c run --lock-timeout 2s $T join FTL_PROXY=proxy.example:443 FTL_JOIN_SECRET=- < <(printf '%s' "$JOIN_SECRET")
want_rc 0 "блокировка свободна сразу после прерывания"
