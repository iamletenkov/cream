# Подключить каталог

Расширения cream берёт только из **источников** — подключённых каталогов.
Источник записывается в `/etc/cream/sources.yaml`, а его содержимое — в кеш
`/var/cache/cream/sources/<имя>/`. Сеть нужна одной команде — `cream source sync`;
установка, обновление и проверки работают по кешу.

## Архив по URL

Обычный путь: опубликованный архив каталога и его sha256.

```bash
CV=5.2.0
URL="https://github.com/iamletenkov/cream-extensions/releases/download/v$CV"
SUM=$(curl -fsSL "$URL/SHA256SUMS" | awk -v f="cream-extensions-$CV.tar.gz" '$2 == f {print $1}')
sudo cream source add core --archive "$URL/cream-extensions-$CV.tar.gz" --sha256 "$SUM" --sync
```

- `--sha256` обязателен: несовпадение суммы — отказ до распаковки (код 69).
- Принимаются только `https://` и `file:///`; голый путь вместо `file:///` — код 64.
- Без `--sync` источник только записывается; кеш появится после
  `sudo cream source sync core`. С `--sync` при сбое загрузки запись не сохраняется.
- Повтор `add` с теми же полями ничего не меняет (код 0).

## Без сети на машине

Тот же архив, доставленный любым способом (scp, флешка, локальное зеркало):

```bash
sha256sum cream-extensions-5.2.0.tar.gz      # сверить с опубликованной суммой
sudo cream source add core \
  --archive file:///srv/cream/cream-extensions-5.2.0.tar.gz \
  --sha256 <сумма> --sync
```

## Локальный каталог

Для разработки расширений — каталог на диске (абсолютный путь). cream **копирует**
его в кеш, поэтому после правок нужен `sync`:

```bash
sudo cream source add dev --path /home/me/my-catalog --sync
# … правки …
sudo cream source sync dev
```

## Поднять версию каталога

У источника с тем же именем и другими полями `add` без `--replace` отказывает
(код 65 `source.conflict`):

```bash
sudo cream source add core --archive "$NEW_URL" --sha256 "$NEW_SUM" --replace --sync
sudo cream upgrade --all
```

## Несколько источников

Порядок в `sources.yaml` — приоритет. Ссылка без имени источника (`hello`) ищется
по порядку; если расширение с тем же именем есть и дальше, cream предупредит
`source.shadowed`. Явная ссылка `dev/hello` берёт расширение из конкретного
источника, а `--source dev` ограничивает поиск одним источником.

```bash
cream source list                 # источники в порядке приоритета
cream source show core            # что синхронизировано: sha256, версия каталога, время
sudo cream source remove dev      # удалить источник вместе с кешем
```

## Без root

От обычного пользователя cream работает со своими файлами: источники —
`$XDG_CONFIG_HOME/cream/sources.yaml`, кеш — `$XDG_CACHE_HOME/cream/`. Это нужно
сборщику образов на хосте; ставить расширения на машину может только root.
Пути можно задать явно: `--sources FILE`, `--cache DIR` (или `CREAM_SOURCES`,
`CREAM_CACHE`).

## Опубликовать свой каталог

Каталог — дерево с `cream-catalog.yaml` в корне ([формат](../reference/manifest.md#cream-catalogyaml)).
Проверить и упаковать его в воспроизводимый архив:

```bash
cream validate --catalog ./my-catalog
SOURCE_DATE_EPOCH=$(git log -1 --format=%ct) cream catalog pack ./my-catalog --out my-catalog-1.0.0.tar.gz
sha256sum my-catalog-1.0.0.tar.gz > SHA256SUMS
```

Как написать само расширение — [написать расширение](write-extension.md).
