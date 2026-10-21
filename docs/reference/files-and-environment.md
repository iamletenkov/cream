# Файлы и переменные окружения

## Пути на машине

С `--root DIR` (или `CREAM_ROOT`) все пути ниже переносятся под `DIR`.

| Путь | Что | Права |
|---|---|---|
| `/usr/bin/cream` | бинарь | 0755 |
| `/usr/lib/systemd/system/cream-configure@.service` | шаблон юнита настройки на загрузке | 0644 |
| `/usr/lib/systemd/system/cream-configure-cloud-user.service` | юнит, который запускает экземпляры стадии `cloud-user` | 0644 |
| `/usr/lib/cream/guest/linux-{amd64,arm64}/cream` | бинари для гостя при сборке образа (только из `.deb`) | 0755 |
| `/etc/cream/sources.yaml` | источники ([формат](manifest.md#sourcesyaml)) | 0644 |
| `/etc/cream/<ext>/config` | несекретные настройки расширения, `EnvironmentFile` | 0644 |
| `/etc/cream/<ext>/secrets` | секреты расширения | 0600 |
| `/etc/systemd/system/cream-configure@<ext>.service.d/10-stage.conf` | порядок стадии `configure` | 0644 |
| `/opt/cream/ext/<ext>/` | дерево расширения; рабочий каталог хуков | 0755 |
| `/var/lib/cream/installed/<ext>.json` | запись реестра (`cream-state/v1`) | 0644 |
| `/var/lib/cream/configured/<ext>.json` | отметка выполнения configure `{version, ext_digest, params_digest, secrets_rev, machine_id, at}`, без значений | 0644 |
| `/var/lib/cream/history.jsonl` | журнал операций `{time, op, ext, from, to, status, phase}`, без параметров | 0644 |
| `/var/lib/cream/sources.lock` | что синхронизировано: sha256 или отпечаток, версия каталога, время | 0644 |
| `/var/lib/cream/ext/<ext>/` | состояние расширения (`$CREAM_STATE_DIR`) | 0755 |
| `/var/lib/cream/lock/{global,<ext>}` | файлы блокировок | 0600, каталог 0700 |
| `/var/lib/cream/incoming/<id>/` | приёмник фазы build | 0700 |
| `/var/cache/cream/sources/<источник>/` | кеш источника | 0755 |
| `/etc/image-labels.d/cream.json` | метки образа `{cream.version, cream.extensions, cream.sources}` по всему реестру — без параметров и путей; переписывается при каждой записи реестра (`install`, `upgrade`, `remove`, `hold`, `set`, код 10 хука, `system init`), атомарно; сбой — предупреждение `labels.write_failed`. bisquite переносит их в метки OCI | 0644 |
| `/run/cream/<ext>.reason` | причина следующего прогона `configure` (`install`, `upgrade`, `apply`, `followup`); юнит читает и удаляет | 0644 |
| `/run/cream/configure/<ext>.result` | исход последнего прогона хука юнитом `{status: ok\|skipped\|failed, reboot_required}` — его читает `cream configure --wait` | 0644 |
| `/run/reboot-required` | создаётся, если хук вернул 10 | |

**OpenWrt** (семейство `openwrt`, cream 1.3, [ADR 0005](../adr/0005-openwrt-family-procd.md)): `/var` там —
tmpfs, поэтому всё, что выше лежит в `/var/lib/cream`, лежит в **`/opt/cream/var`** (`installed/`, `configured/`,
`history.jsonl`, `sources.lock`, `ext/<ext>/`, `lock/`, `incoming/`), кеш источников — в `/opt/cream/var/cache`,
`TMPDIR` хуков — `/opt/cream/var/tmp` (очищается скриптом `S19` при загрузке), а `reason`, `result` и `reboot-required` — в `/var/run`. Юнитов systemd нет:

| Путь | Что | Права |
|---|---|---|
| `/etc/init.d/cream-configure-early`, `/etc/rc.d/S19cream-configure-early` | configure стадии `early` (до `S20network`) | 0755 |
| `/etc/init.d/cream-configure`, `/etc/rc.d/S95cream-configure` | configure стадий `system` и `cloud-user` | 0755 |
| `/opt/cream/var/stages/<стадия>/<ext>` | configure расширения включён на стадии (пустой файл) | 0644 |

**Режим пользователя.** Без root и без `--root` cream работает со своими файлами:
источники — `$XDG_CONFIG_HOME/cream/sources.yaml` (`~/.config`), кеш —
`$XDG_CACHE_HOME/cream/` (`~/.cache`), `sources.lock` — `$XDG_STATE_HOME/cream/`
(`~/.local/state`). С `--cache DIR` файл `sources.lock` лежит в `DIR`.

## Переменные окружения cream

| Переменная | Смысл |
|---|---|
| `CREAM_ROOT` | то же, что `--root` |
| `CREAM_SOURCES` | то же, что `--sources` |
| `CREAM_CACHE` | то же, что `--cache` |
| `XDG_CONFIG_HOME`, `XDG_CACHE_HOME`, `XDG_STATE_HOME` | пути режима пользователя |
| `SOURCE_DATE_EPOCH` | время файлов в архиве `cream catalog pack` (без неё — 0) |
| `CREAM_PHASE` | фаза (`build` или `live`) для вложенного вызова cream из хука; без флага `--phase` и без переменной фаза определяется по PID 1: `systemd` и `/run/systemd/system` — `live`, другой процесс (appliance `virt-customize`, контейнер без systemd) — `build`, `/proc` не читается — 64 `phase.undetermined` |
| `CREAM_LOCK_FD`, `CREAM_LOCK_MODE` | унаследованная блокировка для вложенного вызова (ставит сам cream) |

Флаг командной строки сильнее переменной.

## Окружение хуков и действий

cream запускает хук и действие напрямую, без оболочки, с **чистым** окружением:
рабочий каталог — `/opt/cream/ext/<ext>`, `umask 022`, stdin — `/dev/null`
(кроме действий со `stdin: secret|pass`).

| Переменная | Значение |
|---|---|
| `PATH` | `/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin` |
| `LANG` | `C.UTF-8` |
| `CREAM_PHASE` | `build` или `live` |
| `CREAM_EXT`, `CREAM_EXT_VERSION` | имя и версия расширения |
| `CREAM_PREV_VERSION` | прежняя версия — при обновлении |
| `CREAM_EXT_DIR` | `/opt/cream/ext/<ext>` |
| `CREAM_ETC_DIR` | `/etc/cream/<ext>` |
| `CREAM_STATE_DIR` | `/var/lib/cream/ext/<ext>` |
| `CREAM_ROOT` | корень (`/` или значение `--root`) |
| `CREAM_CONFIG_FILE`, `CREAM_SECRETS_FILE` | файлы настроек и секретов |
| `CREAM_REASON` | у `configure`: `boot`, `install`, `upgrade`, `apply` или `followup` (повтор, поставленный `cream configure --again`) |
| `CREAM_CHANGED_KEYS` | у `apply`: изменённые ключи через пробел |
| `CREAM_ACTION`, `CREAM_OUTPUT` | у действия: имя и формат вывода (`json` или `text`) |
| `CREAM_LOCK_FD`, `CREAM_LOCK_MODE` | дескриптор и режим удерживаемой блокировки |
| ключи настроек | все **несекретные** ключи `config.keys` под своими именами |
| ключи `scope: build` | только у `install`/`upgrade` текущей операции: переданные значения и умолчания, секретные тоже ([манифест](manifest.md#ключи-scope-build)) |
| `TMPDIR` | только на OpenWrt: `/opt/cream/var/tmp` (на диске; `/tmp` там — ОЗУ) |
| `IPKG_INSTROOT` | только на OpenWrt в фазе build: `/` — скрипты пакетов `apk add` включают службы без запуска, `uci-defaults` ждут первой загрузки (аналог `policy-rc.d`) |
| `APK_CONFIG` | только на OpenWrt в фазе build: `/opt/cream/var/apk-build.conf` — `/etc/apk/config` образа плюс `preserve-env`, чтобы apk 3 передал `IPKG_INSTROOT` скриптам пакетов |

Секреты в окружение не попадают: хук читает их из `$CREAM_SECRETS_FILE` или
`cream config get --secret "$CREAM_EXT" KEY`. Вывод хука и действия проходит
фильтр: известные значения секретов заменяются на `***`. Без `--json` он идёт в
stderr cream, при запуске из юнита — в журнал systemd, на OpenWrt при загрузке — в `logread`.

## Юниты systemd

```
[Unit]
Description=cream: configure %i
ConditionPathExists=/var/lib/cream/installed/%i.json

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/bin/cream configure --from-unit %i
```

Расширение с хуком `configure` получает экземпляр `cream-configure@<ext>.service`.
cream включает его ссылкой в `*.wants` цели стадии и пишет порядок в drop-in
`10-stage.conf`; это работает и без запущенного systemd (в образе).

| Стадия | Ссылка в | Порядок |
|---|---|---|
| `early` | `sysinit.target.wants` | `DefaultDependencies=no`, `After=local-fs.target`, `Before=network-pre.target sysinit.target shutdown.target`, `Conflicts=shutdown.target` |
| `system` | `multi-user.target.wants` | `After=network.target` |
| `cloud-user` | `cream-configure-cloud-user.service.wants` | `After=cloud-final.service` |
| `graphical` | `graphical.target.wants` | `After=multi-user.target display-manager.service` |

`after`, `before`, `wants` из манифеста дописываются к порядку стадии. На живой
машине после `install`/`upgrade` cream запускает `configure` сразу; если systemd
ещё загружается — ставит перезапуск в очередь (`configure.no_block`).

## Init-скрипты procd (OpenWrt)

`cream system init` на OpenWrt пишет два скрипта `#!/bin/sh /etc/rc.common` и
ссылки на них в `/etc/rc.d` (как `enable`). Скрипт стадии при загрузке проходит
`/opt/cream/var/stages/<стадия>/*` по именам и для каждого расширения с записью
реестра вызывает `cream configure --from-unit <ext>`. Скрипты синхронные (не
`USE_PROCD`): procd выполняет `rc.d` по одному и ждёт каждый, поэтому `early`
заканчивается до сети. `after`, `before`, `wants` и стадия `graphical` у
семейства `openwrt` запрещены манифестом. `cream configure` и `cream set` на
живом OpenWrt запускают хук configure сразу (`apply: hook` в `cream list`).

| Стадия | Скрипт | Когда |
|---|---|---|
| `early` | `S19cream-configure-early` | до `S20network` |
| `system` | `S95cream-configure` | после сети и `wrt.cloudinit` (`S47`) |
| `cloud-user` | `S95cream-configure`, после `system` | там же |

## Блокировки

| Операция | Блокировка |
|---|---|
| `install`, `upgrade`, `remove` | `lock/global` и `lock/<ext>` эксклюзивно |
| `set`, `apply`, `configure`, действие с `lock: exclusive` | `lock/<ext>` эксклюзивно |
| действие с `lock: shared` | `lock/<ext>` разделяемо |
| действие с `lock: none` | ничего; может только читать настройки |
| `source add\|remove\|sync` своих источников | `<кеш>/.lock` |

Ожидание — `--lock-timeout` (2 мин), затем код 75. Вложенный cream (например,
`cream set --allow-readonly` из действия) получает `CREAM_LOCK_FD` и не ждёт
блокировку родителя; запросить `exclusive` при унаследованной `shared` нельзя
(75 `lock.upgrade`). Юнит systemd — не потомок владельца блокировки и её не
наследует: поэтому действие, вызываемое из `Exec*` юнита, объявляется
`lock: none`.
