#!/usr/bin/env bash
# §6.4, cream 1.1.0: стадии configure early, cloud-user, graphical. Установка
# включает экземпляр в цель стадии, пишет drop-in 10-stage.conf с порядком
# стадии и сразу выполняет configure; systemd принимает порядок без циклов.
# Проверка на загрузке — 62-stages-boot (после перезагрузки 60-after-boot).
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"
W=/etc/systemd/system

# Источник fixtures после 50-upgrade-remove указывает на catalog-next: фикстуры
# стадий — из своего источника (path на каталог фикстур).
c source add stages --path "$IT/src/catalog" --sync
want_rc 0 "source add stages"

# stage_ok <ext> <стадия> <цель> — install, запись реестра, ссылка и configure.
stage_ok() {
	c install "stages/$1"
	want_rc 0 "install $1"
	eq "$(j '.result.configure.stage')" "$2" "$1: стадия"
	eq "$(reg "$1" .configure.target)" "$3" "$1: цель в реестре"
	exists "$W/$3.wants/cream-configure@$1.service" "$1: ссылка в $3.wants"
	eq "$(tail -n 1 "$STATE/$1/configure.log")" "phase=live reason=install version=1.0.0" "$1: configure после install"
}

step "early: sysinit.target, до сети"
stage_ok configure-early early sysinit.target
D=$W/cream-configure@configure-early.service.d/10-stage.conf
has $D '^DefaultDependencies=no$' "early: DefaultDependencies=no"
has $D '^After=local-fs.target$' "early: After=local-fs.target"
has $D '^Before=network-pre.target sysinit.target shutdown.target$' "early: Before=network-pre.target sysinit.target shutdown.target"
has $D '^Conflicts=shutdown.target$' "early: Conflicts=shutdown.target (1.1.1)"
has $D '^Wants=network-pre.target$' "early: Wants=network-pre.target"

step "cloud-user: без cloud-init — вспомогательный юнит в multi-user.target.wants (1.1.1)"
[ ! -e /usr/lib/systemd/system/cloud-final.service ] || fail "в образе есть cloud-init"
stage_ok configure-cloud-user cloud-user cream-configure-cloud-user.service
has $W/cream-configure@configure-cloud-user.service.d/10-stage.conf '^After=cloud-final.service$' \
	"cloud-user: After=cloud-final.service"
exists $W/multi-user.target.wants/cream-configure-cloud-user.service "cloud-user: вспомогательный юнит в multi-user.target.wants"
exists /usr/lib/systemd/system/cream-configure-cloud-user.service "пакет: cream-configure-cloud-user.service"

step "graphical: graphical.target, после менеджера входа"
stage_ok configure-graphical graphical graphical.target
has $W/cream-configure@configure-graphical.service.d/10-stage.conf '^After=multi-user.target display-manager.service gdm.service$' \
	"graphical: After=display-manager.service + after манифеста"

step "systemd видит порядок стадий"
show() { systemctl show -p "$2" --value "cream-configure@$1.service" | tr ' ' '\n'; }
show configure-early After >/tmp/early-after.txt
show configure-early Before >/tmp/early-before.txt
has /tmp/early-after.txt '^local-fs.target$' "early: After содержит local-fs.target"
hasnt /tmp/early-after.txt '^(basic|sysinit).target$' "early: без зависимостей по умолчанию (нет After=basic/sysinit)"
has /tmp/early-before.txt '^sysinit.target$' "early: Before содержит sysinit.target"
has /tmp/early-before.txt '^network-pre.target$' "early: Before содержит network-pre.target"
show configure-cloud-user After >/tmp/cu-after.txt
has /tmp/cu-after.txt '^cloud-final.service$' "cloud-user: After содержит cloud-final.service"
show configure-graphical After >/tmp/gr-after.txt
has /tmp/gr-after.txt '^display-manager.service$' "graphical: After содержит display-manager.service"
has /tmp/gr-after.txt '^multi-user.target$' "graphical: After содержит multi-user.target"
for e in configure-early configure-cloud-user configure-graphical; do
	systemd-analyze verify "cream-configure@$e.service" >/tmp/verify-$e.txt 2>&1 || true
	hasnt /tmp/verify-$e.txt 'cycle' "systemd-analyze verify $e: без цикла"
done

step "cream 1.1 стадии требуют cream: >=1.1.0"
mkdir -p /tmp/low/hooks
cp "$IT/src/catalog/extensions/configure-early/hooks/"* /tmp/low/hooks/
sed 's/^name: configure-early/name: low/; s/^cream: .*/cream: ">=1.0.0"/' \
	"$IT/src/catalog/extensions/configure-early/extension.yaml" >/tmp/low/extension.yaml
c validate /tmp/low
want_rc 65 "early при cream: >=1.0.0"
want_code manifest.invalid "early при cream: >=1.0.0"
