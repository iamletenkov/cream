#!/usr/bin/env bash
# §6.4, рабочий стол в облаке (gdm3 + cloud-init): стадия cloud-user (cream
# 1.1.1) — в .wants вспомогательного cream-configure-cloud-user.service, тот — в
# multi-user.target.wants без порядка относительно экземпляров (After=cloud-final
# экземпляра цикла не даёт, и выключенный cloud-init его не блокирует), graphical —
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
PU=cream-configure-cloud-user.service
inst fixtures/configure-cloud-user $PU
exists $W/multi-user.target.wants/$PU "cloud-user: $PU в multi-user.target.wants"
# Раскладка cream 1.1.0 (ссылка в cloud-init.target.wants, цель в реестре):
# system init переносит её к вспомогательному юниту и обновляет реестр.
mkdir -p $W/cloud-init.target.wants
mv $W/$PU.wants/cream-configure@configure-cloud-user.service $W/cloud-init.target.wants/
jq '.configure.target = "cloud-init.target"' /var/lib/cream/installed/configure-cloud-user.json >/tmp/cu.json
cat /tmp/cu.json >/var/lib/cream/installed/configure-cloud-user.json
c system init
want_rc 0 "system init: переход с 1.1.0"
eq "$(reg configure-cloud-user .configure.target)" $PU "cloud-user: цель в реестре после system init"
exists $W/$PU.wants/cream-configure@configure-cloud-user.service "cloud-user: ссылка в $PU.wants"
absent $W/cloud-init.target.wants/cream-configure@configure-cloud-user.service "cloud-user не в cloud-init.target.wants"
absent $W/multi-user.target.wants/cream-configure@configure-cloud-user.service "cloud-user не в multi-user.target.wants"
inst fixtures/configure-graphical graphical.target
inst desk/desktop-like multi-user.target
for u in cream-configure@configure-cloud-user.service cream-configure@configure-graphical.service \
	cream-configure@desktop-like.service $PU cloud-final.service multi-user.target; do
	systemd-analyze verify "$u" >/tmp/verify.txt 2>&1 || true
	hasnt /tmp/verify.txt 'cycle' "systemd-analyze verify $u: без цикла"
done

step "отметки configure сняты — загрузка выполнит configure, как первая из образа"
# Прогон configure до перезагрузки записал отметки выполнения (§10.7): без
# них следующая загрузка проверяет порядок и запуск configure стадий.
for e in configure-cloud-user configure-graphical desktop-like; do
	rm -f "/var/lib/cream/configured/$e.json"
	absent "/var/lib/cream/configured/$e.json" "$e: отметки нет"
done
