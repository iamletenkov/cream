#!/usr/bin/env bash
# §6.5, §14 шаги 3 и 6: install после failed — хук повторяется сам; upgrade из
# источника реестра (Р8) после подъёма пина архива; remove с зависимыми,
# обычный, --purge и --forget.
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

step "install после failed — хук повторяется без флагов"
c install fixtures/failing-install
want_rc 79 "install с FAIL=1"
want_code hook.failed
eq "$(reg failing-install .status)" failed "реестр: failed"
eq "$(counter $STATE/failing-install/install.count)" 1 "хук вызван"
c install fixtures/failing-install
want_rc 79 "повтор того же install"
eq "$(counter $STATE/failing-install/install.count)" 2 "хук повторён автоматически"
c install fixtures/failing-install FAIL=0
want_rc 0 "install с FAIL=0"
eq "$(counter $STATE/failing-install/install.count)" 3 "хук повторён с новым значением"
eq "$(reg failing-install .status)" installed "реестр: installed"
eq "$(hist '[.[] | select(.ext == "failing-install") | .status] | join(" ")')" "failed failed installed" \
	"history.jsonl: две неудачи и успех"

step "upgrade из источника реестра"
c install fixtures/upgrade-fixture
want_rc 0 "install upgrade-fixture 1.0.0"
c upgrade upgrade-fixture
want_rc 0 "upgrade без новой версии в кеше"
eq "$(reg upgrade-fixture .version)" 1.0.0 "версия не изменилась"
eq "$(counter $STATE/upgrade-fixture/upgrade.count)" 0 "хук upgrade не вызван"
c source add fixtures --archive file:///srv/dist/catalog-next.tar.gz \
	--sha256 "$(sha256sum /srv/dist/catalog-next.tar.gz | cut -d' ' -f1)"
want_rc 65 "другой пин без --replace — отказ"
want_code source.conflict
c source add fixtures --archive file:///srv/dist/catalog-next.tar.gz \
	--sha256 "$(sha256sum /srv/dist/catalog-next.tar.gz | cut -d' ' -f1)" --replace --sync
want_rc 0 "пин архива поднят (--replace --sync)"
c upgrade upgrade-fixture
want_rc 0 "upgrade upgrade-fixture"
eq "$(reg upgrade-fixture .version)" 2.0.0 "реестр: 2.0.0"
eq "$(reg upgrade-fixture .source.catalog_version)" 1.1.0 "реестр: версия каталога"
has $STATE/upgrade-fixture/upgrade.log '^phase=live reason= prev=1.0.0 changed= version=2.0.0$' \
	"хук upgrade: CREAM_PREV_VERSION=1.0.0"
absent /opt/cream/ext/.upgrade-fixture.prev "прежнее дерево убрано после успеха"
eq "$(hist '[.[] | select(.op == "upgrade" and .ext == "upgrade-fixture")] | .[-1] | "\(.from)>\(.to) \(.status)"')" \
	"1.0.0>2.0.0 installed" "history.jsonl: upgrade"

step "remove"
c remove cloud-user
want_rc 67 "remove библиотеки, от которой зависят"
want_code deps.dependents
exists /var/lib/cream/installed/cloud-user.json "cloud-user на месте"
c remove upgrade-fixture
want_rc 0 "remove upgrade-fixture"
eq "$(counter $STATE/upgrade-fixture/remove.count)" 1 "хук remove вызван"
absent /var/lib/cream/installed/upgrade-fixture.json "запись реестра снята"
absent /opt/cream/ext/upgrade-fixture "дерево снято"
exists $STATE/upgrade-fixture "без --purge состояние сохранено"
c remove --purge failing-install
want_rc 0 "remove --purge failing-install"
absent $STATE/failing-install "--purge: состояние удалено"
absent /etc/cream/failing-install "--purge: настройки удалены"
c remove upgrade-fixture
want_rc 66 "remove неустановленного"

step "remove --forget — без хука"
c install fixtures/noop
want_rc 0 "install noop (из каталога 1.1.0)"
c remove --forget noop
want_rc 0 "remove --forget noop"
eq "$(counter $STATE/noop/remove.count)" 0 "хук remove не вызван"
absent /var/lib/cream/installed/noop.json "запись реестра снята"
absent /opt/cream/ext/noop "дерево снято"
