#!/usr/bin/env bash
# §13 (R3), §14 шаг 5: секреты не попадают в argv (ps во время join — окно
# 2 с), в окружение действия, в журнал systemd (вывод фильтруется), в реестр,
# history.jsonl, config и вывод cream; секрет domain-ключа живёт только в
# /etc/cream/<ext>/secrets (0600).
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"
T=fixture-teleport-like

step "join из юнита systemd: ps, окружение и журнал во время действия"
umask 077
printf '%s' "$JOIN_SECRET" >/root/join.secret
umask 022
: >/tmp/ps.txt
: >/tmp/environ.txt
systemd-run --unit it-join --collect --quiet -p StandardInput=file:/root/join.secret \
	/usr/bin/cream run $T join FTL_PROXY=proxy2.example:443 FTL_JOIN_SECRET=-
seen=0
for _ in $(seq 1 100); do
	ps -eo args >>/tmp/ps.txt
	pid=$(pgrep -f "/opt/cream/ext/$T/actions/join" | head -n 1 || true)
	if [ -n "$pid" ]; then
		seen=1
		tr '\0' '\n' <"/proc/$pid/environ" >>/tmp/environ.txt 2>/dev/null || true
	fi
	[ "$(systemctl show -p ActiveState --value it-join.service 2>/dev/null)" = active ] || [ $seen = 0 ] || break
	sleep 0.1
done
wait_until 30 sh -c "! systemctl is-active -q it-join.service" || fail "join из юнита не завершился"
rm -f /root/join.secret
eq "$seen" 1 "процесс действия join пойман в ps"
has /tmp/ps.txt "actions/join" "выборка ps содержит действие"
hasnt /tmp/ps.txt "$JOIN_SECRET" "секрета нет в argv ни одного процесса"
has /tmp/environ.txt '^CREAM_ACTION=join$' "окружение действия прочитано"
hasnt /tmp/environ.txt "$JOIN_SECRET" "секрета нет в окружении действия"
has /tmp/environ.txt '^FTL_PROXY=proxy2.example:443$' "несекретный параметр — в окружении"
has /etc/cream/$T/config '^FTL_PROXY=proxy2.example:443$' "join из юнита отработал"
# Журнал проверяется, когда journald работает; под qemu-user (arm64 на
# amd64-хосте) он падает на загрузке — это строка skip, а не молчание.
journal=1
systemctl is-active -q systemd-journald.service || journal=0
journalctl --no-pager -o cat >/tmp/journal.txt 2>/dev/null || true
if [ $journal = 1 ]; then
	has /tmp/journal.txt 'join: proxy=proxy2.example:443 secret=\*\*\*' "журнал: вывод действия с маской"
else
	printf 'skip journald не работает (%s) — проверки журнала пропущены\n' "$(systemctl is-active systemd-journald.service || true)"
fi

step "секреты нигде, кроме secrets"
for s in "$JOIN_SECRET" "$TOKEN_SECRET"; do
	hasnt /tmp/journal.txt "$s" "журнал systemd без ${s%-*}"
	if grep -rlF -- "$s" /var/lib/cream /var/cache/cream /run/cream /opt/cream 2>/dev/null; then
		fail "секрет ${s%-*} в файлах cream"
	fi
	ok "реестр, history.jsonl, состояние, кеш, деревья — без ${s%-*}"
	if grep -rlF -- "$s" /etc/cream --exclude=secrets 2>/dev/null; then
		fail "секрет ${s%-*} в настройках вне secrets"
	fi
	ok "/etc/cream (кроме secrets) — без ${s%-*}"
done
hasnt /etc/cream/$T/secrets "$JOIN_SECRET" "секрет join не сохраняется даже в secrets"
has /etc/cream/$T/secrets "$TOKEN_SECRET" "секрет ключа FTL_TOKEN — в secrets"
for cmd in "list" "show $T" "config export --shell $T" "actions $T"; do
	# shellcheck disable=SC2086
	c $cmd
	want_rc 0 "cream $cmd"
	if printf '%s%s' "$OUT" "$ERR" | grep -qF -e "$JOIN_SECRET" -e "$TOKEN_SECRET"; then
		fail "секрет в выводе cream $cmd"
	fi
	ok "вывод cream $cmd без секретов"
done
