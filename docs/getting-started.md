# Первые шаги

За десять минут: поставить cream, подключить каталог расширений, поставить
расширение, поменять его настройку, вызвать действие и всё убрать. Команды
проверены на чистом Debian 12; нужны root (`sudo`) и доступ к GitHub.

## 1. Поставить cream

```bash
V=1.1.4; ARCH=$(dpkg --print-architecture)
curl -fLO "https://github.com/iamletenkov/cream/releases/download/v$V/cream_${V}_${ARCH}.deb"
curl -fLO "https://github.com/iamletenkov/cream/releases/download/v$V/SHA256SUMS"
sha256sum -c --ignore-missing SHA256SUMS        # cream_…deb: OK
sudo apt install "./cream_${V}_${ARCH}.deb"
cream version                                    # cream 1.1.4
```

## 2. Подключить каталог

Каталог — архив с расширениями. cream скачивает его один раз, сверяет sha256 до
распаковки, проверяет все манифесты и кладёт в кеш. Дальше всё работает из кеша,
без сети.

```bash
CV=5.2.0
URL="https://github.com/iamletenkov/cream-extensions/releases/download/v$CV"
SUM=$(curl -fsSL "$URL/SHA256SUMS" | awk -v f="cream-extensions-$CV.tar.gz" '$2 == f {print $1}')
sudo cream source add core --archive "$URL/cream-extensions-$CV.tar.gz" --sha256 "$SUM" --sync
cream source list
```

`core` — имя источника, им расширения адресуются: `core/locale`. Подробнее —
[подключить каталог](guides/add-catalog.md).

## 3. Поставить расширение с параметром

```bash
sudo cream install core/locale LOCALE=ru_RU.UTF-8
cream list
```

```
locale  2.0.0  installed  apply: hook
```

Параметры `KEY=VALUE` проверяются по типам из манифеста до запуска установки.
Расширение, которому нужны другие, без них не ставится: cream назовёт
недостающие (код 67), их можно перечислить в одной команде —
`sudo cream install core/apt-guard core/btop`.

## 4. Посмотреть и поменять настройки

```bash
cream show locale
```

```
KEYBOARD_LAYOUTS=us,ru  (default, re:^[a-z]{2,8}…)
KEYBOARD_TOGGLE=        (default, re:^grp:[a-z0-9_]+$)
LOCALE=ru_RU.UTF-8      (file, re:^[a-z]{2,3}_[A-Z]{2}\.UTF-8(@[a-z]+)?$)
```

```bash
sudo cream set locale KEYBOARD_TOGGLE=grp:alt_shift_toggle
cat /etc/cream/locale/config
```

`set` записывает значения в `/etc/cream/<ext>/config` и сразу применяет их
хуком расширения. Неверное значение отвергается до записи с кодом 65.

## 5. Вызвать действие

Действия — команды, которые расширение объявило в манифесте:

```bash
cream actions <ext>              # список: имя, описание, блокировка, формат вывода
sudo cream run <ext> <действие>  # например: sudo cream run firewall status
sudo cream run --json <ext> <действие>   # результат в конверте JSON
```

Флаги cream ставятся **до** имени расширения: всё после имени действия уходит
действию как есть.

## 6. Обновить и удалить

```bash
sudo cream source sync           # перечитать источники (единственный сетевой шаг)
sudo cream upgrade locale        # до версии из кеша
sudo cream remove locale         # хук remove; настройки в /etc/cream/locale остаются
```

`cream remove --purge locale` удаляет расширение вместе с настройками и
состоянием.

## Что дальше

- [Поставить и настроить расширение](guides/install-extension.md) — секреты,
  повтор после сбоя, рискованные расширения, зависимости.
- [Написать расширение](guides/write-extension.md) — свой каталог за 15 минут.
- [Диагностика](guides/diagnostics.md) — что делать, если что-то упало.
- [Справочник CLI](reference/cli.md).
