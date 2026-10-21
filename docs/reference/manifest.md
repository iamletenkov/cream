# Манифест расширения (`cream/v1`)

Формат `extension.yaml`, каталога `cream-catalog.yaml` и файла источников
`sources.yaml`. Краткая версия — `man cream-extension.yaml`. Пример с нуля —
[написать расширение](../guides/write-extension.md).

## Раскладка расширения

```
<name>/
  extension.yaml      обязателен
  hooks/<хук>         исполняемые файлы хуков — ровно те, что объявлены в hooks
  actions/<действие>  исполняемые файлы действий
  files/…             прочее: юниты, шаблоны, обёртки (имя каталога — конвенция)
  README.md
```

- Имя каталога равно `name`.
- Файлы в `hooks/` и `actions/` — с битом исполнения; UTF-8-скрипт с shebang или ELF.
- Символические ссылки — только внутрь дерева расширения.
- Разбор строгий: неизвестное поле — ошибка, скаляр вместо списка — ошибка. Ключи
  с префиксом `x-` на любом уровне игнорируются.
- Новое необязательное поле внутри `cream/v1` требует поднять `cream:` у
  манифеста, который его использует: старый cream откажет понятной ошибкой
  версии (код 68), а не «неизвестным полем».

## Поля верхнего уровня

