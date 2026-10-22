#!/usr/bin/env bash
# make test-build (спека §14 «Build-фаза», план T22): фаза build cream через
# настоящий virt-customize на cloud-образе Debian 12, проверки — scenario.sh.
#
#   bash test/build/run.sh <cream_<v>_amd64.deb> <файл лога>
#
# Окружение (задаёт Makefile): TEST_BUILD_IMAGE_URL и TEST_BUILD_IMAGE_SHA512 —
# закреплённый базовый образ; кеш — .cache/test-build/<имя образа> (скачивается
# один раз, sha512 сверяется с закреплённым и с SHA512SUMS рядом с образом).
# TEST_BUILD_HOST=1 — virt-customize хоста вместо контейнера test/build/Dockerfile
# (последовательность Р30 обходится без --tar-in и идёт и на 1.48 Debian 12).
# Временный каталог (overlay, staging, распакованный пакет) удаляется при любом
# выходе. Вызывать под замком HEAVY_LOCK (make test-build берёт его сам).
set -euo pipefail

deb=${1:?путь к .deb}
log=${2:?файл лога}
url=${TEST_BUILD_IMAGE_URL:?TEST_BUILD_IMAGE_URL}
sha=${TEST_BUILD_IMAGE_SHA512:?TEST_BUILD_IMAGE_SHA512}
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
cache=$ROOT/.cache/test-build
image=cream-build:debian-13

[ -f "$deb" ] || {
	echo "run.sh: нет пакета $deb (make deb ARCH=amd64)" >&2
	exit 66
}
mkdir -p "$(dirname "$log")" "$cache"
exec > >(tee "$log") 2>&1
echo "== test-build $(date -Is), лог $log"

# --- Базовый образ: один раз в кеш, сверка sha512 каждый прогон -------------
base=$cache/$(basename "$url")
if [ ! -f "$base" ]; then
	echo "== загрузка $url"
	sums=$(curl -fsSL "$(dirname "$url")/SHA512SUMS")
	listed=$(printf '%s\n' "$sums" | awk -v f="$(basename "$url")" '$2 == f {print $1}')
	[ "$listed" = "$sha" ] || {
		echo "run.sh: sha512 в SHA512SUMS («$listed») не совпадает с закреплённым" >&2
		exit 69
	}
	curl -fSL --retry 3 -o "$base.part" "$url"
	mv "$base.part" "$base.check"
	base_tmp=$base.check
else
	base_tmp=$base
fi
echo "$sha  $base_tmp" | sha512sum -c --quiet - || {
	echo "run.sh: sha512 образа $base_tmp не совпадает" >&2
	exit 69
}
[ "$base_tmp" = "$base" ] || mv "$base_tmp" "$base"
echo "== базовый образ $(basename "$base") (sha512 сверен)"

work=$(mktemp -d "$cache/work.XXXXXX")
cleanup() { rm -rf -- "$work"; }
trap cleanup EXIT
trap 'exit 130' INT TERM
cp "$deb" "$work/cream.deb"
mkdir -p "$work/tmp"

start=$(date +%s)
if [ "${TEST_BUILD_HOST:-}" = 1 ]; then
	echo "== режим: virt-customize хоста ($(virt-customize --version))"
	mkdir -p "$cache/appliance-host"
	LIBGUESTFS_BACKEND=direct LIBGUESTFS_CACHEDIR=$cache/appliance-host TMPDIR=$work/tmp \
		bash "$ROOT/test/build/scenario.sh" "$work/cream.deb" "$base" "$work" "$ROOT"
else
	echo "== режим: контейнер $image (test/build/Dockerfile)"
	docker build -q -t "$image" "$ROOT/test/build" >/dev/null
	mkdir -p "$cache/appliance-container"
	# /dev/kvm — группой хоста; память ограничена: appliance libguestfs ~1 ГБ.
	docker run --rm --name "cream-build-$$" --device /dev/kvm --group-add "$(stat -c %g /dev/kvm)" \
		-u "$(id -u):$(id -g)" --memory 3g --pids-limit 1024 \
		-e HOME=/tmp -e LIBGUESTFS_BACKEND=direct -e LIBGUESTFS_CACHEDIR=/cache/appliance-container \
		-e TMPDIR="/cache/${work#"$cache"/}/tmp" \
		-v "$ROOT:/src:ro" -v "$cache:/cache" "$image" \
		bash /src/test/build/scenario.sh "/cache/${work#"$cache"/}/cream.deb" "/cache/$(basename "$base")" \
		"/cache/${work#"$cache"/}" /src
fi

# Значения параметров не попали в лог прогона.
read -r s1 s2 s3 <"$work/values"
if grep -qF -e "$s1" -e "$s2" -e "$s3" "$log"; then
	echo "FAIL: значение секрета в логе прогона" >&2
	exit 1
fi
echo "ok значений секретов в логе нет"
echo "== test-build прошёл за $(($(date +%s) - start)) с"
