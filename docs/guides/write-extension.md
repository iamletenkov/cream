# Написать расширение

Соберём каталог с одним расширением `hello`: оно пишет приветствие в файл,
даёт поменять его через `cream set` и показывает действием `status`. Затем
проверим, упакуем и поставим его — без root, в отдельный корень.

## 1. Раскладка

```
my-catalog/
  cream-catalog.yaml
  extensions/
    hello/
      extension.yaml
      hooks/install
      hooks/apply
      hooks/remove
      actions/status
```

Имя каталога расширения равно его `name`. Файлы в `hooks/` и `actions/` —
исполняемые (`chmod +x`): скрипты с shebang или ELF.

`my-catalog/cream-catalog.yaml`:

```yaml
schema: cream/v1
name: my-catalog
version: 0.1.0
roots: [extensions]
```

## 2. Манифест

`my-catalog/extensions/hello/extension.yaml`:

```yaml
schema: cream/v1
name: hello
version: 1.0.0
summary: Пример — пишет приветствие в файл состояния
cream: ">=1.0.0"
platform:
  family: deb
  arch: [amd64, arm64]
hooks:
  install: {}
  apply: {}
  remove: {}
config:
  keys:
    GREETING: {type: str, default: "привет", description: "текст приветствия"}
actions:
  status: {summary: "текущее приветствие", lock: none, output: json}
```

Все поля и их значения — [справочник манифеста](../reference/manifest.md).

## 3. Хуки и действие

Хук получает чистое окружение: `PATH`, `LANG=C.UTF-8`, переменные `CREAM_*` и все
несекретные ключи настроек под своими именами
([полный список](../reference/files-and-environment.md#окружение-хуков-и-действий)).

`hooks/install`:

```sh
#!/bin/sh
set -eu
mkdir -p "$CREAM_STATE_DIR"
printf '%s\n' "$GREETING" > "$CREAM_STATE_DIR/greeting"
```

`hooks/apply` — вызывается после `cream set`, список изменённых ключей — в
`$CREAM_CHANGED_KEYS`:

```sh
#!/bin/sh
set -eu
printf '%s\n' "$GREETING" > "$CREAM_STATE_DIR/greeting"
```

`hooks/remove`:

```sh
#!/bin/sh
set -eu
rm -f "$CREAM_STATE_DIR/greeting"
```

`actions/status` — с `output: json` печатает в stdout ровно один JSON-объект,
журнал — в stderr:

```sh
#!/bin/sh
set -eu
printf '{"greeting":"%s"}\n' "$(cat "$CREAM_STATE_DIR/greeting")"
```

```bash
chmod +x my-catalog/extensions/hello/hooks/* my-catalog/extensions/hello/actions/*
```

Правила для хуков:

- **Идемпотентность.** cream повторяет `install` после сбоя и при смене ключей
  установки; повтор с теми же входами не должен ничего менять.
- **Коды возврата.** 0 — успех; 10 — успех, нужна перезагрузка; 20 — неприменимо
  в этой среде; остальное — сбой.
- **Не ждать systemd.** В `apply` и `configure` — `systemctl --no-block`;
  синхронный `restart` юнита, который сам зовёт `cream run`, даст взаимную
  блокировку — `cream validate` об этом предупредит.
- Секреты в окружение не попадают: читайте их из `$CREAM_SECRETS_FILE` или
  `cream config get --secret "$CREAM_EXT" KEY`.

## 4. Проверить и упаковать

```bash
cream validate --catalog my-catalog
```

```
my-catalog: каталог my-catalog 0.1.0, расширений 1: hello 1.0.0 — без ошибок
```

```bash
SOURCE_DATE_EPOCH=0 cream catalog pack my-catalog --out my-catalog-0.1.0.tar.gz
```

Архив воспроизводим: при той же версии cream и том же `SOURCE_DATE_EPOCH` байты
совпадают.

## 5. Поставить без root

Глобальный флаг `--root DIR` переносит все пути cream под `DIR` — так расширение
проверяется без root и без риска для машины. Платформу cream определяет по
`DIR/etc/os-release`, его нужно положить в корень:

```bash
R=$PWD/root; mkdir -p "$R/etc" && cp /etc/os-release "$R/etc/"
SUM=$(sha256sum my-catalog-0.1.0.tar.gz | cut -d' ' -f1)
cream --root "$R" source add my --archive "file://$PWD/my-catalog-0.1.0.tar.gz" --sha256 "$SUM" --sync
cream --root "$R" install my/hello GREETING=здравствуй
cream --root "$R" run hello status          # {"greeting":"здравствуй"}
cream --root "$R" set hello GREETING=hi     # hello: изменено GREETING; применение: hook
cream --root "$R" run --json hello status   # конверт: result.output.greeting = "hi"
cream --root "$R" remove --purge hello
```

Для частых правок удобнее источник-каталог: `--path "$PWD/my-catalog"` и
`cream --root "$R" source sync my` после каждого изменения.

## Что ещё умеет манифест

| Задача | Поле |
|---|---|
| запускать настройку на загрузке | `hooks.configure` со `stage: early \| system \| cloud-user \| graphical` — [как выбрать стадию](configure-stages.md); на каждой загрузке, а не только при изменениях, — `every_boot: true` (с 1.2.0) |
| разложить юниты и конфиги, которыми владеет cream | `files: [{src, dst, mode}]` |
| потребовать другое расширение или способность | `depends`, `requires`; дать свою — `provides` |
| запретить соседство | `conflicts.extensions`, `conflicts.capabilities` |
| только при сборке образа или с подтверждением | `targets.build`, `targets.live`, `targets.live_note` |
| ограничить ОС и потребовать команды | `platform.os`, `platform.commands` |
| общий код для нескольких расширений | `kind: library` + `depends` у потребителей |
| ключ, который меняется только переустановкой | `config.keys.<KEY>.scope: install` |
| значение только на время установки (токен зеркала при сборке), нигде не хранится | `config.keys.<KEY>.scope: build` (с 1.2.0) — [ключи `scope: build`](../reference/manifest.md#ключи-scope-build) |
| секрет | `config.keys.<KEY>.secret: file`, в действии — `params.<KEY>.secret: true` и `stdin: secret` |

Подробно — [справочник манифеста](../reference/manifest.md); живые примеры —
каталог [cream-extensions](https://github.com/iamletenkov/cream-extensions).
