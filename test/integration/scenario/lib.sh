# shellcheck shell=bash
# Общие функции сценариев: исполняются внутри контейнера с systemd (PID 1),
# установленным cream и деревом /it (сценарии, исходники каталогов, .deb).
# Каждая проверка либо печатает «ok …», либо завершает сценарий кодом 1 с
# «FAIL …» — run.sh останавливает прогон на первом упавшем сценарии.
set -euo pipefail

IT=/it
# Секреты прогона — случайные, одни на все сценарии ($IT/secrets.env, 0600):
# их не должно быть ни в ps, ни в журнале, ни в реестре.
if [ ! -f "$IT/secrets.env" ]; then
	rand() { od -An -N12 -tx1 /dev/urandom | tr -d ' \n'; }
	(umask 077 && printf 'JOIN_SECRET=it-join-%s\nTOKEN_SECRET=it-token-%s\n' "$(rand)" "$(rand)" >"$IT/secrets.env")
fi
# shellcheck source=/dev/null
. "$IT/secrets.env"
# Пользователь машины, которого ждут от cloud-user (выбирает 10-install).
CLOUD_USER=$(cat "$IT/cloud-user" 2>/dev/null || true)
export JOIN_SECRET TOKEN_SECRET CLOUD_USER

ok() { printf 'ok   %s\n' "$*"; }
fail() {
	printf 'FAIL %s\n' "$*" >&2
	exit 1
}
step() { printf '\n== %s\n' "$*"; }

# c [флаги cream…] <команда> … — cream --json; stdout (конверт) в $OUT, код в
# $RC, stderr в $ERR. Не падает сам. stdin — перенаправлением
# (`c … < <(printf …)`), не конвейером: в конвейере c — в подоболочке, и
# $OUT/$RC до сценария не доходят.
OUT="" RC=0 ERR=""
c() {
	local errf
	errf=$(mktemp)
	set +e
	OUT=$(cream --json "$@" 2>"$errf")
	RC=$?
	set -e
	ERR=$(cat "$errf")
	rm -f "$errf"
	if [ -n "$ERR" ]; then printf '%s\n' "$ERR" | sed 's/^/  stderr: /'; fi
	return 0
}

# j <фильтр jq> — значение из последнего конверта ($OUT).
j() { printf '%s\n' "$OUT" | jq -r "$@"; }

# want_rc N [что] — код последнего вызова c.
want_rc() {
	if [ "$RC" != "$1" ]; then
		printf '%s\n' "$OUT" | sed 's/^/  stdout: /' >&2
		fail "${2:-cream} — код $RC, ожидался $1"
	fi
	ok "${2:-cream} — код $1"
}

# want_code <error.code> [что] — код ошибки конверта.
want_code() {
	local got
	got=$(j '.error.code // ""')
	[ "$got" = "$1" ] || fail "${2:-cream} — error.code «$got», ожидался «$1»"
	ok "${2:-cream} — error.code $1"
}

# eq <фактическое> <ожидаемое> <что>
eq() {
	[ "$1" = "$2" ] || fail "$3: «$1», ожидалось «$2»"
	ok "$3 = $2"
}

# has <файл> <ERE> <что>, hasnt <файл> <ERE> <что>
has() {
	grep -Eq -- "$2" "$1" 2>/dev/null || fail "$3: в $1 нет /$2/"
	ok "$3"
}
hasnt() {
	if grep -Eq -- "$2" "$1" 2>/dev/null; then fail "$3: в $1 есть /$2/"; fi
	ok "$3"
}

exists() {
	[ -e "$1" ] || fail "$2: нет $1"
	ok "$2"
}
absent() {
	if [ -e "$1" ] || [ -L "$1" ]; then fail "$2: $1 существует"; fi
	ok "$2"
}

# count <файл> — число строк (нет файла — 0); для счётчиков хуков — значение.
count() { if [ -f "$1" ]; then wc -l <"$1" | tr -d ' '; else echo 0; fi; }
counter() { cat "$1" 2>/dev/null || echo 0; }

# reg <ext> <фильтр jq> — поле записи реестра.
reg() { jq -r "$2" "/var/lib/cream/installed/$1.json"; }

# hist <фильтр jq> — по массиву записей history.jsonl.
hist() { jq -rs "$1" /var/lib/cream/history.jsonl; }

# wait_until <секунды> <команда…> — ждать, пока команда не станет успешной.
wait_until() {
	local t=$1 i
	shift
	for ((i = 0; i < t * 2; i++)); do
		if "$@" >/dev/null 2>&1; then return 0; fi
		sleep 0.5
	done
	return 1
}

# now_ms — время в миллисекундах (замеры lock-restart).
now_ms() { date +%s%3N; }

STATE=/var/lib/cream/ext
export IT STATE
