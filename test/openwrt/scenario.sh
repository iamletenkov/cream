#!/bin/sh
# Сценарий test-openwrt внутри контейнера с корнем OpenWrt 25.12 (busybox sh).
# /t — только чтение: cream_linux_amd64.tar.gz, catalog (testdata/openwrt).
set -eu

step() { echo "== $*"; }
fail() {
	echo "FAIL: $*" >&2
	exit 1
}
has() { [ -e "$1" ] || fail "нет $1"; }
hasnt() { [ ! -e "$1" ] || fail "лишнее $1"; }
grep_q() { grep -q "$1" "$2" || fail "в $2 нет «$1»: $(cat "$2")"; }

step "окружение: $(. /etc/os-release && echo "$PRETTY_NAME"), pid1=$(cat /proc/1/comm)"
. /etc/os-release
[ "$ID" = openwrt ] || fail "не OpenWrt: $ID"
[ -L /var ] || fail "/var не ссылка на tmpfs"
command -v apk >/dev/null || fail "нет apk"
[ "$(readlink -f /bin/sh)" = /bin/busybox ] || fail "sh не busybox"

step "установка cream из tar.gz (busybox tar)"
mkdir -p /opt/cream-dist
tar -xzf /t/cream_linux_amd64.tar.gz -C /opt/cream-dist
install -m 0755 /opt/cream-dist/cream /usr/bin/cream 2>/dev/null || cp /opt/cream-dist/cream /usr/bin/cream
chmod 0755 /usr/bin/cream
cream version

step "cream system init: init-скрипты procd, /opt/cream/var"
cream system init
has /etc/init.d/cream-configure
has /etc/init.d/cream-configure-early
[ "$(readlink /etc/rc.d/S95cream-configure)" = ../init.d/cream-configure ] || fail "S95cream-configure"
[ "$(readlink /etc/rc.d/S19cream-configure-early)" = ../init.d/cream-configure-early ] || fail "S19cream-configure-early"
has /opt/cream/var/installed
has /opt/cream/var/tmp
hasnt /usr/lib/systemd/system/cream-configure@.service
hasnt /var/lib/cream
cream system init --json | grep -q '"unit": "cream-configure"' || fail "system init --json"

step "источник path"
cream source add owrt --path /t/catalog --sync
cream source list

step "фаза build по PID 1 (не procd): IPKG_INSTROOT=/, TMPDIR на диске, configure только включён"
cream install fixture-openwrt 2>/tmp/build.err || { cat /tmp/build.err; fail "install build"; }
grep -q 'фаза build (pid1=' /tmp/build.err || fail "фаза не build: $(cat /tmp/build.err)"
s=/opt/cream/var/ext/fixture-openwrt
grep_q 'phase=build reason= tmpdir=/opt/cream/var/tmp instroot=/ ' $s/install.log
hasnt $s/configure.count
has /opt/cream/var/stages/system/fixture-openwrt
hasnt /usr/sbin/policy-rc.d
cream remove --purge fixture-openwrt
hasnt /opt/cream/var/stages/system/fixture-openwrt
hasnt /opt/cream/ext/fixture-openwrt
hasnt $s

step "фаза build: apk add в хуке передаёт IPKG_INSTROOT=/ скриптам пакета (APK_CONFIG с preserve-env)"
# /t/envprobe.apk собран run.sh (apk mkpkg в Alpine: в apk OpenWrt mkpkg нет):
# post-install пишет IPKG_INSTROOT, который видит скрипт пакета.
mkdir -p /opt/t-apk/extensions/apk-probe/hooks
cat >/opt/t-apk/cream-catalog.yaml <<'YAML'
schema: cream/v1
name: t-apk
version: 1.0.0
roots: [extensions]
YAML
cat >/opt/t-apk/extensions/apk-probe/extension.yaml <<'YAML'
schema: cream/v1
name: apk-probe
version: 1.0.0
summary: apk add из хука
cream: ">=1.3.0"
platform: {family: openwrt, arch: [amd64, arm64]}
hooks: {install: {}}
YAML
printf '#!/bin/sh\nset -e\napk add --allow-untrusted --no-network /t/envprobe.apk\n' \
	>/opt/t-apk/extensions/apk-probe/hooks/install
