#!/usr/bin/env bash
# Живая интеграция cream (спека §14, план T19): настоящий .deb в контейнере с
# systemd как PID 1, сценарии test/integration/scenario/NN-*.sh по порядку.
#
#   bash test/integration/run.sh <distro> <cream.deb> <каталог-лога>
#
# distro — debian-12 | ubuntu-24.04 | debian-13 | ubuntu-22.04 (образ —
# test/integration/Dockerfile с BASE=<distro через «:»>). Сценарий с меткой
# «# it: reboot-before» выполняется после docker restart контейнера — так
# проверяется configure на загрузке. ONLY=10,20 — только перечисленные
# сценарии (для отладки; состояние между ними общее, порядок важен).
# Контейнер один, имя cream-it-<distro>[-<arch>], удаляется при любом выходе.
# ARCH=arm64 на amd64-хосте — контейнер под qemu (host.sh). Вызывать
# под замком HEAVY_LOCK (make test-integration берёт его сам).
set -euo pipefail

distro=${1:?distro}
deb=${2:?путь к .deb}
logdir=${3:?каталог лога}
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
# shellcheck source=host.sh
. "$ROOT/test/integration/host.sh"

case "$distro" in
debian-12 | debian-13 | ubuntu-22.04 | ubuntu-24.04) ;;
*)
	echo "run.sh: неизвестный дистрибутив «$distro»" >&2
	exit 64
	;;
esac
[ -f "$deb" ] || {
	echo "run.sh: нет пакета $deb (make deb ARCH=…)" >&2
	exit 66
}
mkdir -p "$logdir"
base=${distro/-/:}

trap it_cleanup EXIT
trap 'exit 130' INT TERM

tag=$distro
[ "$IT_ARCH" = "$IT_HOST_ARCH" ] || tag=$distro-$IT_ARCH
it_image "$tag" "$base"
it_start "$tag" "cream-it-$tag"

# Дерево /it: сценарии, исходники каталогов (как в репозитории), пакет.
it_exec mkdir -p /it/src
docker cp "$ROOT/test/integration/scenario" "$IT_NAME:/it/scenario"
for cat in pilot catalog catalog-next; do
	docker cp "$ROOT/testdata/$cat" "$IT_NAME:/it/src/$cat"
done
docker cp "$deb" "$IT_NAME:/it/cream.deb"
# docker cp сохраняет uid владельца с хоста: дерево каталогов — root, как
# у источника на машине (§13: кеш и источники принадлежат root).
it_exec chown -R root:root /it

passed=() failed=""
for s in "$ROOT"/test/integration/scenario/[0-9][0-9]-*.sh; do
	name=$(basename "$s" .sh)
	if [ -n "${ONLY:-}" ] && ! [[ ",$ONLY," == *",${name%%-*},"* ]]; then
		continue
	fi
	if grep -q '^# it: reboot-before' "$s"; then
		it_log "$distro: docker restart (загрузка с установленными расширениями)"
		docker restart "$IT_NAME" >/dev/null
		it_wait_boot
	fi
	it_log "$distro: $name"
	start=$(date +%s)
	if it_exec bash "/it/scenario/$name.sh" >"$logdir/$name.log" 2>&1; then
		passed+=("$name")
		it_log "$distro: $name — ok ($(($(date +%s) - start)) с, $(grep -c '^ok ' "$logdir/$name.log") проверок)"
	else
		failed=$name
		it_log "$distro: $name — FAIL ($(($(date +%s) - start)) с), хвост лога $logdir/$name.log:"
		tail -n 25 "$logdir/$name.log" >&2
		break
	fi
done

# Состояние машины на конец прогона — для разбора падений.
{
	it_exec systemctl --failed --no-legend || true
	it_exec journalctl -b --no-pager -o short-monotonic || true
} >"$logdir/journal.log" 2>&1

if [ -n "$failed" ]; then
	it_log "$distro: упал $failed (прошли: ${passed[*]:-нет})"
	exit 1
fi
it_log "$distro: все сценарии прошли (${#passed[@]}): ${passed[*]}"
