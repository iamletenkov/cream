#!/usr/bin/env bash
# Фаза build cream на настоящем virt-customize (спека §12.2, §14 «Build-фаза»;
# план T22). Делает то, что будет делать bisquite (docs/guides/build-phase.md),
# и проверяет образ без загрузки.
#
#   bash test/build/scenario.sh <cream.deb> <базовый.qcow2> <каталог работы> <корень репозитория>
#
# Запускает run.sh — в контейнере test/build/Dockerfile или на хосте. Каталог
# работы — 0700, его удаляет run.sh. Значения параметров не попадают ни в argv
# (ps на время virt-customize пишется в журнал и проверяется), ни в вывод.
# ok() всегда успешна: «A && ok || fail» здесь — то же, что if-then-else.
# shellcheck disable=SC2015
set -euo pipefail

deb=${1:?cream.deb}
base=${2:?базовый образ}
work=${3:?каталог работы}
src=${4:?корень репозитория}
umask 077

pass=0
ok() {
	pass=$((pass + 1))
	echo "ok $pass $*"
}
fail() {
	echo "FAIL: $*" >&2
	exit 1
}
eq() { [ "$1" = "$2" ] && ok "$3" || fail "$3: «$1» ≠ «$2»"; }
log() { echo "== $*"; }

# --- Пакет: хостовый cream и гостевой бинарь из того же .deb ----------------
pkg=$work/pkg
dpkg-deb -x "$deb" "$pkg"
cream=$pkg/usr/bin/cream
guest=$pkg/usr/lib/cream/guest/linux-amd64/cream
[ -x "$cream" ] && [ -x "$guest" ] || fail "в пакете нет usr/bin/cream или гостевого linux-amd64/cream"
log "cream $("$cream" version)"

# Свои источники сборки (Р12, Р24): sources.yaml и кеш — в каталоге работы.
hc() { "$cream" --json --sources "$work/sources.yaml" --cache "$work/cache" "$@"; }
hc source add pilot --path "$src/testdata/pilot" --sync >/dev/null
hc source add fixtures --path "$src/testdata/catalog" --sync >/dev/null
ok "source add pilot и fixtures с --sync"

# --- Образ: overlay поверх кеша, базовый не меняется ------------------------
img=$work/image.qcow2
qemu-img create -q -f qcow2 -F qcow2 -b "$base" "$img"
ok "overlay $(basename "$img") поверх $(basename "$base")"

# Значения параметров: секрет и несекретные — уникальные на прогон, чтобы
# поиск по образу, журналу и ps не совпал случайно.
rnd() { od -An -tx1 -N6 /dev/urandom | tr -d ' \n'; }
secret=cream-secret-$(rnd)
secret2=cream-secret-$(rnd)
envtag=build-$(rnd)

# --- Разрешение на хосте (шаг 1) ---------------------------------------------
# Секрет resolve называется только именем (Р28: голое KEY или KEY=-, литерал —
# 64 secret.in_argv): params_digest от значения секрета не зависит
# (`KEY=<secret>`), настоящее значение идёт только в params.env.
resolve() {
	hc resolve --phase build --target-arch amd64 --host-arch amd64 --family deb "$@"
}
resolve pilot/cloud-user pilot/fixture-docker-like DOCKER_LOG_LEVEL=debug \
	pilot/fixture-teleport-like "FTL_ENV=$envtag" FTL_TOKEN >"$work/resolve.json"
eq "$(jq -r '[.result.layers[].name] | join(" ")' "$work/resolve.json")" \
	"cloud-user fixture-docker-like fixture-teleport-like" "resolve --phase build: три слоя в порядке установки"
eq "$(jq -r '.result.layers[2].secret_params | join(",")' "$work/resolve.json")" FTL_TOKEN \
	"resolve: секрет назван в secret_params"
rc=0
hc resolve --phase build --family deb pilot/fixture-teleport-like FTL_TOKEN=literal >"$work/resolve-lit.json" || rc=$?
eq "$rc $(jq -r .error.code "$work/resolve-lit.json")" "64 secret.in_argv" "resolve: литерал секрета — 64 secret.in_argv"
grep -qF literal "$work/resolve-lit.json" && fail "resolve напечатал значение секрета"
ok "resolve не печатает значение секрета"
resolve fixtures/failing-install FAIL=1 >"$work/resolve-fail.json"

# --- Слой: staging (шаг 2) и один virt-customize (шаг 3) ---------------------
pslog=$work/ps.log
: >"$pslog"

