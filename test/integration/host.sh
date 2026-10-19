# shellcheck shell=bash
# Общие функции хоста для run.sh и capture-envfile.sh: образ с systemd как PID 1
# и контейнер под ним. Контейнер всегда один (хост общий и ограничен по памяти),
# имя — с префиксом cream-it-, удаляется ловушкой вызывающего (it_cleanup).
#
# Без --privileged: systemd в контейнере работает с cgroupns хоста и cgroup v2,
# смонтированной на запись (--cgroupns=private на Docker без privileged отдаёт
# /sys/fs/cgroup только на чтение, и PID 1 не создаёт init.scope).

IT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
IT_NAME=""
# ARCH (amd64|arm64) — архитектура контейнера; чужая для хоста идёт через
# --platform и binfmt/qemu хоста (медленно; основной путь arm64 — раннер CI).
case "$(uname -m)" in
x86_64) IT_HOST_ARCH=amd64 ;;
aarch64) IT_HOST_ARCH=arm64 ;;
*) IT_HOST_ARCH=$(uname -m) ;;
esac
IT_ARCH=${ARCH:-$IT_HOST_ARCH}
IT_PLATFORM=()
if [ "$IT_ARCH" != "$IT_HOST_ARCH" ]; then IT_PLATFORM=(--platform "linux/$IT_ARCH"); fi

it_log() { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*" >&2; }

# it_image <tag> <base> — собрать образ cream-it:<tag> из test/integration/Dockerfile.
it_image() {
	it_log "образ cream-it:$1 (BASE=$2)"
	docker build -q "${IT_PLATFORM[@]}" -t "cream-it:$1" --build-arg "BASE=$2" "$IT_DIR" >/dev/null
}

# it_start <tag> <name> — поднять контейнер и дождаться загрузки systemd.
it_start() {
	IT_NAME=$2
	docker rm -f "$IT_NAME" >/dev/null 2>&1 || true
	docker run -d --name "$IT_NAME" "${IT_PLATFORM[@]}" \
		--cgroupns=host -v /sys/fs/cgroup:/sys/fs/cgroup:rw \
		--tmpfs /run --tmpfs /run/lock --tmpfs /tmp \
		--memory 1g --pids-limit 2048 \
		"cream-it:$1" >/dev/null
	it_wait_boot
}

# it_wait_boot — systemd дошёл до running/degraded (не дольше 90 с).
it_wait_boot() {
	local s="" _
	for _ in $(seq 1 90); do
		s=$(docker exec "$IT_NAME" systemctl is-system-running 2>/dev/null || true)
		case "$s" in
		running | degraded) return 0 ;;
		esac
		sleep 1
	done
	it_log "systemd в $IT_NAME не загрузился: состояние «$s»"
	docker logs "$IT_NAME" 2>&1 | tail -n 30 >&2
	return 1
}

# it_exec <cmd…> — команда в контейнере.
it_exec() { docker exec -i "$IT_NAME" "$@"; }

it_cleanup() {
	if [ -n "$IT_NAME" ]; then
		docker rm -f "$IT_NAME" >/dev/null 2>&1 || true
		IT_NAME=""
	fi
}
