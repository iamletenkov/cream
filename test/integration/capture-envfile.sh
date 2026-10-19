#!/usr/bin/env bash
# Эталоны EnvironmentFile (спека §14, план T19): каждый internal/envfile/testdata/
# cases/<case>.env отдаётся настоящему systemd как EnvironmentFile= юнита
# systemd-run, окружение юнита пишется в internal/envfile/testdata/
# systemd-<ver>/<case>.json — {"env": [...]} или {"error": true}, если systemd
# отверг файл. В эталон идут только ключи, которых нет в окружении того же юнита
# без EnvironmentFile. Снимается один раз; reference_test сверяет с ними Parse.
#
#   bash test/integration/capture-envfile.sh [BASE…]
#
# Умолчание — ubuntu:22.04 (249), debian:12 (252), ubuntu:24.04 (255),
# debian:13 (257); контейнеры по одному, под замком HEAVY_LOCK (make вызывает
# под ним сам; вне make — flock здесь).
set -euo pipefail

if [ "${1:-}" = guest ]; then
	# Внутри контейнера: /cases/*.env → /out/<case>.json; печатает версию systemd.
	ver=$(systemctl --version | awk 'NR == 1 {print $2}')
	run() { systemd-run --wait --pipe --quiet --collect "$@" /usr/bin/env -0; }
	run >/out/.base 2>/dev/null
	for c in /cases/*.env; do
		name=$(basename "$c" .env)
		if run -p "EnvironmentFile=$c" >/out/.case 2>/out/.err; then
			jq -Rs --rawfile base /out/.base '
				($base | split("\u0000") | map(split("=")[0])) as $b
				| {env: (split("\u0000") | map(select(length > 0))
					| map(select((split("=")[0]) as $k | ($b | any(. == $k)) | not))
					| sort)}' </out/.case >"/out/$name.json"
		else
			printf '{"error": true}\n' >"/out/$name.json"
		fi
	done
	rm -f /out/.base /out/.case /out/.err
	echo "$ver"
	exit 0
fi

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
if [ -z "${CREAM_HEAVY_HELD:-}" ]; then
	exec flock "${HEAVY_LOCK:-/tmp/cream-heavy.lock}" env CREAM_HEAVY_HELD=1 bash "$0" "$@"
fi
# shellcheck source=host.sh
. "$ROOT/test/integration/host.sh"
trap it_cleanup EXIT

bases=("$@")
[ ${#bases[@]} -gt 0 ] || bases=(ubuntu:22.04 debian:12 ubuntu:24.04 debian:13)
cases="$ROOT/internal/envfile/testdata/cases"
for base in "${bases[@]}"; do
	tag=${base//:/-}
	it_image "$tag" "$base"
	it_start "$tag" "cream-it-envfile-$tag"
	it_exec mkdir -p /cases /out
	docker cp "$cases/." "$IT_NAME:/cases/"
	docker cp "$0" "$IT_NAME:/capture.sh"
	ver=$(it_exec bash /capture.sh guest)
	out="$ROOT/internal/envfile/testdata/systemd-$ver"
	mkdir -p "$out"
	docker cp "$IT_NAME:/out/." "$out/"
	it_log "$base: systemd $ver → ${out#"$ROOT"/} ($(find "$out" -name '*.json' | wc -l) эталонов)"
	it_cleanup
done
