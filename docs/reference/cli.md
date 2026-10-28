# Справочник CLI

Команды cream 1.2 (контракт `cream/v1`). То же кратко — `cream help <команда>` и
`man cream`. Коды выхода — [отдельная страница](exit-codes.md), формы `--json` —
[вывод `--json`](json-output.md).

## Синтаксис

```
cream <команда> [аргументы…] [флаги]
```

- Флаги можно ставить и после аргументов (`cream set hello K=V --no-apply`);
  `--` заканчивает флаги. Исключение — `cream run`: флаги cream только до имени
  расширения.
- `cream help [<команда>]`, `-h`, `--help` — справка; `cream --version` — то же,
  что `cream version`.
- Ссылка на расширение — `[источник/]имя[@версия]`. Без источника расширение
  ищется по источникам в порядке приоритета; `@версия` — проверка, а не выбор:
  другая версия в кеше — код 66.
- Изменяющие команды (`install`, `upgrade`, `remove`, `set`, `configure`,
  `system init`, `source add|remove|sync` для источников машины) требуют root.

## Глобальные флаги

| Флаг | Окружение | Смысл |
|---|---|---|
| `--json` | — | stdout — ровно один JSON-объект; сообщения и вывод хуков — в stderr |
| `--root DIR` | `CREAM_ROOT` | перенести все пути cream под абсолютный `DIR` (тесты, фикстуры, работа без root) |
| `--sources FILE` | `CREAM_SOURCES` | свой файл источников вместо `/etc/cream/sources.yaml` |
| `--cache DIR` | `CREAM_CACHE` | свой кеш источников; `sources.lock` — в нём же |
| `--lock-timeout D` | — | сколько ждать блокировку, по умолчанию `2m0s` |
| `-q`, `-v` | — | меньше / больше сообщений в stderr |

## Источники

```
cream source add <name> --archive <https://…|file:///…> --sha256 <hex> [--replace] [--sync]
cream source add <name> --path DIR [--replace] [--sync]
cream source remove <name>
cream source list
cream source show <name>
cream source sync [<name>…]
```

| Флаг `add` | Смысл |
|---|---|
| `--archive URL` | архив каталога `tar.gz`: только `https://` или `file:///` |
| `--sha256 HEX` | sha256 архива, обязателен с `--archive` |
| `--path DIR` | каталог на диске, абсолютный путь; копируется в кеш |
| `--replace` | заменить источник с тем же именем и другими полями |
| `--sync` | сразу синхронизировать; при сбое запись не сохраняется |

`add` с теми же полями ничего не меняет. `sync` — единственная сетевая команда:
загрузка, сверка sha256 до распаковки, отказ на опасных архивах (абсолютные
пути, `..`, ссылки наружу, устройства), проверка каталога и всех манифестов,
атомарная подмена кеша. Битый каталог не заменяет прежний кеш.
`remove` удаляет источник вместе с кешем. Флаги `--git`, `--ref`, `--commit`
зарезервированы (код 64 `usage.reserved`).

## Проверка и упаковка каталога

```
cream validate [--catalog DIR] [DIR…]
cream catalog pack DIR --out FILE
```

- `validate` проверяет каталог с `cream-catalog.yaml` (`--catalog`) и/или
  отдельные каталоги расширений: схему манифестов, файлы хуков и действий, граф
  `depends` внутри каталога, подозрительные вызовы `systemctl` в скриптах
  (предупреждения `validate.*`). Без аргументов — код 64.
- `catalog pack` сначала проверяет каталог, затем пишет детерминированный
  `tar.gz`: сортировка, владелец 0:0, время файлов — `SOURCE_DATE_EPOCH`
  (без переменной — 0).

## Установка, обновление, удаление

```
cream install [--phase build|live] [--source S] [--with-deps] [--reinstall] [--allow-downgrade] [--accept-risk] [--allow-skip] [--params-file F] <ref>… [KEY=VALUE…] [KEY=-]
cream upgrade [--dry-run] [-y] [--accept-risk <ext>|all]… [--fail-fast] <ext>… | --all
cream hold <ext>…
cream unhold <ext>…
cream remove [--purge|--forget] [--force] <ext>
```

