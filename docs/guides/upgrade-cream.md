# Обновить cream

cream обновляется пакетом `.deb` поверх старого. Установленные расширения, их
настройки (`/etc/cream`) и реестр (`/var/lib/cream`) пакет не трогает; `postinst`
выполняет `cream system init` (каталоги, юниты, пересчёт целей configure) и
`daemon-reload`.

Минорные версии совместимы: команды, флаги, поля `--json` и коды ошибок только
добавляются. Понизить версию можно, но расширения с `cream: ">=<новая>"`
перестанут ставиться (код 68 `platform.cream_version`).

## Перед обновлением

```bash
cream version
cream list                      # запомнить версии и статусы
pgrep -a cream || true          # нет идущих операций cream (иначе дождаться)
```

## Обновление

```bash
V=1.1.4; ARCH=$(dpkg --print-architecture)
cd "$(mktemp -d)"
curl -fLO "https://github.com/iamletenkov/cream/releases/download/v$V/cream_${V}_${ARCH}.deb"
curl -fLO "https://github.com/iamletenkov/cream/releases/download/v$V/SHA256SUMS"
sha256sum -c --ignore-missing SHA256SUMS     # cream_…deb: OK
sudo apt install "./cream_${V}_${ARCH}.deb"
```

Без сети на машине — тот же `.deb` и `SHA256SUMS`, доставленные иначе (scp,
локальное зеркало); сверка та же. Если cream стоит из `tar.gz` — заменить
`/usr/bin/cream` новым бинарём и выполнить `sudo cream system init`.

## Проверка

```bash
cream version                                # новая версия
cream list                                   # те же расширения и статусы
systemctl cat cream-configure@.service | head -5
systemctl --failed | grep -i cream || echo "нет упавших юнитов cream"
```

Если `postinst` напечатал предупреждение `system.registry_busy` (в этот момент шла
другая операция cream), повторите после её окончания: `sudo cream system init`.

## Откат

Поставить прежний `.deb` той же процедурой:

```bash
sudo apt install --allow-downgrades "./cream_<прежняя>_${ARCH}.deb"
```

Реестр новее исполнителя даёт код 74 `state.newer`; в пределах формата
`cream-state/v1` (все версии 1.x) такого не бывает.
