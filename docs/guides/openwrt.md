# cream на OpenWrt

cream 1.3 ставит расширения семейства `openwrt` на машины с **OpenWrt 25.12 и новее** (`apk`), x86-64 и arm64:
виртуальные машины, Raspberry Pi 4/5, arm64-серверы. Роутеры (MIPS, armv7, мало флеша) и OpenWrt 24.10 (`opkg`) не
поддерживаются. Решение и его причины — [ADR 0005](../adr/0005-openwrt-family-procd.md).

## Что отличается от Debian

| | Debian/Ubuntu | OpenWrt |
|---|---|---|
| поставка cream | `.deb` | `cream_<v>_linux_<arch>.tar.gz` из релиза |
| реестр, состояние, отметки, кеш | `/var/lib/cream`, `/var/cache/cream` | `/opt/cream/var` (`/var` на OpenWrt — tmpfs) |
| configure на загрузке | юниты `cream-configure@<ext>.service` | init-скрипты `S19cream-configure-early`, `S95cream-configure` |
| `cream configure`, `cream set` | перезапуск юнита | хук сразу, напрямую |
| журнал configure | `journalctl -u cream-configure@<ext>` | `logread -e cream-configure` |
| `TMPDIR` хуков | системный `/tmp` | `/opt/cream/var/tmp` (на диске) |
| запрет служб в фазе build | `policy-rc.d` | `IPKG_INSTROOT=/` в окружении хуков |

Семейство cream определяет сам по `/etc/os-release` (`ID=openwrt` или `lede`/`openwrt` в `ID_LIKE`), фазу — по PID 1:
`procd` — живая машина, иначе — сборка образа.

## Установить cream

Нужны root и около 15 МБ на корне. В образах `openwrt-builder` есть всё нужное (`tar` busybox умеет `-z`).

```sh
v=1.3.0 arch=amd64        # arm64 — для Raspberry Pi и armsr
cd /opt && wget -q "https://github.com/iamletenkov/cream/releases/download/v$v/cream_${v}_linux_${arch}.tar.gz"
wget -qO- "https://github.com/iamletenkov/cream/releases/download/v$v/SHA256SUMS" | grep "linux_${arch}.tar.gz" | sha256sum -c
mkdir -p /opt/cream-dist && tar -xzf "cream_${v}_linux_${arch}.tar.gz" -C /opt/cream-dist
cp /opt/cream-dist/cream /usr/bin/cream && chmod 0755 /usr/bin/cream
cream system init
```

`cream system init` создаёт `/opt/cream/var`, пишет `/etc/init.d/cream-configure-early` и
`/etc/init.d/cream-configure` и включает их (`/etc/rc.d/S19…`, `/etc/rc.d/S95…`). Повтор безопасен. Обновление cream —
те же шаги с новой версией.

## Поставить расширение

Дальше — как на Debian ([подключить каталог](add-catalog.md), [поставить расширение](install-extension.md)):

```sh
cream source add stvor --archive https://…/cream-extensions-openwrt-<v>.tar.gz --sha256 <hex> --sync
cream install <ext> KEY=VALUE
cream list
```

Расширение должно объявлять `platform.family: openwrt` и `cream: ">=1.3.0"`; расширение `deb` на OpenWrt — отказ 68
`platform.family`. Хук `configure` после `install` идёт сразу; на загрузке — из init-скрипта своей стадии и только если
отметка выполнения не совпала (или `every_boot: true`).

## Написать расширение для OpenWrt

- Хуки — любые исполняемые файлы; в образе нет `bash`, если его не поставить: пишите на `#!/bin/sh` (busybox ash)
  или требуйте `bash` в `platform.commands`.
- Состояние — в `$CREAM_STATE_DIR` (`/opt/cream/var/ext/<ext>`) или под `/opt`. Всё под `/var`, кроме `/var/run`,
  пропадает при перезагрузке — валидатор предупреждает (`validate.openwrt_tmpfs`).
- Большие временные файлы — в `$TMPDIR` (на диске), не в `/tmp` (ОЗУ).
- Стадии: `early` (до сети), `system`, `cloud-user`; `graphical` и `after`/`before`/`wants` — нет.
- Перезапуск службы из `apply` — фоном: `/etc/init.d/<svc> restart &` (иначе `validate.apply_blocking`).
- Службы — init-скрипты procd в `files` (`/etc/init.d/<svc>`), включение — `/etc/init.d/<svc> enable` в `install`.
- `apk add` в хуке `install` фазы build получает `IPKG_INSTROOT=/` (cream передаёт apk `APK_CONFIG` с
  `preserve-env`): пакет включается, но не запускается, `uci-defaults` ждут первой загрузки.
- `$TMPDIR` очищается при загрузке скриптом `S19`, как `/tmp`.
- В образах OpenWrt нет `/etc/machine-id`: отметка configure не отличает клон машины от оригинала — у клона
  configure сам не повторится, запустите `cream configure --force --all`.

## Образ с cream

В `virt-customize` (bisquite, `OpenWrtBuilder`) cream сам видит фазу build: `cream install` ставит дерево и включает
configure на стадии, а хук configure отрабатывает на первой загрузке машины.

## `sysupgrade`

Прошивка переписывает корень: cream, его реестр и расширения после `sysupgrade` не сохраняются — cream ничего не
кладёт в `/lib/upgrade/keep.d`. Расширение, которому нужно пережить обновление прошивки (например, ключи узла),
перечисляет **только своё состояние** в своём файле `/lib/upgrade/keep.d/<ext>`; после обновления cream и расширения
ставятся заново.

## Диагностика

```sh
logread -e cream-configure         # вывод configure на загрузке
cream list                         # статус, apply: hook, отметка configure
ls /opt/cream/var/stages/*         # что включено на каких стадиях
cream configure --force <ext>      # прогнать configure сейчас
```
