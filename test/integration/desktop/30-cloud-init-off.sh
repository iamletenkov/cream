#!/usr/bin/env bash
# §6.4, cream 1.1.1: cloud-init установлен, но выключен (/etc/cloud/cloud-init.disabled):
# генератор не подключает cloud-init.target, и экземпляр стадии cloud-user
# держится только на cream-configure-cloud-user.service. Проверка — после
# перезагрузки, 40-boot-cloud-init-off.
# shellcheck source=../scenario/lib.sh
. /it/scenario/lib.sh

step "cloud-init выключен"
touch /etc/cloud/cloud-init.disabled
exists /usr/lib/systemd/system/cloud-final.service "cloud-init установлен"
# Отметка: строк журнала configure до перезагрузки.
for e in configure-cloud-user configure-graphical desktop-like; do
	count "$STATE/$e/configure.log" >"/var/tmp/it-$e.lines"
	ok "$e: $(cat "/var/tmp/it-$e.lines") строк configure.log до загрузки"
done
