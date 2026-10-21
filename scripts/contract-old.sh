#!/bin/sh
# Контракт ADR-0004 (совместимые добавления cream/v1): манифест с полем новее
# выпущенного cream (every_boot, scope: build — cream 1.2.0) объявляет
# cream: ">=1.2.0", и настоящий старый выпуск отказывает ему по cream: —
# 68 platform.cream_version, а не «неизвестное поле» или «допустимо config или
# install». Проверка гоняет релизный бинарь старой версии по каждой фикстуре
# testdata/contract/*/extensions/*.
#
#   sh scripts/contract-old.sh <версия> <sha256 тарбола linux_amd64> <каталог кеша>
#
# Запуск — из корня репозитория; каталог кеша — относительный путь внутри него
# (монтируется в контейнер как $PWD/<кеш>). Бинарь — linux/amd64.
#
# Тарбол скачивается один раз (curl хоста) и сверяется с закреплённой суммой;
# бинарь запускается в контейнере $DEBIAN_IMAGE. Цель make — contract-old.
set -eu

ver=$1
sum=$2
cache=$3
image=${DEBIAN_IMAGE:-debian:13}

tarball="cream_${ver}_linux_amd64.tar.gz"
url="https://github.com/iamletenkov/cream/releases/download/v${ver}/${tarball}"
dir="$cache/contract-old/$ver"
mkdir -p "$dir"

if [ ! -f "$dir/$tarball" ]; then
    curl -fsSL --retry 3 -o "$dir/$tarball.part" "$url"
    mv "$dir/$tarball.part" "$dir/$tarball"
fi
if ! echo "$sum  $dir/$tarball" | sha256sum -c --status; then
    echo "contract-old: sha256 $tarball не совпала с закреплённой — удалите $dir и проверьте OLD_CREAM_SHA256" >&2
    exit 1
fi
tar -xzf "$dir/$tarball" -C "$dir" cream

fail=0
n=0
for ext in testdata/contract/*/extensions/*/; do
    [ -d "$ext" ] || continue
    ext=${ext%/}
    n=$((n + 1))
    rc=0
    out=$(docker run --rm --platform linux/amd64 --network none -u "$(id -u):$(id -g)" -v "$PWD:/src:ro" -v "$PWD/$dir:/old:ro" -w /src \
        -e HOME=/tmp "$image" /old/cream validate --json "$ext") || rc=$?
    if [ "$rc" -eq 68 ] && printf '%s' "$out" | grep -q '"platform.cream_version"'; then
        echo "contract-old: $ext — cream $ver: 68 platform.cream_version"
    else
        echo "contract-old: $ext — cream $ver ответил $rc, ждали 68 platform.cream_version:" >&2
        printf '%s\n' "$out" >&2
        fail=1
    fi
done
if [ "$n" -eq 0 ]; then
    echo "contract-old: нет фикстур testdata/contract/*/extensions/*" >&2
    exit 1
fi
exit "$fail"