| Флаг `install` | Смысл |
|---|---|
| `--source S` | искать ссылки без источника только в `S` |
| `--with-deps` | добрать недостающие `depends` из источника того расширения, которому они нужны |
| `--reinstall` | вызвать хук `install`, даже если ничего не изменилось |
| `--allow-downgrade` | разрешить понижение версии и ту же версию с другим отпечатком; ставится путём upgrade (хук `upgrade` с `CREAM_PREV_VERSION`), в `history.jsonl` — `op: downgrade`; без флага — 65 `version.downgrade` / `version.same_digest` |
| `--accept-risk` | ставить расширение с `targets.live: confirm` |
| `--allow-skip` | код 20 хука — статус `skipped` без ошибки (иначе код 68) |
| `--params-file F` | параметры `KEY=VALUE` из файла (0600, владелец root) |
| `--phase build\|live` | фаза; без флага — `CREAM_PHASE` родителя, иначе по PID 1 |
| `--from-dir`, `--record`, `--remove-params-file` | устарело с 1.2, уйдёт в cream 2.0: один слой, доставленный bisquite 2.x, — [для сборщиков](../guides/build-phase.md) |

- Параметры `KEY=VALUE` проверяются по манифесту до запуска хука; секрет — только
  `KEY=-` (значение со stdin, один ключ) или `--params-file`.
- Несколько ссылок ставятся в порядке зависимостей; параметры тогда не
  принимаются. Без `--with-deps` зависимости сами не ставятся (код 67 со списком
  кандидатов); `--with-deps` добирает `depends` по имени из источника расширения, в той
  версии, что там лежит (не `requires`).
- В фазе build (внутри сборки образа) `install`, `upgrade` и `remove` работают так
  же — дерево из источника, — но хуки идут под `policy-rc.d` (службы не
  стартуют), configure только включается, systemd не трогается.
- Повтор безопасен: без изменений — ничего; новые значения обычных ключей —
  запись и применение; новое значение ключа `scope: install`, статус `failed` или
  `--reinstall` — снова хук `install`.
- `upgrade` берёт новую версию из кеша того источника, откуда расширение
  поставлено. Сначала строится план по каждому расширению — `upgrade`, `retry`,
  `up-to-date`, `held`, `skipped`, `blocked:<причина>` (`risk`, `platform`,
  `dependency`, `held`, `source`, `version`), — потом обновления идут в порядке
  зависимостей. Без новой версии — `up-to-date`: ни хука, ни проверок платформы и
  риска. Пропуск или сбой одного не останавливает остальные (кроме зависимых от
  него); код выхода — по худшему исходу. Подробно —
  [обновить расширения](../guides/upgrade-extensions.md).

| Флаг `upgrade` | Смысл |
|---|---|
| `--all` | все установленные, кроме закреплённых (`held`) и поставленных не из источника (`skipped`) |
| `--dry-run` | только план; код выхода — как у плана |
| `-y`, `--yes` | не спрашивать в терминале; риск не снимает |
| `--accept-risk <ext>` | обновлять `<ext>` с `targets.live: confirm`; повторяется или через запятую; `all` — все |
| `--fail-fast` | пропуск в плане — ничего не делать; первая ошибка — остальное `not_run` |

- В терминале (stdin и stderr — tty) без `-y`, `--dry-run`, `--json` cream
  спрашивает y/N про каждое `targets.live: confirm` и про план; «нет» плану —
  код 1 `upgrade.declined`.
- `hold` закрепляет версию (поле `held` записи реестра): `upgrade --all` пропускает,
  `upgrade <ext>` и `install` другой версии — код 68 `hold.held`; `unhold` снимает.
  Без изменений — `changed: false`; неустановленное — 66.

| Флаг `remove` | Смысл |
|---|---|
| `--purge` | удалить и `/etc/cream/<ext>`, `/var/lib/cream/ext/<ext>` |
| `--forget` | без хука `remove`: только реестр, дерево и файлы cream |
| `--force` | удалить, хотя от расширения зависят установленные |

