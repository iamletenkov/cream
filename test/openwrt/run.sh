#!/usr/bin/env bash
# cream на OpenWrt (ADR-0005, план bisquite-bundled-cream Task 27): корневая ФС
# OpenWrt 25.12 из релиза iamletenkov/openwrt-builder (musl, busybox ash, apk)
# импортируется в docker-образ, в контейнере — сценарий scenario.sh под
# busybox sh: установка cream из tar.gz, system init (init-скрипты procd),
# install/configure/remove фикстур testdata/openwrt в фазах build и live,
# «загрузка» — init-скрипты rc.d с аргументом boot. procd в контейнере не
# запущен: его запуск и перезагрузку проверяет приёмка на ВМ (runbook 03).
#
#   bash test/openwrt/run.sh <cream-linux-amd64> <каталог кеша> <лог>
#
# Корневая ФС скачивается один раз (curl хоста) в <кеш>/test-openwrt и
# сверяется с закреплённой суммой OPENWRT_ROOTFS_SHA256.
set -euo pipefail

bin=${1:?путь к cream-linux-amd64}
cache=${2:?каталог кеша}
log=${3:?файл лога}
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
: "${OPENWRT_ROOTFS_URL:?нужен OPENWRT_ROOTFS_URL}"
: "${OPENWRT_ROOTFS_SHA256:?нужен OPENWRT_ROOTFS_SHA256}"

dir="$cache/test-openwrt"
mkdir -p "$dir" "$(dirname "$log")"
rootfs="$dir/rootfs-${OPENWRT_ROOTFS_SHA256:0:16}.tar.gz"
if [ ! -f "$rootfs" ]; then
	curl -fsSL --retry 3 -o "$rootfs.part" "$OPENWRT_ROOTFS_URL"
	mv "$rootfs.part" "$rootfs"
fi
if ! echo "$OPENWRT_ROOTFS_SHA256  $rootfs" | sha256sum -c --status; then
	echo "test-openwrt: sha256 корневой ФС не совпала — удалите $rootfs и проверьте OPENWRT_ROOTFS_SHA256" >&2
	exit 1
fi
image="cream-test-openwrt:${OPENWRT_ROOTFS_SHA256:0:16}"
if ! docker image inspect "$image" >/dev/null 2>&1; then
	docker import "$rootfs" "$image" >/dev/null
fi

# Поставка — как в релизе: tar.gz с бинарём cream и лицензиями.
work=$(mktemp -d "$dir/run.XXXXXX")
work=$(cd "$work" && pwd) # docker -v принимает только абсолютный путь
trap 'rm -rf "$work"' EXIT
mkdir "$work/tar"
cp "$bin" "$work/tar/cream"
cp "$ROOT/LICENSE" "$ROOT/NOTICE" "$work/tar/"
chmod 0755 "$work/tar/cream"
tar -C "$work/tar" -czf "$work/cream_linux_amd64.tar.gz" cream LICENSE NOTICE
cp -r "$ROOT/testdata/openwrt" "$work/catalog"
cp "$ROOT/test/openwrt/scenario.sh" "$work/scenario.sh"

# Пакет-зонд для проверки IPKG_INSTROOT в скриптах apk (ADR-0005): apk mkpkg
# есть в apk-tools 3 Alpine, в сборке OpenWrt его нет.
mkdir -p "$work/probe/files/usr/share/envprobe"
echo x >"$work/probe/files/usr/share/envprobe/x"
printf '#!/bin/sh\necho "IPKG_INSTROOT=${IPKG_INSTROOT-unset}" >/opt/envprobe.out\n' >"$work/probe/post"
chmod 0755 "$work/probe/post"
docker run --rm --network none -u "$(id -u):$(id -g)" -v "$work:/w" "${ALPINE_IMAGE:-alpine:3.23@sha256:132c5416901054a60a6c1f9f5073fd16dc91c8f3de1693f6be93b5912cf3ee71}" \
	apk mkpkg -I name:envprobe -I version:1.0-r0 -I arch:noarch -s post-install:/w/probe/post \
	-F /w/probe/files -o /w/envprobe.apk

echo "== test-openwrt: $image → $log"
rc=0
docker run --rm --platform linux/amd64 --network none --tmpfs /tmp:rw,nosuid,nodev,mode=1777 \
	-v "$work:/t:ro" "$image" /bin/sh /t/scenario.sh >"$log" 2>&1 || rc=$?
tail -n 40 "$log"
if [ "$rc" -ne 0 ]; then
	echo "test-openwrt: сценарий упал (код $rc), полный лог — $log" >&2
	exit "$rc"
fi
echo "test-openwrt: ok"