| Поле | Тип, умолчание | Правило |
|---|---|---|
| `schema` | строка, обязателен | ровно `cream/v1` |
| `name` | строка, обязателен | `[a-z0-9][a-z0-9._-]{0,63}`; запрещены `cream`, `lib`, `ext`, `installed`, `lock`, `global` и префикс `cream-` |
| `version` | `X.Y.Z[-pre]`, обязателен | SemVer 2.0 |
| `summary` | строка, обязателен | одна строка, не длиннее 120 знаков |
| `cream` | диапазон, обязателен | версии cream, например `">=1.1.0"`; мажор диапазона — 1. Сборка cream не с тега считается подходящей (предупреждение `platform.dev_build`) |
| `kind` | `extension` \| `library`, `extension` | `library` — только файлы, без хуков; ставится как зависимость |
| `platform.family` | `deb`, обязателен | семейство пакетов |
| `platform.arch` | список, обязателен | `amd64`, `arm64` (`x86_64`, `aarch64` приводятся к ним) |
| `platform.os` | список `{id, version?}`, любой | `ID` и `VERSION_ID` из `/etc/os-release`; `version` — диапазон: `{id: ubuntu, version: ">=22.04"}` |
| `platform.commands` | список, `[]` | команды, которые должны быть в `PATH` до хука `install` |
| `targets.build` | `exec` \| `none`, `exec` | ставить ли при сборке образа (хук в госте; архитектура хоста сборки = цели) |
| `targets.live` | `yes` \| `confirm` \| `no`, `yes` | на живую машину: да; только с `--accept-risk`; нельзя |
| `targets.live_note` | строка | обязательна при `confirm` и `no`; её печатает отказ |
| `provides` | список, `[]` | способности расширения: `[a-z0-9][a-z0-9._-]{0,63}` |
| `requires` | список, `[]` | способности, которые должны дать уже установленные |
| `depends` | список `{name, version?}`, `[]` | зависимость по имени: `{name: teleport, version: ">=4.0.0 <5"}` |
| `conflicts.extensions` | список, `[]` | несовместимые имена (проверяются обе стороны) |
| `conflicts.capabilities` | список, `[]` | несовместим с любым **другим** поставщиком способности |
| `hooks` | словарь | [хуки](#хуки) |
| `config.keys` | словарь | [настройки](#настройки-configkeys) |
| `actions` | словарь | [действия](#действия) |
| `files` | список `{src, dst, mode?}` | [файлы](#файлы) |

**Диапазоны версий** — компараторы `>=`, `<=`, `>`, `<`, `=` через пробел, все
должны выполняться: `">=4.0.0 <5"`. `||`, `~`, `^` и голая версия не
поддерживаются; `"<5"` исключает и предрелизы 5.0.0.

**Зарезервировано** в `cream/v1` и отвергается кодом 65 `manifest.reserved`:
поле `bin`, хуки `secret` и `check`, `secret: hook` у ключа, `targets.build: files`.

## Хуки

```yaml
hooks:
  install: {timeout: 20m}
  upgrade: {}
  configure: {stage: system, after: [docker.service]}
  apply: {}
  remove: {}
```

| Хук | Когда вызывается | Таймаут по умолчанию |
|---|---|---|
| `install` | первая установка; повтор после сбоя; новое значение ключа `scope: install`; `--reinstall`. Обязателен, кроме `kind: library` | нет |
| `upgrade` | новая версия; без хука вызывается `install` с `CREAM_PREV_VERSION` | как у `install` |
| `configure` | на загрузке (юнит `cream-configure@<ext>`) — с 1.2.0, только если отметка выполнения не совпала с реестром (или `every_boot: true`); после `install`/`upgrade` на живой машине, после `set` при отсутствии `apply`, по `cream configure` | 15 мин; у стадии `early` — 2 мин и не больше |
| `apply` | после `cream set` с изменёнными ключами; `CREAM_CHANGED_KEYS` — их список | 60 с |
| `remove` | `cream remove`; без хука `remove` отказывает, остаётся `--forget` | 30 мин |

`timeout` — длительность Go: `30s`, `5m`, `1h`.

**Коды возврата хука:** 0 — успех; 10 — успех, нужна перезагрузка
(`reboot_required`, `/run/reboot-required`); 20 — неприменимо здесь (с
`--allow-skip` — статус `skipped`, иначе код 68), у `configure` с 1.2.0 —
«пропущено»: не ошибка, отметка снимается, повтор на следующей загрузке;
остальное — сбой (код 79). Таблица — [коды хуков](exit-codes.md#коды-хуков).

### `configure.every_boot` (с 1.2.0)

```yaml
hooks:
  configure: {stage: system, every_boot: true}
```

С cream 1.2.0 успешный `configure` пишет отметку выполнения, и на загрузке хук
идёт, только если с тех пор что-то изменилось (версия, дерево, настройки,
секрет, `machine-id`). `every_boot: true` возвращает прежнее «на каждой
загрузке». Ставьте его, если одного прогона мало: хук восстанавливает
состояние, не живущее до перезагрузки (правила ядра без сохранения, `ip link`,
`/run`, модули); перебирает пользователей или домашние каталоги; ждёт
пользователя cloud-init; сам ставит себе повтор (`cream configure --again`);
правит файлы, которые переписывает приложение; зависит от железа (GPU,
камеры, `/dev/*`). Поле требует `cream: ">=1.2.0"` (иначе 65
`manifest.invalid` с `details.since`), только у `configure`.

### Стадии `configure`

`hooks.configure.stage` обязательно у `configure` и решает, когда на загрузке
systemd запускает `cream-configure@<ext>.service`. Как выбрать стадию, что
доступно на каждой, коды возврата, повтор и отладка —
[руководство по стадиям](../guides/configure-stages.md).

| `stage` | Когда на загрузке | Пример |
|---|---|---|
| `early` | до сети, в `sysinit.target`; таймаут не больше 2 мин | файрвол |
| `system` | в `multi-user.target`, после `network.target` | службы, Docker |
| `cloud-user` | после `cloud-final.service`; без cloud-init — сразу с `multi-user.target` (с 1.1.1) | настройки пользователя cloud-init |
| `graphical` | в `graphical.target`, после менеджера входа; только если машина грузится в `graphical.target` | киоск, VNC |

`early`, `cloud-user` и `graphical` требуют `cream: ">=1.1.0"`. `after`,
`before`, `wants` — дополнительные юниты порядка, например
`before: [display-manager.service]` у того, что менеджер входа должен застать
готовым. У `early` порядок на `network.target`/`network-online.target` даёт цикл —
предупреждение `manifest.early_network`.

## Настройки (`config.keys`)

```yaml
config:
  keys:
    LISTEN_PORT: {type: port, default: "8080", description: "порт"}
    TOKEN:       {type: str, default: "", empty: true, secret: file, description: "токен"}
    LBL_*:       {type: "re:^[a-z0-9-]+$", pattern: true, description: "метки LBL_<имя>"}
```

Имя ключа — `[A-Z][A-Z0-9_]*`; запрещены `CREAM_*`, `PATH`, `LANG`, `HOME`, `PWD`,
`LD_PRELOAD`, `LD_LIBRARY_PATH`, `BASH_ENV`, `ENV`, `IFS`.

| Поле | Значение |
|---|---|
| `type` | см. таблицу типов |
| `default` | строка; пустая — только при `empty: true` |
| `empty` | `true` — пустое значение допустимо |
| `description` | обязательно |
| `secret` | `file` — хранится в `/etc/cream/<ext>/secrets` (0600), в окружение хука не попадает, в выводе маскируется |
| `readonly` | `true` — `cream set` отказывает; пишут хук, действие или владелец с `--allow-readonly` |
| `pattern` | `true` — имя вида `PREFIX_*`: подходит любой `PREFIX_<хвост>` |
| `scope` | `config` (умолчание); `install` — параметр установки: меняется только `cream install <ext> KEY=…` и перезапускает хук `install`; `build` (с 1.2.0, требует `cream: ">=1.2.0"`) — [ключ только для сборки](#ключи-scope-build) |

| Тип | Допустимые значения |
|---|---|
| `bool` | `0` или `1` |
| `int` | целое (int64) |
| `port` | 1–65535 |
| `host` | имя, IPv4 или IPv6 с необязательным `:порт` (IPv6 с портом — в `[ ]`) |
| `path` | абсолютный путь |
| `str` | строка без управляющих знаков, `$` и обратной кавычки |
| `enum:a,b,c` | одно из перечисленных |
| `re:<выражение>` | совпадение с регулярным выражением RE2 (якоря `^…$` — ваши) |
| `list:<тип>` | элементы через пробел, каждый — по типу |

### Ключи `scope: build`

```yaml
config:
  keys:
    MIRROR_TOKEN: {type: str, secret: file, scope: build, description: "токен зеркала на время сборки"}
```

Значение ключа `scope: build` получает **только** окружение хука `install` или
`upgrade` текущей операции — переменной с именем ключа (секрет тоже, вывод
хука маскируется). Оно нигде не хранится: ни в `config`, ни в `secrets`
домена, ни в `params_digest`, ни в `history.jsonl`, ни в метках образа;
`configure` и `apply` его не видят. Передаётся `cream install <ext> KEY=…`,
`KEY=-` со stdin или `--params-file` (в сборке — `--mount=type=secret`
bisquite). `cream get`/`set` на такой ключ — 64 `config.scope_build`, `show` его
не печатает. Обязательный ключ (`empty: false` без `default`) обязан быть задан
при `install` нового расширения — иначе 64 `config.build_required` (то же
заранее отвечает `cream check`); при `upgrade` не требуется, ключ с `default`
получает умолчание. Допустим в обеих фазах.

## Действия

```yaml
actions:
  status: {summary: "состояние", lock: none, output: json, phases: [build, live]}
  join:
    summary: "регистрация"
    args: pairs
    params:
      PROXY:  {required: true}                 # тип — из config.keys
      SECRET: {type: str, secret: true, required: true}
    stdin: secret
    output: json
    timeout: 5m
```

Имя — `[a-z0-9][a-z0-9-]{0,63}`, файл — `actions/<имя>`.

| Поле | Значения, умолчание | Смысл |
|---|---|---|
| `summary` | строка | описание для `cream actions` |
| `args` | `none` \| `pairs` \| `free`, `none` | без аргументов; `KEY=VALUE` с проверкой по `params`; argv как есть |
| `params` | `{KEY: {type?, required?, secret?}}` | для `pairs`; тип по умолчанию — из `config.keys` |
| `stdin` | `none` \| `secret` \| `pass`, `none` | `/dev/null`; значение секрета `KEY=-`; поток cream как есть |
| `lock` | `exclusive` \| `shared` \| `none`, `exclusive` | блокировка настроек расширения на время действия |
| `output` | `text` \| `json`, `text` | при `json` stdout — ровно один JSON-объект |
| `timeout` | длительность, `2m` | затем код 79 `action.timeout` |
| `phases` | список из `build`, `live`; `[live]` | где действие допустимо |
| `root` | `true` \| `false`, `true` | нужен ли root |

Правила:

- Секретный параметр у действия — не больше одного, только с `stdin: secret`.
- Код выхода действия — 0–63; смысл кодов документирует расширение.
- `lock: none` — действие только читает настройки. Его нужно объявить, если
  действие вызывается из `Exec*` юнита, который может перезапуститься, пока
  блокировку держит другое действие: иначе взаимная блокировка через systemd
  (`cream validate` предупреждает `validate.unit_lock`).

## Файлы

```yaml
files:
  - {src: files/hello.service, dst: /etc/systemd/system/hello.service, mode: "0644"}
```

cream раскладывает файлы до хука `install`, записывает их sha256 и удаляет при
`remove`; изменённый оператором файл сохраняется как `<dst>.cream-modified`.

- `src` — относительный путь внутри дерева, `dst` — абсолютный путь.
- `mode` — восьмеричный, без записи для всех.
- `dst` с компонентом `cream-…`, в каталогах cream (`/opt/cream`, `/var/lib/cream`,
  `/var/cache/cream`, `/usr/lib/cream`, `/run/cream`), бинарь cream и файлы
  настроек `/etc/cream/<ext>/{config,secrets}` запрещены.
- Один `dst` — у одного расширения; повтор у другого — конфликт (код 67).

## `cream-catalog.yaml`

Корень каталога расширений:

```yaml
schema: cream/v1
name: cream-extensions
version: 5.2.0          # версия каталога
roots: [extensions]     # где искать каталоги расширений
```

Имя расширения уникально во всём каталоге. Проверка — `cream validate --catalog DIR`
(вдобавок к манифестам проверяет граф `depends` внутри каталога).

## `sources.yaml`

Пишется командами `cream source …`, но читается и после ручной правки.
Порядок — приоритет поиска.

```yaml
sources:
  - name: core
    type: archive
    url: https://github.com/iamletenkov/cream-extensions/releases/download/v5.2.0/cream-extensions-5.2.0.tar.gz
    sha256: 21121c164c9239366c94607b22545f5788a70c132be18153a2c7659f36ccce97
  - name: dev
    type: path
    path: /home/me/my-catalog
```

| Поле | Правило |
|---|---|
| `name` | `[a-z0-9][a-z0-9._-]{0,63}` |
| `type` | `archive` или `path` (`git` зарезервирован) |
| `url` | для `archive`: `https://…` или `file:///…`, `tar.gz` |
| `sha256` | для `archive`, обязателен |
| `path` | для `path`, абсолютный |
