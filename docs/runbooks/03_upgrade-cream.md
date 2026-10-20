# 03. Обновление cream на роботе

cream обновляется пакетом `.deb` поверх старого. Установленные расширения, их настройки (`/etc/cream`) и реестр
(`/var/lib/cream`) пакет не трогает; `postinst` выполняет `cream system init` (раскладка, шаблон
`cream-configure@.service`, пересчёт целей configure) и `daemon-reload`.

## Перед обновлением
```bash
cream version
cream list                                  # запомнить версии и статусы
pgrep -a cream || true                      # нет идущих операций cream (иначе дождаться)
```
Новая минорная версия совместима: команды, флаги, поля `--json` и коды ошибок только добавляются (ADR-0001).
Даунгрейд через пакет возможен, но расширения с `cream: ">=<новая>"` перестанут ставиться (68
`platform.cream_version`).

## Обновление
```bash
V=1.1.3; ARCH=$(dpkg --print-architecture)    # amd64 | arm64
cd "$(mktemp -d)"
curl -fLO "https://github.com/iamletenkov/cream/releases/download/v$V/cream_${V}_${ARCH}.deb"
curl -fLO "https://github.com/iamletenkov/cream/releases/download/v$V/SHA256SUMS"
sha256sum -c --ignore-missing SHA256SUMS     # cream_…deb: OK
sudo apt install "./cream_${V}_${ARCH}.deb"
```
Без сети на машине — тот же `.deb` и `SHA256SUMS`, доставленные иначе (scp, локальное зеркало); сверка та же.

## Проверка
```bash
cream version                                # новая версия
cream list                                   # те же расширения и статусы, что до обновления
systemctl cat cream-configure@.service | head -5
systemctl --failed | grep -i cream || echo "нет упавших юнитов cream"
```
Если `postinst` напечатал предупреждение `system.registry_busy` (в этот момент шла другая операция cream и
держала `lock/global`), повторить после её окончания: `sudo cream system init`.

## Откат
Поставить прежний `.deb` той же процедурой:
`sudo apt install --allow-downgrades ./cream_<прежняя>_<arch>.deb`. Реестр новее исполнителя даёт 74 `state.newer` — тогда откат невозможен без восстановления
`/var/lib/cream` из резервной копии; в пределах `cream-state/v1` такого не бывает.