Флаг `--require-pinned` зарезервирован (код 64 `usage.reserved`).

## Настройки

```
cream list
cream show <ext>
cream get [--secret] <ext> KEY
cream set [--apply|--no-apply] [--wait] [--allow-readonly] [--params-file F] <ext> KEY=VALUE… [KEY=-]
cream config get [--secret] <ext> KEY
cream config export --shell <ext>
```

- `list` — установленные расширения: версия, статус (`installed`, `failed`,
  `failed-upgrade`, `skipped`), способ применения настроек, нужна ли перезагрузка,
  закреплено ли (`cream hold`).
- `show` — ключи: значение, откуда (`default` или `file`), тип; секреты — `***`,
  `(задан)` или `(пусто)`; битое значение в файле помечено `invalid`.
- `show --json` и `list --json` у расширения с хуком `configure` дают
  `configured: {at, matches, every_boot}` — когда configure последний раз
  прошёл успешно (`null` — ещё не проходил или упал), совпадает ли отметка
  выполнения с реестром и настройками, идёт ли configure на каждой загрузке;
  без хука `configure` — `null`.
- `get` — одно значение; секрет — только с `--secret` и только root.
- `set` проверяет все ключи до записи и пишет одной транзакцией.

| Флаг `set` | Смысл |
|---|---|
| `--apply` | применить по правилу расширения (по умолчанию): хук `apply` или перезапуск `cream-configure@<ext>` |
| `--no-apply` | только записать |
| `--wait` | ждать перезапуска configure (во время загрузки systemd — без ожидания, с предупреждением) |
| `--allow-readonly` | записать `readonly`-ключ |
| `--params-file F` | значения из файла (0600, владелец root) — для нескольких секретов |

`config get` — то же, что `get`, для скриптов; `config export --shell` печатает
`KEY='…'` для POSIX shell, без секретов.

## Действия

```
cream actions <ext>
cream run [--json] [--lock-timeout D] [-q] <ext> <action> [args…]
```

- `actions` — действия установленного расширения: описание, аргументы,
  блокировка, формат вывода.
- `run` запускает действие. **Флаги cream — только до `<ext>`**: всё после имени
  действия уходит ему как есть, включая `--json`.
  `cream run --json hello status` — конверт cream; `cream run hello status --json`
  — `--json` получит действие.
- Аргументы `args: pairs` проверяются по `params` до запуска; секретный параметр —
  только `KEY=-`, значение читается со stdin cream и уходит на stdin действия.
- Код выхода — код действия (0–63). 64–79 — ошибки cream: не найдено, блокировка
  занята, нарушен протокол вывода, таймаут.
- Без `--json` stdout действия идёт насквозь, stderr — через фильтр секретов.

## Настройка на загрузке и система

```
cream configure [--wait] [--force] <ext>
cream configure --force --all
cream configure --again <ext>
cream configure --from-unit <ext>
cream system init
```

- `configure` запускает хук `configure` расширения сейчас, с
  `CREAM_REASON=apply`: перезапускает юнит `cream-configure@<ext>`, если systemd
  работает, иначе выполняет хук сам. `--wait` — ждать исхода: `status` `ok` или
  `skipped` (хук вернул 20) и `reboot_required` в ответе, 79 — хук упал
  (`details.status: failed`); исход юнит пишет в
  `/run/cream/configure/<ext>.result`. Без `--wait` перезапуск ставится в
  очередь и `status` нет; пока systemd загружается,
  перезапуск ставится в очередь и с `--wait` (`configure.no_block`).
  `--from-unit` — только для вызова из самого юнита. На загрузке юнит
  выполняется сам на стадии расширения —
  [стадии configure](../guides/configure-stages.md).
