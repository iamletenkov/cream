#!/usr/bin/env bash
# Р11: fixture-agent-like через files кладёт drop-in командной метки в каталог
# настроек fixture-teleport-like. Владение — по реестру A: файл в его записи,
# не в записи B; повторный install его не трогает; remove A убирает только
# свой файл, остальное в /etc/cream/fixture-teleport-like и реестр B целы.
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"
A=fixture-agent-like B=fixture-teleport-like
DST=/etc/cream/$B/command-labels.d/agent

# snapshot — содержимое каталога B без файла A и запись реестра B.
snapshot() {
	(cd /etc/cream/$B && find . -type f ! -path ./command-labels.d/agent -print0 | sort -z | xargs -0 sha256sum)
	sha256sum /var/lib/cream/installed/$B.json
}

step "install A кладёт файл в каталог B"
before=$(snapshot)
c install pilot/$A
want_rc 0 "install $A"
exists $DST "файл A в каталоге B"
eq "$(stat -c '%a %U' $DST)" "644 root" "режим и владелец"
eq "$(reg $A '[.files[].dst] | join(" ")')" "$DST" "файл в реестре A"
eq "$(reg $B "[.files[].dst | select(. == \"$DST\")] | length")" 0 "файла нет в реестре B"
eq "$(reg $A ".files[0].sha256")" "$(sha256sum $DST | cut -d' ' -f1)" "sha256 в реестре A"
eq "$(snapshot)" "$before" "каталог и реестр B не тронуты"

step "повторный install — changed:false, файл цел"
sum=$(sha256sum $DST)
c install pilot/$A
want_rc 0 "повторный install $A"
eq "$(j '.result.changed')" false "changed"
eq "$(sha256sum $DST)" "$sum" "файл тот же"
eq "$(counter $STATE/$A/install.count)" 1 "хук install не вызван повторно"

step "remove A убирает только свой файл"
c remove $A
want_rc 0 "remove $A"
absent $DST "файл A убран"
exists /etc/cream/$B/command-labels.d/00-base "файл B в том же каталоге на месте"
eq "$(snapshot)" "$before" "каталог и реестр B не тронуты"

step "A снова установлен (для загрузки и purge)"
c install pilot/$A
want_rc 0 "install $A"
exists $DST "файл A на месте"
