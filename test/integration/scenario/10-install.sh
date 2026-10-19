#!/usr/bin/env bash
# §14 шаги 1–3: пакет, источники по file:// с sha256, install цепочки
# cloud-user → fixture-docker-like (--accept-risk) → fixture-teleport-like,
# двойной install — changed:false, configure после install.
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

step "пакет cream"
dpkg -i "$IT/cream.deb" >/dev/null
exists /usr/bin/cream "бинарь /usr/bin/cream"
exists /usr/lib/systemd/system/cream-configure@.service "шаблон cream-configure@"
for d in /etc/cream /var/lib/cream/installed /var/lib/cream/lock; do
	exists "$d" "postinst: $d"
done
c version
want_rc 0 "cream version"

step "каталоги: validate и pack"
c validate --catalog "$IT/src/pilot"
want_rc 0 "validate пилотного каталога"
eq "$(j '[.warnings[].code | select(. != "platform.dev_build")] | length')" 0 "предупреждения validate (кроме dev-сборки)"
mkdir -p /srv/dist
for cat in pilot catalog catalog-next; do
	c catalog pack "$IT/src/$cat" --out "/srv/dist/$cat.tar.gz"
	want_rc 0 "catalog pack $cat"
done

step "источники: add --archive file:// --sha256 --sync"
sum() { sha256sum "$1" | cut -d' ' -f1; }
c source add pilot --archive file:///srv/dist/pilot.tar.gz --sha256 "$(sum /srv/dist/pilot.tar.gz)" --sync
want_rc 0 "source add pilot --sync"
eq "$(j '.result.source.synced')" true "pilot синхронизирован"
c source add fixtures --archive file:///srv/dist/catalog.tar.gz --sha256 "$(sum /srv/dist/catalog.tar.gz)" --sync
want_rc 0 "source add fixtures --sync"
c source add bad --archive file:///srv/dist/pilot.tar.gz --sha256 "$(printf '0%.0s' $(seq 1 64))" --sync
want_rc 69 "sha256 не совпал"
c source show bad
want_rc 66 "запись источника при сбое sync не сохранена"
c source add pilot --archive file:///srv/dist/pilot.tar.gz --sha256 "$(sum /srv/dist/pilot.tar.gz)"
want_rc 0 "повторный source add с теми же полями"
eq "$(j '.result.changed')" false "source add идемпотентен"

step "install цепочки пилота"
# Учётка машины для cloud-user: в ubuntu:24.04 уже есть ubuntu (uid 1000) —
# её и ждём (запасной путь без cloud-init); в debian — создаём pilot.
first_user() { getent passwd | awk -F: '$3 >= 1000 && $3 < 65534 {print $1 ":" $6}' |
	while IFS=: read -r u h; do [ -d "$h" ] && { echo "$u"; break; }; done; }
[ -n "$(first_user)" ] || useradd -m pilot
first_user >"$IT/cloud-user"
CLOUD_USER=$(cat "$IT/cloud-user")
ok "учётка машины: $CLOUD_USER"
c install pilot/fixture-docker-like --accept-risk
want_rc 67 "fixture-docker-like без cloud-user"
c install pilot/cloud-user
want_rc 0 "install cloud-user (library)"
eq "$(j '.result.kind')" library "cloud-user — library"
c install pilot/fixture-docker-like
want_rc 68 "live: confirm без --accept-risk"
c install pilot/fixture-docker-like --accept-risk
want_rc 0 "install fixture-docker-like --accept-risk"
eq "$(j '.result.status')" installed "статус fixture-docker-like"
eq "$(j '.result.configure.stage')" system "стадия configure"
eq "$(j '.result.files | length')" 1 "files fixture-docker-like в реестре"
eq "$(counter $STATE/fixture-docker-like/install.count)" 1 "хук install fixture-docker-like вызван раз"
has $STATE/fixture-docker-like/configure.log "^reason=install user=$CLOUD_USER level=info dockerd=active\$" \
	"configure после install: пользователь из cloud-user, служба поднята"
exists /etc/systemd/system/multi-user.target.wants/cream-configure@fixture-docker-like.service \
	"configure включён в multi-user.target.wants"
exists /etc/systemd/system/cream-configure@fixture-docker-like.service.d/10-stage.conf "drop-in стадии"
has /etc/systemd/system/cream-configure@fixture-docker-like.service.d/10-stage.conf \
	'After=.*fixture-dockerd\.service' "drop-in: After= из манифеста"
eq "$(systemctl is-active fixture-dockerd.service)" active "fixture-dockerd запущен хуком install"

c install pilot/fixture-teleport-like
want_rc 0 "install fixture-teleport-like"
eq "$(counter $STATE/fixture-teleport-like/install.count)" 1 "хук install fixture-teleport-like вызван раз"
eq "$(cat $STATE/fixture-teleport-like/bin-version)" 18.10.0 "«бинарь» версии по умолчанию"
eq "$(systemctl is-enabled fixture-teleport.service)" enabled "служба teleport включена"

step "повторный install — changed:false, хук не вызывается"
c install pilot/fixture-teleport-like
want_rc 0 "повторный install fixture-teleport-like"
eq "$(j '.result.changed')" false "changed"
eq "$(counter $STATE/fixture-teleport-like/install.count)" 1 "хук install не вызван повторно"
c install pilot/fixture-docker-like --accept-risk
want_rc 0 "повторный install fixture-docker-like"
eq "$(j '.result.changed')" false "changed"

c list
want_rc 0 "cream list"
eq "$(j '[.result.extensions[].name] | sort | join(" ")')" \
	"cloud-user fixture-docker-like fixture-teleport-like" "установленные расширения"
eq "$(hist '[.[] | select(.op == "install" and .status == "installed")] | length')" 3 \
	"history.jsonl: три установки, повтор без изменений не пишется"