# stage <id> <файл resolve> <индекс слоя> [KEY=VALUE…] — каталог 0700 <id>/ с
# <name>/, record.json и params.env (0600), затем tar с владельцем 0:0.
stage() {
	local id=$1 res=$2 i=$3 name dir st
	shift 3
	name=$(jq -r ".result.layers[$i].name" "$res")
	dir=$(jq -r ".result.layers[$i].dir" "$res")
	st=$work/stage/$id
	mkdir -p "$st"
	chmod 0700 "$work/stage" "$st"
	cp -a "$dir" "$st/$name"
	jq ".result.layers[$i]" "$res" >"$st/record.json"
	if [ $# -gt 0 ]; then printf '%s\n' "$@" >"$st/params.env"; fi
	tar -C "$st" --owner=0 --group=0 --numeric-owner -cf "$work/$id.tar" .
	rm -rf -- "$st"
	echo "$name"
}

# customize <id> <name> [--run-command …] — virt-customize слоя в
# последовательности docs/guides/build-phase.md (Р30, совместима с
# virt-customize 1.48 Debian 12 — без --tar-in): --mkdir incoming/<id>,
# --copy-in архива в incoming/, распаковка tar --numeric-owner в госте и
# удаление архива. На время вызова фоном снимается `ps -eo args` в $pslog.
# Код virt-customize — в $vc_rc.
customize() {
	local id=$1 name=$2 wp in=/var/lib/cream/incoming
	shift 2
	(while :; do
		ps -eo args >>"$pslog" 2>/dev/null || true
		sleep 0.2
	done) &
	wp=$!
	vc_rc=0
	virt-customize -a "$img" --no-network \
		--copy-in "$guest:/usr/bin" \
		--chmod 0755:/usr/bin/cream \
		--run-command '/usr/bin/cream system init' \
		--mkdir "$in/$id" \
		--copy-in "$work/$id.tar:$in/" \
		--run-command "tar -C $in/$id --numeric-owner -xf $in/$id.tar && rm -f $in/$id.tar" \
		"$@" || vc_rc=$?
	kill "$wp" 2>/dev/null || true
	wait "$wp" 2>/dev/null || true
	rm -f -- "$work/$id.tar"
}

install_cmd() {
	local id=$1 name=$2 params=$3
	local in=/var/lib/cream/incoming/$id
	printf '/usr/bin/cream --json install --phase build --from-dir %s --record %s' "$in/$name" "$in/record.json"
	if [ "$params" = 1 ]; then printf ' --params-file %s --remove-params-file' "$in/params.env"; fi
}

declare -A ids
layer() {
	local i=$1 id name
	shift
	id=$(printf '%02d' "$i")-$(rnd)
	name=$(stage "$id" "$work/resolve.json" "$i" "$@")
	ids[$name]=$id
	log "слой $i: $name (incoming/$id)"
	customize "$id" "$name" --run-command "$(install_cmd "$id" "$name" "$([ $# -gt 0 ] && echo 1 || echo 0)")"
	eq "$vc_rc" 0 "virt-customize слоя $name"
}

layer 0
layer 1 DOCKER_LOG_LEVEL=debug
layer 2 "FTL_ENV=$envtag" "FTL_TOKEN=$secret"

# Негатив 1: подменённый record.json (ext_digest) — 65 record.mismatch,
# params.env (с другим секретом) удалён и при отказе, секрета в ответе нет.
# Вывод cream и код — в файлы гостя: команда отказа валит virt-customize.
neg() {
	local tag=$1 id=$2 name=$3
	printf '%s >/var/tmp/cream-%s.json 2>/var/tmp/cream-%s.err; echo $? >/var/tmp/cream-%s.rc' \
		"$(install_cmd "$id" "$name" 1)" "$tag" "$tag" "$tag"
}
id=90-$(rnd)
name=$(stage "$id" "$work/resolve.json" 2 "FTL_ENV=$envtag" "FTL_TOKEN=$secret2")
log "негатив: подменённый record.json ($name, incoming/$id)"
# Подмена — до tar: перепаковать с другим ext_digest.
mkdir -p "$work/stage/$id" && tar -C "$work/stage/$id" -xf "$work/$id.tar"
jq '.ext_digest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"' \
	"$work/stage/$id/record.json" >"$work/stage/$id/record.new"
mv "$work/stage/$id/record.new" "$work/stage/$id/record.json"
tar -C "$work/stage/$id" --owner=0 --group=0 --numeric-owner -cf "$work/$id.tar" .
rm -rf -- "$work/stage/$id"
ids[neg-mismatch]=$id
customize "$id" "$name" --run-command "$(neg mismatch "$id" "$name")"
eq "$vc_rc" 0 "virt-customize негатива mismatch (код cream — в файле гостя)"

# Негатив 2: упавший хук install (failing-install, FAIL=1) — 79, policy-rc.d
# снят и при сбое, incoming/<id> оставлен для разбора без params.env.
id=91-$(rnd)
name=$(stage "$id" "$work/resolve-fail.json" 0 FAIL=1)
ids[neg-hook]=$id
log "негатив: упавший хук install ($name, incoming/$id)"
customize "$id" "$name" --run-command "$(neg hook "$id" "$name")"
eq "$vc_rc" 0 "virt-customize негатива hook (код cream — в файле гостя)"

# --- Проверка образа без загрузки -------------------------------------------
log "осмотр образа (guestfish --ro)"
out=$work/out
mkdir -p "$out"
guestfish --ro -a "$img" -i <<EOF >"$out/fish.txt"
tar-out /var/lib/cream $out/varlib.tar numericowner:true
tar-out /etc/cream $out/etc.tar numericowner:true
tar-out /opt/cream $out/opt.tar numericowner:true
tar-out /etc/systemd/system $out/systemd.tar numericowner:true
download /var/tmp/cream-mismatch.json $out/mismatch.json
download /var/tmp/cream-mismatch.rc $out/mismatch.rc
download /var/tmp/cream-hook.json $out/hook.json
download /var/tmp/cream-hook.rc $out/hook.rc
download /var/tmp/cream-hook.err $out/hook.err
echo policy-rc.d:
exists /usr/sbin/policy-rc.d
echo secret-files:
sh "grep -rlF --exclude-dir=proc --exclude-dir=sys --exclude-dir=dev --exclude-dir=run -e $secret / || true"
echo secret2-files:
sh "grep -rlF --exclude-dir=proc --exclude-dir=sys --exclude-dir=dev --exclude-dir=run -e $secret2 / || true"
echo end
EOF
for t in varlib etc opt systemd; do
	mkdir -p "$out/$t"
	tar -C "$out/$t" --no-same-owner -xf "$out/$t.tar"
	tar -tvf "$out/$t.tar" --numeric-owner >"$out/$t.list"
done
section() { awk -v s="$1:" '$0 == s {on = 1; next} /^[a-z0-9.-]+:$|^end$/ {on = 0} on && NF' "$out/fish.txt"; }

# Реестр: phase build, installed, дайджесты — из record.json (resolve).
for i in 0 1 2; do
	name=$(jq -r ".result.layers[$i].name" "$work/resolve.json")
	r=$out/varlib/installed/$name.json
	[ -f "$r" ] || fail "нет installed/$name.json"
	eq "$(jq -r '.phase + " " + .status' "$r")" "build installed" "$name: phase build, status installed"
	eq "$(jq -r .ext_digest "$r")" "$(jq -r ".result.layers[$i].ext_digest" "$work/resolve.json")" "$name: ext_digest = record"
	eq "$(jq -r .params_digest "$r")" "$(jq -r ".result.layers[$i].params_digest" "$work/resolve.json")" \
		"$name: params_digest = record (секрет как <secret>)"
done
eq "$(jq -r .source.name "$out/varlib/installed/fixture-teleport-like.json")" pilot "источник в реестре — pilot"

# Настройки: несекретные — в config, секрет — только в secrets (0600, root).
grep -qx 'DOCKER_LOG_LEVEL=debug' "$out/etc/fixture-docker-like/config" && ok "DOCKER_LOG_LEVEL=debug в config" ||
	fail "DOCKER_LOG_LEVEL=debug нет в /etc/cream/fixture-docker-like/config"
grep -qF "$envtag" "$out/etc/fixture-teleport-like/config" && ok "FTL_ENV в config" || fail "FTL_ENV нет в config"
grep -qF "$secret" "$out/etc/fixture-teleport-like/config" && fail "секрет в config"
grep -qF "$secret" "$out/etc/fixture-teleport-like/secrets" && ok "секрет в secrets" || fail "секрета нет в secrets"
eq "$(awk '$NF ~ /fixture-teleport-like\/secrets$/ {print $1, $2}' "$out/etc.list")" "-rw------- 0/0" "secrets — 0600 root:root"

# Секрет нигде, кроме файла secrets: ни в реестре, ни в history, ни в образе.
grep -rqF "$secret" "$out/varlib" && fail "секрет в /var/lib/cream"
ok "секрета нет в /var/lib/cream (installed, history, state)"
grep -c . "$out/varlib/history.jsonl" >/dev/null || fail "history.jsonl пуст"
eq "$(section secret-files | sort | tr '\n' ' ')" "/etc/cream/fixture-teleport-like/secrets " \
	"секрет в образе — только в /etc/cream/fixture-teleport-like/secrets"
eq "$(section secret2-files | tr -d '\n')" "" "секрет отвергнутого слоя в образе не остался"

# incoming: успешные слои удалены, упавшие — оставлены без params.env.
for n in cloud-user fixture-docker-like fixture-teleport-like; do
	[ -e "$out/varlib/incoming/${ids[$n]}" ] && fail "incoming/${ids[$n]} ($n) не удалён"
	ok "incoming/${ids[$n]} ($n) удалён"
done
for n in neg-mismatch neg-hook; do
	[ -d "$out/varlib/incoming/${ids[$n]}" ] || fail "incoming/${ids[$n]} ($n) удалён при сбое"
	[ -e "$out/varlib/incoming/${ids[$n]}/params.env" ] && fail "params.env ($n) остался"
	ok "$n: incoming/${ids[$n]} оставлен, params.env удалён"
done
if find "$out/varlib/incoming" -maxdepth 1 -name '*.tar' | grep -q .; then fail "архив слоя остался в incoming/"; fi
ok "архивы слоёв из incoming/ удалены после распаковки"
if find "$out/varlib" -name params.env | grep -q .; then fail "params.env остался в /var/lib/cream"; fi
ok "params.env нигде в /var/lib/cream"

# configure только включён ссылкой, policy-rc.d снят, владельцы 0:0.
wants=$out/systemd/multi-user.target.wants
[ -L "$wants/cream-configure@fixture-docker-like.service" ] &&
	ok "ссылка multi-user.target.wants/cream-configure@fixture-docker-like.service" ||
	fail "нет ссылки cream-configure@fixture-docker-like.service в multi-user.target.wants"
[ -L "$wants/fixture-dockerd.service" ] && ok "хук включил fixture-dockerd.service" || fail "fixture-dockerd.service не включён"
[ -e "$out/varlib/ext/fixture-docker-like/configure.log" ] && fail "configure выполнился в фазе build"
ok "configure в фазе build не выполнялся"
eq "$(section policy-rc.d | tr -d '\n')" false "/usr/sbin/policy-rc.d снят"
bad=$(cat "$out/opt.list" "$out/etc.list" "$out/varlib.list" | awk '$2 != "0/0"')
eq "$bad" "" "владелец /opt/cream, /etc/cream, /var/lib/cream — 0:0"

# Негативы.
eq "$(tr -d '\n' <"$out/mismatch.rc")" 65 "подменённый record.json — код 65"
eq "$(jq -r '.error.code + " " + .error.details.field' "$out/mismatch.json")" "record.mismatch ext_digest" \
	"record.mismatch, details.field ext_digest"
eq "$(tr -d '\n' <"$out/hook.rc")" 79 "упавший хук install — код 79"
eq "$(jq -r .error.code "$out/hook.json")" hook.failed "error.code hook.failed"
grep -qF 'сбой по FAIL=1' "$out/hook.err" && ok "stderr хука дошёл до вызывающего" || fail "нет stderr хука: $(cat "$out/hook.err")"
for f in mismatch hook; do
	if grep -qF -e "$secret2" -e "$envtag" -e FAIL=1 <(jq -c '.error' "$out/$f.json"); then
		fail "$f: значение параметра в тексте ошибки: $(jq -c .error "$out/$f.json")"
	fi
	ok "$f: в ошибке нет значений параметров"
done

# argv на время virt-customize: значений параметров нет.
grep -q '^virt-customize ' "$pslog" || fail "журнал ps не застал virt-customize"
if grep -qF -e "$secret" -e "$secret2" -e "$envtag" "$pslog"; then fail "значение параметра в argv процесса"; fi
ok "ps -eo args ($(wc -l <"$pslog") строк) без значений параметров"
echo "$secret $secret2" >"$work/values"
log "все проверки прошли: $pass"
