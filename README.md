# cream

Исполнитель расширений для машин на Debian и Ubuntu и для сборки их образов.

Расширение — каталог с манифестом `extension.yaml` и скриптами: как поставить
программу, как её настроить, обновить и убрать. cream берёт расширения из
проверенных каталогов, хранит их настройки и состояние, запускает настройку на
загрузке, когда она нужна, и даёт расширениям действия с машинно-читаемым выводом. Один и
тот же манифест работает и на живой машине, и внутри `virt-customize` при сборке
образа.

## Установка

Пакеты — в [выпусках GitHub](https://github.com/iamletenkov/cream/releases): `.deb`
и `tar.gz` для `amd64` и `arm64`, рядом — `SHA256SUMS`.

**Пакет `.deb`:**

```bash
V=1.2.0; ARCH=$(dpkg --print-architecture)    # amd64 | arm64
curl -fLO "https://github.com/iamletenkov/cream/releases/download/v$V/cream_${V}_${ARCH}.deb"
curl -fLO "https://github.com/iamletenkov/cream/releases/download/v$V/SHA256SUMS"
sha256sum -c --ignore-missing SHA256SUMS
sudo apt install "./cream_${V}_${ARCH}.deb"
cream version
```

**Архив `tar.gz`** — тот же бинарь без пакетного менеджера:

```bash
V=1.2.0; ARCH=$(dpkg --print-architecture)
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
  или локальный каталог. Сеть нужна только синхронизации источника; установка идёт
  из кеша.
- **Полный жизненный цикл.** `install`, `upgrade`, `remove` проверяют платформу,
  зависимости и конфликты до запуска хуков; повтор после сбоя — та же команда.
- **Настройка на загрузке.** Хук `configure` запускается юнитом
  `cream-configure@<ext>` в выбранной стадии: до сети, после сети, после cloud-init
  или поверх графического входа. С 1.2.0 — один раз до успеха и снова при
  изменениях (отметка выполнения), `every_boot: true` — на каждой загрузке;
  код 20 — «пропущено, повторить на следующей».
- **Типизированные настройки.** `cream set` проверяет значения по типам манифеста
  до записи; файлы — в формате `EnvironmentFile` systemd, секреты — отдельным
  файлом 0600 и только со stdin.
- **Действия расширений.** `cream run <ext> <действие>` — с блокировками,
  таймаутами и выводом одним JSON-объектом, для людей, скриптов и агентов.
- **Сборка образов.** `cream install` внутри `virt-customize` сам узнаёт фазу
  build и ставит то же расширение из каталога в образе, без сети; ключи
  `scope: build` живут только в окружении хука. `cream check` проверяет строки
  cream образа до сборки, `/etc/image-labels.d/cream.json` отдаёт состав в метки
  образа. Так работает [bisquite](https://github.com/iamletenkov/bisquite).
- **Машинно-читаемый вывод.** У каждой команды есть `--json` со стабильными
  кодами ошибок.

## Документация

- [Первые шаги](docs/getting-started.md)
- Руководства: [подключить каталог](docs/guides/add-catalog.md) ·
  [поставить и настроить расширение](docs/guides/install-extension.md) ·
  [написать расширение](docs/guides/write-extension.md) ·
  [стадии configure](docs/guides/configure-stages.md) ·
  [диагностика](docs/guides/diagnostics.md) ·
  [обновить cream](docs/guides/upgrade-cream.md) ·
  [фаза build для сборщика образов](docs/guides/build-phase.md) ·
  [cream в образах bisquite](docs/guides/bisquite-images.md)
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
| Зависимости | нет, бинарь статический; systemd — для настройки на загрузке; в фазе build cream работает внутри гостя (`virt-customize`), на хосте сборки ему ничего не нужно |
| Формат расширений | `cream/v1`; в пределах мажорной версии команды, флаги, поля `--json` и коды ошибок только добавляются |

## Лицензия

[Apache-2.0](LICENSE), © 2026 Letenkov Maksim. Сторонний код — [NOTICE](NOTICE).
