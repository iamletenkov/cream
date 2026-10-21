# cream

Исполнитель расширений для машин на Debian и Ubuntu и для сборки их образов.

Расширение — каталог с манифестом `extension.yaml` и скриптами: как поставить
программу, как её настроить, обновить и убрать. cream берёт расширения из
проверенных каталогов, хранит их настройки и состояние, запускает настройку на
каждой загрузке и даёт расширениям действия с машинно-читаемым выводом. Один и
тот же манифест работает и на живой машине, и внутри `virt-customize` при сборке
образа.

## Установка

Пакеты — в [выпусках GitHub](https://github.com/iamletenkov/cream/releases): `.deb`
и `tar.gz` для `amd64` и `arm64`, рядом — `SHA256SUMS`.

**Пакет `.deb`:**

```bash
V=1.1.4; ARCH=$(dpkg --print-architecture)    # amd64 | arm64
curl -fLO "https://github.com/iamletenkov/cream/releases/download/v$V/cream_${V}_${ARCH}.deb"
curl -fLO "https://github.com/iamletenkov/cream/releases/download/v$V/SHA256SUMS"
sha256sum -c --ignore-missing SHA256SUMS
sudo apt install "./cream_${V}_${ARCH}.deb"
cream version
```

**Архив `tar.gz`** — тот же бинарь без пакетного менеджера:

```bash
V=1.1.4; ARCH=$(dpkg --print-architecture)
curl -fLO "https://github.com/iamletenkov/cream/releases/download/v$V/cream_${V}_linux_${ARCH}.tar.gz"
curl -fLO "https://github.com/iamletenkov/cream/releases/download/v$V/SHA256SUMS"
sha256sum -c --ignore-missing SHA256SUMS
tar xzf "cream_${V}_linux_${ARCH}.tar.gz" cream
sudo install -m 0755 cream /usr/bin/cream
sudo cream system init      # каталоги и юниты systemd (пакет делает это сам)
```

Сборка из исходников — [docs/development.md](docs/development.md).

## Быстрый старт

Подключить общий каталог [cream-extensions](https://github.com/iamletenkov/cream-extensions)
и поставить из него расширение:

```bash
CV=5.2.0
URL="https://github.com/iamletenkov/cream-extensions/releases/download/v$CV"
SUM=$(curl -fsSL "$URL/SHA256SUMS" | awk -v f="cream-extensions-$CV.tar.gz" '$2 == f {print $1}')
sudo cream source add core --archive "$URL/cream-extensions-$CV.tar.gz" --sha256 "$SUM" --sync

sudo cream install core/apt-guard core/btop    # btop зависит от apt-guard
cream list
sudo cream remove btop
```

Дальше — [первые шаги](docs/getting-started.md): настройки, действия, обновление.

## Возможности

- **Проверяемые источники.** Каталог — архив по `https://` с обязательной sha256
  или локальный каталог. Сеть нужна только `cream source sync`; установка идёт из
  кеша.
- **Полный жизненный цикл.** `install`, `upgrade`, `remove` проверяют платформу,
  зависимости и конфликты до запуска хуков; повтор после сбоя — та же команда.
- **Настройка на каждой загрузке.** Хук `configure` запускается юнитом
  `cream-configure@<ext>` в выбранной стадии: до сети, после сети, после cloud-init
  или поверх графического входа.
- **Типизированные настройки.** `cream set` проверяет значения по типам манифеста
  до записи; файлы — в формате `EnvironmentFile` systemd, секреты — отдельным
  файлом 0600 и только со stdin.
- **Действия расширений.** `cream run <ext> <действие>` — с блокировками,
  таймаутами и выводом одним JSON-объектом, для людей, скриптов и агентов.
- **Сборка образов.** Фаза build ставит то же расширение внутри `virt-customize`
  без сети; так работает [bisquite](https://github.com/iamletenkov/bisquite).
- **Машинно-читаемый вывод.** У каждой команды есть `--json` со стабильными
  кодами ошибок.

## Документация

- [Первые шаги](docs/getting-started.md)
- Руководства: [подключить каталог](docs/guides/add-catalog.md) ·
  [поставить и настроить расширение](docs/guides/install-extension.md) ·
  [написать расширение](docs/guides/write-extension.md) ·
  [диагностика](docs/guides/diagnostics.md) ·
  [обновить cream](docs/guides/upgrade-cream.md) ·
  [фаза build для сборщика образов](docs/guides/build-phase.md)
- Справочник: [CLI](docs/reference/cli.md) ·
  [манифест](docs/reference/manifest.md) ·
  [коды выхода и предупреждения](docs/reference/exit-codes.md) ·
  [вывод `--json`](docs/reference/json-output.md) ·
  [файлы и переменные окружения](docs/reference/files-and-environment.md)
- [Устройство cream](docs/architecture.md) · [все документы](docs/README.md) ·
  [изменения по версиям](CHANGELOG.md)

На машине с пакетом: `man cream`, `man cream-extension.yaml`, `cream help <команда>`.

## Совместимость

| | |
|---|---|
| ОС | Debian и Ubuntu; в CI — Debian 12, Debian 13, Ubuntu 24.04 |
| Архитектуры | `amd64`, `arm64` |
| Зависимости | нет, бинарь статический; systemd — для настройки на загрузке; `libguestfs-tools` — только для фазы build |
| Формат расширений | `cream/v1`; в пределах мажорной версии команды, флаги, поля `--json` и коды ошибок только добавляются |

## Лицензия

[Apache-2.0](LICENSE), © 2026 Letenkov Maksim. Сторонний код — [NOTICE](NOTICE).
