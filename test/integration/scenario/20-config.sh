#!/usr/bin/env bash
# §14 шаги 3 и 6: set/show/get, apply хуком и перезапуском configure,
# readonly, секрет со stdin, ключ scope: install — повтор хука install через
# cream install и отказ 65 у cream set.
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"
T=fixture-teleport-like D=fixture-docker-like

step "set с хуком apply"
c set $T FTL_ENV=prod
want_rc 0 "set FTL_ENV"
eq "$(j '.result.apply')" hook "применение — хук apply"
eq "$(j '.result.changed_keys | join(" ")')" FTL_ENV "изменённые ключи"
has $STATE/$T/apply.log '^changed=FTL_ENV env=prod$' "apply получил CREAM_CHANGED_KEYS и новое значение"
has /etc/cream/$T/config '^FTL_ENV=prod$' "config: FTL_ENV=prod"
c set $T FTL_ENV=prod
want_rc 0 "set того же значения"
eq "$(j '.result.changed')" false "changed"
eq "$(count $STATE/$T/apply.log)" 1 "apply без изменений не вызывается"
c set $T FTL_ENV=Bad_Value
want_rc 65 "значение не по типу"
c set $T FTL_PROXY=evil.example:443
want_rc 65 "readonly без --allow-readonly"
want_code config.readonly
c get $T FTL_ENV
want_rc 0 "get"
eq "$(j '.result.value // .result')" prod "get FTL_ENV"

step "секрет: только KEY=-, в secrets 0600, маска в show"
c set $T "FTL_TOKEN=$TOKEN_SECRET"
want_rc 64 "секрет в argv"
want_code secret.in_argv
c set --no-apply $T FTL_TOKEN=- < <(printf '%s' "$TOKEN_SECRET")
want_rc 0 "set FTL_TOKEN=- со stdin"
eq "$(stat -c '%a %U' /etc/cream/$T/secrets)" "600 root" "secrets — 0600 root"
eq "$(stat -c '%a' /etc/cream/$T/config)" 644 "config — 0644"
hasnt /etc/cream/$T/config "$TOKEN_SECRET" "секрета нет в config"
c show $T
want_rc 0 "show"
eq "$(j '.result.keys[] | select(.key == "FTL_TOKEN") | .value')" '(задан)' "секрет в show — маска (§8.3)"
c get --secret $T FTL_TOKEN
want_rc 0 "get --secret от root"
c config export --shell $T
want_rc 0 "config export --shell"
if printf '%s' "$OUT" | grep -q "$TOKEN_SECRET"; then fail "секрет в config export"; fi
ok "секрета нет в config export"

step "применение перезапуском configure (без хука apply)"
before=$(count $STATE/$D/configure.log)
c set --wait $D DOCKER_LOG_LEVEL=debug
want_rc 0 "set --wait DOCKER_LOG_LEVEL"
eq "$(j '.result.apply')" restart "применение — restart configure"
wait_until 30 grep -q 'level=debug' $STATE/$D/configure.log || fail "configure не перезапущен"
eq "$(count $STATE/$D/configure.log)" $((before + 1)) "configure вызван раз"
has $STATE/$D/configure.log "^reason=apply user=$CLOUD_USER level=debug dockerd=active\$" "CREAM_REASON=apply через маркер"
eq "$(systemctl is-active cream-configure@$D.service)" active "cream-configure@$D активен"
c set --no-apply $D DOCKER_LOG_LEVEL=warn
want_rc 0 "set --no-apply"
eq "$(count $STATE/$D/configure.log)" $((before + 1)) "--no-apply не перезапускает configure"

step "scope: install — set отказывает, install с новым значением повторяет хук"
c set $T FTL_VERSION=18.11.0
want_rc 65 "set ключа scope: install"
want_code config.install_scope
c install pilot/$T FTL_VERSION=18.10.0
want_rc 0 "install с тем же значением scope: install"
eq "$(j '.result.changed')" false "changed"
eq "$(counter $STATE/$T/install.count)" 1 "хук install не вызван"
c install pilot/$T FTL_VERSION=18.11.0
want_rc 0 "install с новым FTL_VERSION"
eq "$(j '.result.changed')" true "changed"
eq "$(counter $STATE/$T/install.count)" 2 "хук install вызван повторно"
eq "$(cat $STATE/$T/bin-version)" 18.11.0 "хук видел новое значение"
has /etc/cream/$T/config '^FTL_ENV=prod$' "повторный install не тронул правку оператора"
