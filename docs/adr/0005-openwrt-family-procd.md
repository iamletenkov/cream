# 0005. Семейство `openwrt`: раскладка `/opt/cream/var`, configure из init-скриптов procd, поставка `tar.gz`

- **Status:** Accepted
- **Date:** 2026-10-21
- **Deciders:** владелец cream
- **Tags:** format, paths, init, packaging

## Контекст и проблема
Владелец решил ставить расширения cream на машины с OpenWrt (план
[bisquite-bundled-cream](../plans/2026-10-21-bisquite-bundled-cream.md), Task 27; отложенная спека OpenWrt, PR #16, —
часть «cream на OpenWrt» без Teleport и Stvor). Вводные владельца: только **OpenWrt 25.12+** (`apk`), x86-64 и arm64
(ВМ, Raspberry Pi 4/5, мощный arm64; роутеров нет); cream ставится из **`tar.gz`**; состояние — в `/opt` (на OpenWrt
`/var` — ссылка на `/tmp`, это tmpfs); этапы configure — через **procd** (`rc.d`), systemd нет; временные каталоги —
на диске, не в `/tmp` (это ОЗУ); базовый образ — `iamletenkov/openwrt-builder` v0.3.0.

cream 1.2 привязан к Debian и systemd: `platform.family: deb` — единственное значение, реестр и состояние —
`/var/lib/cream`, configure — юнит `cream-configure@.service`, фаза по PID 1 — `systemd` или «сборка», `policy-rc.d`
в фазе build. На OpenWrt реестр пропал бы на первой перезагрузке, а живая машина (PID 1 — `procd`) считалась бы
сборкой.

## Рассмотренные варианты
- **A — минор `cream/v1` (1.3.0): семейство `openwrt` как новое значение перечисления, выбор раскладки и init по
  семейству хоста.** Аддитивно, как стадии в 1.1 и `every_boot` в 1.2; манифест с `family: openwrt` требует
  `cream: ">=1.3.0"` (`checkSince`), старый cream отказывает по `cream:` (68).
- **B — флаг или конфиг «я на OpenWrt».** Лишний ручной шаг и способ ошибиться; семейство и так видно из
  `/etc/os-release`.
- **C — USE_PROCD-службы для configure.** procd ставит экземпляр в очередь и не ждёт: стадия `early` не успела бы до
  `S20network`, а порядок стадий распался бы.
- **D — сохранять реестр cream через `/lib/upgrade/keep.d`.** После `sysupgrade` бинарей и деревьев нет — реестр
  указывал бы на пустоту (рулинг Р4 спеки OpenWrt).

## Решение
Вариант A. cream 1.3.0:
1. **`platform.family: openwrt`** (с `cream: ">=1.3.0"`); семейство хоста — по `ID`/`ID_LIKE` из os-release
   (`openwrt`, `lede`). У `openwrt` нет стадии `graphical` и полей `after`/`before`/`wants` (имена юнитов systemd) —
   65 `manifest.invalid`; порядок задаёт только стадия.
2. **Раскладка по семейству** (`paths.Layout.Family`, читается из os-release под `--root`): на OpenWrt реестр,
   состояние расширений, отметки configure, журнал, блокировки, кеш и `sources.lock` — под **`/opt/cream/var`**;
   метки одного запуска (`reason`, `result`, `reboot-required`) — в `/var/run` (tmpfs, так и надо). `/opt/cream/ext`,
   `/etc/cream`, `/etc/image-labels.d/cream.json` — как на Debian. Пути Debian не меняются.
3. **Init по семейству** (`internal/initsys`: `systemd` или `procd`). На OpenWrt `cream system init` пишет два
   rc.common-скрипта и ссылки `rc.d`: `/etc/init.d/cream-configure-early` (`S19`, стадия `early`, до `S20network`) и
   `/etc/init.d/cream-configure` (`S95`, стадии `system` и `cloud-user`, после `wrt.cloudinit` `S47`). Включённый
   configure — пустой файл `/opt/cream/var/stages/<стадия>/<ext>` (аналог `.wants`); скрипт стадии зовёт
   `cream configure --from-unit <ext>` по именам по порядку. Скрипты — **синхронные rc.common, не USE_PROCD**:
   procd выполняет `rc.d` при загрузке по одному и ждёт каждый без таймаута (`rcS`, `max_running_tasks = 1`).
   Вывод — в `logread` (procd пишет его с именем скрипта).
