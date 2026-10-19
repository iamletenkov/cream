#!/usr/bin/env bash
# §6.4, cream 1.1.0, рабочий стол в облаке (gdm3 + cloud-init): стадия
# cloud-user включается в cloud-init.target (cloud-final упорядочен после
# multi-user.target — ссылка из multi-user.target.wants дала бы цикл), graphical —
# в graphical.target после менеджера входа, а «desktop» (Р5 плана каталога) —
# стадия system до менеджера входа. Проверка на загрузке — 20-boot.
# shellcheck source=../scenario/lib.sh
. /it/scenario/lib.sh
W=/etc/systemd/system

step "пакет и машина"
dpkg -i "$IT/cream.deb" >/dev/null
exists /usr/bin/cream "бинарь /usr/bin/cream"
exists /usr/lib/systemd/system/cloud-final.service "cloud-init: cloud-final.service"
exists /usr/lib/systemd/system/cloud-init.target "cloud-init: cloud-init.target"
exists /usr/lib/systemd/system/gdm.service "gdm3: gdm.service"
# Менеджер входа и цель по умолчанию — как на настольной машине.
[ -e $W/display-manager.service ] || ln -s /usr/lib/systemd/system/gdm.service $W/display-manager.service
systemctl set-default graphical.target >/dev/null 2>&1
eq "$(systemctl show -p Id --value display-manager.service)" gdm.service "display-manager.service — gdm"
# cloud-init в контейнере: источник NoCloud из seed, сеть не трогает.
mkdir -p /var/lib/cloud/seed/nocloud
printf 'instance-id: cream-it\nlocal-hostname: cream-it\n' >/var/lib/cloud/seed/nocloud/meta-data
printf '#cloud-config\n{}\n' >/var/lib/cloud/seed/nocloud/user-data
printf 'datasource_list: [NoCloud, None]\nnetwork: {config: disabled}\n' >/etc/cloud/cloud.cfg.d/99-cream-it.cfg
printf 'policy: enabled\n' >/etc/cloud/ds-identify.cfg
ok "cloud-init: NoCloud seed"

step "источник: каталог фикстур и расширение «desktop» (стадия system до менеджера входа)"
mkdir -p /srv/desk/extensions/desktop-like/hooks
printf 'schema: cream/v1\nname: desk\nversion: 1.0.0\nroots: [extensions]\n' >/srv/desk/cream-catalog.yaml
cat >/srv/desk/extensions/desktop-like/extension.yaml <<'YAML'
schema: cream/v1
name: desktop-like
version: 1.0.0
summary: модель расширения desktop (Р5)
cream: ">=1.0.0"
platform: {family: deb, arch: [amd64, arm64]}
hooks:
  install: {}
  configure:
    stage: system
    after: [cloud-init.service, guestfs-firstboot.service]
    before: [display-manager.service, gdm.service, gdm3.service, lightdm.service]
YAML
cp "$IT/src/catalog/extensions/configure-system/hooks/install" /srv/desk/extensions/desktop-like/hooks/
cp "$IT/src/catalog/extensions/configure-early/hooks/configure" /srv/desk/extensions/desktop-like/hooks/
c source add fixtures --path "$IT/src/catalog" --sync
want_rc 0 "source add fixtures"
c source add desk --path /srv/desk --sync
want_rc 0 "source add desk"

step "install: цели стадий по машине"
inst() {
	c install "$1"
	want_rc 0 "install $1"
	eq "$(reg "${1#*/}" .configure.target)" "$2" "${1#*/}: цель $2"
	exists "$W/$2.wants/cream-configure@${1#*/}.service" "${1#*/}: ссылка в $2.wants"
}
inst fixtures/configure-cloud-user cloud-init.target
# cloud-init «исчез» и вернулся: system init переносит ссылку и цель в реестре.
mv /usr/lib/systemd/system/cloud-final.service /tmp/cloud-final.service
c system init
want_rc 0 "system init без cloud-init"
eq "$(reg configure-cloud-user .configure.target)" multi-user.target "cloud-user: цель в реестре без cloud-init"
exists $W/multi-user.target.wants/cream-configure@configure-cloud-user.service "cloud-user: ссылка в multi-user.target.wants"
mv /tmp/cloud-final.service /usr/lib/systemd/system/cloud-final.service
c system init
want_rc 0 "system init с cloud-init"
eq "$(reg configure-cloud-user .configure.target)" cloud-init.target "cloud-user: цель в реестре с cloud-init"
absent $W/multi-user.target.wants/cream-configure@configure-cloud-user.service "cloud-user не в multi-user.target.wants"
inst fixtures/configure-graphical graphical.target
inst desk/desktop-like multi-user.target
for e in configure-cloud-user configure-graphical desktop-like; do
	systemd-analyze verify "cream-configure@$e.service" >/tmp/verify-$e.txt 2>&1 || true
	hasnt /tmp/verify-$e.txt 'cycle' "systemd-analyze verify $e: без цикла"
done