chmod 0755 /opt/t-apk/extensions/apk-probe/hooks/install
cream source add tapk --path /opt/t-apk --sync
cream install apk-probe
grep_q 'IPKG_INSTROOT=/$' /opt/envprobe.out
grep_q 'preserve-env' /opt/cream/var/apk-build.conf
cream remove --forget apk-probe

step "фаза live: install → configure сразу (reason install), без systemd"
cream install --phase live fixture-openwrt MESSAGE=привет
cream install --phase live fixture-openwrt-early
grep_q 'phase=live reason= tmpdir=/opt/cream/var/tmp instroot=unset ' $s/install.log
grep_q 'phase=live reason=install ' $s/configure.log
[ "$(cat $s/message)" = привет ] || fail "message"
e=/opt/cream/var/ext/fixture-openwrt-early
grep_q 'reason=install ' $e/configure.log
has /opt/cream/var/stages/system/fixture-openwrt
has /opt/cream/var/stages/early/fixture-openwrt-early
has /opt/cream/var/configured/fixture-openwrt-early.json
grep -q '"target": *"S19cream-configure-early"' /opt/cream/var/installed/fixture-openwrt-early.json || fail "target early в реестре: $(cat /opt/cream/var/installed/fixture-openwrt-early.json)"

step "загрузка: rc.d boot — early пропущен по отметке, system (every_boot) отработал"
echo junk >/opt/cream/var/tmp/leftover
echo junk >/opt/cream/var/tmp/.hidden
/etc/rc.d/S19cream-configure-early boot
/etc/rc.d/S95cream-configure boot
hasnt /opt/cream/var/tmp/leftover
hasnt /opt/cream/var/tmp/.hidden
has /opt/cream/var/tmp
[ "$(cat $e/configure.count)" = 1 ] || fail "early configure.count=$(cat $e/configure.count)"
[ "$(cat $s/configure.count)" = 2 ] || fail "system configure.count=$(cat $s/configure.count)"
grep_q 'reason=boot ' $s/configure.log

step "cream configure вручную (apply) и cream set"
cream configure fixture-openwrt
grep_q 'reason=apply ' $s/configure.log
cream set fixture-openwrt MESSAGE=мир
[ "$(cat $s/message)" = мир ] || fail "message после set"

step "remove --purge: ничего не осталось"
cream remove --purge fixture-openwrt
cream remove --purge fixture-openwrt-early
for p in /opt/cream/var/stages/system/fixture-openwrt /opt/cream/var/stages/early/fixture-openwrt-early \
	/opt/cream/ext/fixture-openwrt /opt/cream/ext/fixture-openwrt-early $s $e \
	/etc/cream/fixture-openwrt /opt/cream/var/installed/fixture-openwrt.json; do
	hasnt "$p"
done
# Загрузка без расширений — не ошибка.
/etc/rc.d/S95cream-configure boot
cream list

step "расширение семейства deb на OpenWrt — 68 platform.family"
mkdir -p /opt/t-deb/extensions/deb-only/hooks
cat >/opt/t-deb/cream-catalog.yaml <<'YAML'
schema: cream/v1
name: t-deb
version: 1.0.0
roots: [extensions]
YAML
cat >/opt/t-deb/extensions/deb-only/extension.yaml <<'YAML'
schema: cream/v1
name: deb-only
version: 1.0.0
summary: только deb
cream: ">=1.0.0"
platform: {family: deb, arch: [amd64, arm64]}
hooks: {install: {}}
YAML
printf '#!/bin/sh\nexit 0\n' >/opt/t-deb/extensions/deb-only/hooks/install
chmod 0755 /opt/t-deb/extensions/deb-only/hooks/install
cream source add tdeb --path /opt/t-deb --sync
rc=0
cream install --phase live --json deb-only >/tmp/deb.json 2>/dev/null || rc=$?
[ "$rc" = 68 ] || fail "deb-only: код $rc"
grep -q '"platform.family"' /tmp/deb.json || fail "deb-only: $(cat /tmp/deb.json)"

echo "== OK"