4. **Фаза:** на OpenWrt `live` — PID 1 `procd`; иначе (`init` appliance libguestfs, chroot с `/proc` хоста) — `build`.
5. **Хуки на OpenWrt** получают `TMPDIR=/opt/cream/var/tmp` (на диске, sticky 1777); в фазе build — ещё
   **`IPKG_INSTROOT=/`**: штатный режим офлайн-сборки OpenWrt — скрипты пакетов `apk add` делают `enable` без
   `start`, а `uci-defaults` остаются первой загрузке. Это аналог `policy-rc.d`, который на OpenWrt не ставится.
   apk-tools 3 запускает скрипты пакетов с чистым окружением, поэтому в фазе build хук получает и
   **`APK_CONFIG=/opt/cream/var/apk-build.conf`** — копию `/etc/apk/config` образа (если есть) плюс `preserve-env`:
   так `IPKG_INSTROOT` доходит до скриптов пакетов при обычном `apk add`. Ключ домена с именем `TMPDIR`,
   `IPKG_INSTROOT` или `APK_CONFIG` их не перекрывает. `TMPDIR` на диске сам не чистится — его очищает скрипт `S19`
   при загрузке; без прав на каталог (`cream run` не от root) `TMPDIR` не ставится. С `preserve-env` скрипты
   пакетов видят всё окружение хука, в том числе значения `scope: build` (и секретные) — они и так root.
   `/etc/apk/config` образа читается внутри корня (`os.OpenInRoot`): ссылка наружу не уносит файл хоста.
6. **Без юнита:** `cream configure` и `cream set` (у расширения без `apply`) на живом OpenWrt запускают хук configure
   сразу под `lock/<ext>`, `cream list` показывает `apply: hook`; `--again` — предупреждение, как без systemd.
7. **Поставка — `tar.gz`** релиза (`cream_<v>_linux_{amd64,arm64}.tar.gz`, уже выпускается): распаковать `cream` в
   `/usr/bin` и выполнить `cream system init`. Пакетов `.apk`/`.ipk` нет (спека OpenWrt В5).
8. **`sysupgrade`:** cream ничего не кладёт в `/lib/upgrade/keep.d` — реестр, деревья и бинарь после обновления
   прошивки не сохраняются (Р4); что сохранить (только состояние вроде ключей узла) — дело самого расширения через
   его `files`.
9. **Валидатор каталога** у семейства `openwrt` предупреждает: `validate.apply_blocking` — `/etc/init.d/<svc>
   restart` (или `service <svc> restart`) в хуке `apply` без `&`; `validate.openwrt_tmpfs` — `files` с `dst` под
   `/var` (кроме `/var/run`) и `/var/lib/` в хуках и действиях.

## Обоснование
Семейство уже определялось по os-release для отказа 68 — достаточно разрешить второе значение и дать ему свою
раскладку и init. Всё остальное (резолвер, источники, реестр, отметка configure, фаза build из источника) работает
без изменений: проверено контейнером из корневой ФС OpenWrt 25.12 (`make test-openwrt`) и ВМ на PVE (`tar.gz` →
`system init` → `source add` → `install` → перезагрузка → configure на `S19`/`S95` → состояние на месте; образ,
собранный `virt-customize` в фазе build, на первой загрузке отработал configure; пакет-зонд `apk add` из хука фазы
build видит `IPKG_INSTROOT=/`).

## Последствия
- **Положительные:** расширения семейства `openwrt` ставятся тем же `cream install`, и в образ (`virt-customize`), и
  на живую машину; состояние переживает перезагрузку; сборка образа не запускает служб.
- **Отрицательные:** у configure на OpenWrt нет параллельности и порядка внутри стадии кроме имён; нет юнита — нет
  `--wait`-исхода от юнита и `--again`; procd-песочницы для хуков нет (хуки — root без ограничений, как и раньше);
  в образах OpenWrt нет `/etc/machine-id`, поэтому отметка configure не отличает клон машины от оригинала.
- **Действия:** каталог — корень `extensions/openwrt` и скрытый шаг cream в `OpenWrtBuilder` bisquite (Task 28);
  `make test-openwrt` в CI; справочники путей, манифеста и кодов дополнены.

## Связанные ADR / документы
- [ADR 0001](0001-contract-cream-v1.md) — контракт `cream/v1`, в минорах только добавляем
- [ADR 0004](0004-bundled-cream-contract-1-2.md) — `checkSince`, автоопределение фазы, отметка configure
- [Спека bisquite-bundled-cream §16](../specs/2026-10-21-bisquite-bundled-cream.md) и отложенная спека OpenWrt
  (`docs/specs/2026-10-20-openwrt.md`, ветка `docs/openwrt-spec`, PR #16) — §4.1, §4.3, §4.7, Р2, Р4
- [Руководство «cream на OpenWrt»](../guides/openwrt.md)