- **Отметка выполнения.** После успешного configure (код хука 0 или 10) cream
  пишет `/var/lib/cream/configured/<ext>.json`: версию, отпечаток дерева,
  `params_digest` несекретных настроек, счётчик записей секретов `secrets_rev`
  и sha256 от `/etc/machine-id` — без значений. На загрузке (причина `boot`)
  совпавшая отметка пропускает хук: юнит завершается успехом, в журнал юнита —
  `configure <ext>: пропуск, отметка совпала`. Не совпало что-либо (`install`,
  `upgrade`, `set`, ротация секрета, клон машины) или отметки нет — хук идёт.
  Сбой хука удаляет отметку — хук повторяется на каждой загрузке до успеха;
  код 20 («пропущено»: устройства пока нет) — не ошибка: код выхода 0 (юнит
  успешен), запись `skipped` в `history.jsonl`, отметка не пишется, а прежняя
  **снимается** — на следующей загрузке хук пойдёт снова; в ответе
  `"status": "skipped"` (через юнит — с `--wait`).
  `remove` удаляет отметку вместе с записью реестра. Причины `install`, `upgrade`,
  `apply`, `followup` идут мимо отметки. `configure.every_boot: true` в
  манифесте — хук на каждой загрузке, отметка пишется только для `show`.
  Фаза build отметок не пишет.
- `--force` удаляет отметку и запускает хук сейчас (при успехе отметка пишется
  снова); `--force --all` — для всех расширений с хуком `configure`.
- `--again` — повтор, который хук ставит себе сам: пишет причину `followup` в
  `/run/cream/<ext>.reason` и перезапускает юнит без ожидания (`--no-block`);
  следующий прогон идёт мимо отметки. Вызывать **отложенно**, когда хук уже
  завершился: `systemd-run --on-active=30 --timer-property=AccuracySec=1s
  cream configure --again <ext>` (без `AccuracySec` systemd вправе сдвинуть
  срабатывание таймера до минуты).
  Синхронный вызов из работающего хука перезапустит его же юнит и оборвёт
  текущий прогон. Без systemd ничего не запускает (`configure.no_systemd`).
- `system init` создаёт каталоги cream с нужными правами, ставит шаблон
  `cream-configure@.service` и `cream-configure-cloud-user.service`, пересчитывает
  цели configure и при работающем systemd делает `daemon-reload`. Повтор
  безвреден; его выполняет `postinst` пакета.

## Предсборочная проверка образа

```
cream check --catalog NAME=ARCHIVE|DIR… [--os ID:VERSION] [--arch A] [--installed FILE] --step '<команда>'…
```

Проверяет строки cream будущего образа сверху вниз **до сборки** — как hadolint для
Dockerfile: без root, без сети, без хуков, ничего не пишет. Состояние переносится
от шага к шагу: что поставил шаг 1, видят следующие. Ответ — по каждому шагу
(`ok`, `error` с кодом и `error.code` той же команды в госте, `skipped`), итоговые
версии и порядок установки; код выхода — первого упавшего шага.

| Флаг | Смысл |
|---|---|
| `--catalog NAME=PATH` | источник: архив `tar.gz` (читается без распаковки на диск) или каталог; можно повторять, порядок — приоритет поиска |
| `--step 'CMD'` | одна строка cream без слова `cream`: `install`, `upgrade`, `remove`, `source add`; можно повторять |
| `--os ID:VERSION` | ОС образа (`debian:12`, `ubuntu:24.04`); без флага `platform.os` и семейство не проверяются |
| `--arch A` | архитектура образа (по умолчанию — этой машины) |
| `--installed FILE` | расширения базового образа — значение метки `cream.extensions` (`[{name, version, source?, provides?}]`); пустая база — `[]`. Без флага база **неизвестна**: незакрытые `depends`/`requires` — предупреждение `check.maybe_installed` («возможно, есть в базе»), `upgrade`/`remove` неизвестного имени и ссылки в источник не из `--catalog` — `skipped` |

