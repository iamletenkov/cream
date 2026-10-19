#!/bin/sh
# Проверка воспроизводимости выпуска (спека §15): две независимые сборки
# make dist-release с одним SOURCE_DATE_EPOCH (время коммита) в разные
# каталоги и с разными кешами сборки Go; SHA256SUMS обеих обязаны совпасть
# байт в байт. Запускается из корня репозитория через make repro-check
# (под замком HEAVY_LOCK).
set -eu

MAKE="${MAKE:-make}"
out="${REPRO_DIR:-dist/repro}"
epoch="${SOURCE_DATE_EPOCH:-$(git log -1 --format=%ct)}"

rm -rf "$out"
for run in a b; do
	echo "== сборка $run"
	# Отдельный пустой GOCACHE на каждую сборку: совпадение не должно
	# объясняться тем, что сборка взяла объекты из кеша прошлой.
	rm -rf ".cache/go-build-repro-$run"
	$MAKE --no-print-directory dist-release-unlocked \
		RELEASE_DIR="$out/$run" GOCACHE_DIR="go-build-repro-$run" \
		SOURCE_DATE_EPOCH="$epoch"
done

for run in a b; do
	[ -s "$out/$run/SHA256SUMS" ] || { echo "repro-check: нет $out/$run/SHA256SUMS" >&2; exit 1; }
done

echo "== SHA256SUMS (сборка a)"
cat "$out/a/SHA256SUMS"
if cmp -s "$out/a/SHA256SUMS" "$out/b/SHA256SUMS"; then
	echo "repro-check: обе сборки совпали ($(wc -l <"$out/a/SHA256SUMS") файлов, SOURCE_DATE_EPOCH=$epoch)"
else
	echo "repro-check: сборки различаются:" >&2
	diff "$out/a/SHA256SUMS" "$out/b/SHA256SUMS" >&2 || true
	exit 1
fi
