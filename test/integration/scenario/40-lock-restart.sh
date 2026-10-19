#!/usr/bin/env bash
# §14 шаг 7, §7.3: взаимная блокировка через systemd. set-managed держит
# lock/<ext> (exclusive) и синхронно перезапускает службу, у которой
# ExecStartPre зовёт cream run <ext> render. С render: lock: none restart
# укладывается в время самого restart; с lock: exclusive (lock-restart-
# exclusive) render ждёт блокировку до --lock-timeout и падает 75.
# Пороги (риск 3 плана): --lock-timeout 10 с, позитивный вариант — < 5 с.
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"
T=fixture-teleport-like
LIMIT_MS=5000

# timed <имя> <ext> <аргументы set-managed…> — $RC, $OUT, $MS.
timed() {
	local t0
	t0=$(now_ms)
	c run --lock-timeout 10s "$@"
	MS=$(($(now_ms) - t0))
}

step "пилот: set-managed → restart → render (lock: none)"
renders=$(count $STATE/$T/render.log)
timed $T set-managed env=prod space=sp-1
want_rc 0 "set-managed (служба не запущена — restart её стартует)"
[ "$MS" -lt "$LIMIT_MS" ] || fail "set-managed занял $MS мс — дольше $LIMIT_MS (ожидание блокировки?)"
ok "set-managed за $MS мс"
eq "$(j '.result.output.labels')" "env=prod space=sp-1" "ответ set-managed"
has /etc/cream/$T/config '^FTL_MANAGED_LABELS="?env=prod space=sp-1"?$' "readonly-ключ записан вложенным cream set"
eq "$(systemctl is-active fixture-teleport.service)" active "служба запущена"
eq "$(count $STATE/$T/render.log)" $((renders + 1)) "render вызван из ExecStartPre"
has $STATE/$T/render.log '^phase=live labels=env=prod space=sp-1$' "render прочёл новые метки"

timed $T set-managed env=stage
want_rc 0 "set-managed при работающей службе (настоящий restart)"
[ "$MS" -lt "$LIMIT_MS" ] || fail "set-managed занял $MS мс — дольше $LIMIT_MS"
ok "set-managed за $MS мс"
c run $T managed-status
want_rc 0 "managed-status во время работы службы"
eq "$(j '.result.output.rendered')" env=stage "managed-status: отрисованные метки"
eq "$(j '.result.output.service')" active "managed-status: служба"

step "фикстура lock-restart (testdata/catalog) — тот же контракт"
c install fixtures/lock-restart
want_rc 0 "install lock-restart"
timed lock-restart set-managed a=1
want_rc 0 "lock-restart set-managed"
[ "$MS" -lt "$LIMIT_MS" ] || fail "lock-restart set-managed занял $MS мс"
ok "lock-restart set-managed за $MS мс"
eq "$(count $STATE/lock-restart/render.log)" 1 "render lock-restart вызван"

step "негатив: lock-restart-exclusive — render ждёт блокировку и падает 75"
c install fixtures/lock-restart-exclusive
want_rc 0 "install lock-restart-exclusive"
# Тот же ExecStartPre, но с --lock-timeout 10 с вместо 120 с по умолчанию.
dropin=/etc/systemd/system/lock-restart-exclusive.service.d
mkdir -p $dropin
printf '[Service]\nExecStartPre=\nExecStartPre=/usr/bin/cream run --lock-timeout 10s lock-restart-exclusive render\n' \
	>$dropin/10-timeout.conf
systemctl daemon-reload
timed lock-restart-exclusive set-managed a=1
[ "$RC" != 0 ] || fail "set-managed с render: lock: exclusive прошёл за $MS мс — тупик не воспроизвёлся"
ok "set-managed упал (код $RC)"
[ "$MS" -ge 10000 ] || fail "упал за $MS мс — раньше --lock-timeout 10 с"
ok "упал по таймауту блокировки: $MS мс"
systemctl show -p ExecStartPre --value lock-restart-exclusive.service >/tmp/lre.txt
has /tmp/lre.txt 'render .*code=exited ; status=75 ' "ExecStartPre render завершился 75 (lock.timeout)"
eq "$(count $STATE/lock-restart-exclusive/render.log)" 0 "render не выполнялся"

step "уборка"
rm -rf $dropin
systemctl daemon-reload
c remove lock-restart-exclusive
want_rc 0 "remove lock-restart-exclusive"
c remove lock-restart
want_rc 0 "remove lock-restart"
absent /etc/systemd/system/lock-restart.service "юнит lock-restart убран"