Что проверяется: есть ли расширение и версия (`@версия`), платформа (`platform.os`,
архитектура; `platform.commands` — только предупреждение `check.commands`),
`targets.build: none` (предупреждение `install.build_none`, в установленное не
попадает), `depends`/`requires`/`conflicts` против базы и предыдущих шагов,
параметры `KEY=VALUE` (ключ известен, значение по типу; секрет — только `KEY=-`),
обязательные ключи `scope: build` (`empty: false` без `default`): шаг `install`,
после которого пойдёт хук `install` (новое расширение, в том числе добранное
`--with-deps`, или `--reinstall`), без `KEY=…`/`KEY=-` — ошибка 64
`config.build_required`, как в госте. Шаг с `--params-file` так не проверяется
(файл видит только гость — заметка `check.params_file`); расширение, которое,
возможно, уже есть в базе или поставлено нестандартной строкой, —
предупреждение `config.build_required`.
`install --with-deps` добирает недостающие `depends` по имени из источника того
расширения, которому они нужны, транзитивно; `requires` не добирает (67
`deps.requires`) — так же, как `install` в госте.

Строка с `;`, `&&`, `|`, `$…`, перенаправлением — `skipped` с заметкой
`check.nonstandard` (нестандартная форма — проверит только гость); стандартный
хвост `< /run/secrets/<id>` для значения `KEY=-` допустим. Источники расширений
базы (поле `source` в `--installed`), которых нет среди `--catalog`, считаются
внешними: в госте они есть в `sources.yaml` базы. Диапазон `cream:` манифестов
сверяется с версией cream, который проверяет, — в бандле bisquite это та же
версия, что ставится в образ. `source add <имя> --archive URL --sha256 H` объявляет внешний каталог: на
хост он не скачивается, шаги из него — `skipped` с заметкой `check.external`, а
зависимости на их имена у следующих шагов — предупреждение
`check.maybe_installed`. Так же «неизвестны» имена упавшего шага: одна ошибка не
тянет за собой каскад. `source add --path` и прочие подкоманды — `skipped`.

```
$ cream check --catalog core=cream-extensions-5.2.0.tar.gz --os ubuntu:24.04 --arch amd64 \
    --step 'install --with-deps desktop' --step 'install docker'
1	ok	install --with-deps desktop — cloud-user 1.0.1 (install), desktop 1.0.1 (install)
2	error	install docker
	ошибка: docker: нет зависимостей apt-guard >=1.0.0 <2 — поставьте их раньше (deps.missing, код 67)
```

## Офлайн-разрешение для сборщиков образов (устарело)

`resolve` обслуживает слой bisquite 2.x (`install --from-dir --record`) и уходит
в cream 2.0; сборка образа проверяет свои строки командой `check`.

```
cream resolve [--phase build|live] [--target-arch A] [--host-arch A] [--family F] [--installed FILE] <ref> [K=V…] [<ref> [K=V…]]…
```

Разрешает слои по кешу без сети и печатает порядок установки, отпечатки дерева и
параметров. Используется сборщиком образов — [фаза build](../guides/build-phase.md).

| Флаг | Смысл |
|---|---|
| `--phase P` | `build` или `live` (по умолчанию `live`) |
| `--target-arch A` | архитектура цели (по умолчанию — этой машины) |
| `--host-arch A` | архитектура хоста сборки; слой `exec` при чужой архитектуре — код 68 |
| `--family F` | семейство пакетов цели (`deb`) |
| `--installed FILE` | только с `--phase build`: JSON-массив `[{name, version, provides?}]` расширений, уже стоящих в базовом образе |

Параметры `K=V` относятся к предыдущей ссылке. Секретный ключ задаётся только
именем (`KEY` или `KEY=-`): значение `resolve` не нужно.

## Версия

```
cream version [--json]
```

Печатает версию cream; с `--json` — и контракт (`"contract": "cream/v1"`).

## Поведение процесса

- **Сигналы.** SIGINT, SIGTERM, SIGHUP прерывают операцию: ожидание блокировки
  заканчивается кодом 75, группа процессов хука или действия завершается,
  блокировки отпускаются.
- **Блокировки.** Изменяющие операции берут `flock` на
  `/var/lib/cream/lock/global` и `/var/lib/cream/lock/<ext>`; вложенный вызов
  cream из хука наследует блокировку родителя и не ждёт сам себя. Подробнее —
  [файлы и окружение](files-and-environment.md#блокировки).
- **umask** — `022`, хуки и действия его наследуют.
- **Секреты** не попадают в argv, вывод и журнал: вывод хуков и действий
  проходит фильтр, заменяющий известные значения секретов на `***`.
