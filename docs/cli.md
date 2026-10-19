# CLI cream 1.0

Справочник команд `cream` версии 1.0 (контракт `cream/v1`). Нормативный источник — [спека](specs/2026-10-18-cream-core.md)
§11; источник правды по поведению — код `internal/cli/cmd_*.go` и golden-файлы
[`internal/cli/testdata/golden/`](../internal/cli/testdata/golden/), которые закрепляют форму `result` каждой команды.
Решения, стоящие за контрактом, — [ADR-0001](adr/0001-contract-cream-v1.md) и [ADR-0002](adr/0002-single-dependency-stdlib-cli.md).

Добавления минорных версий помечены «с 1.1.0»: в минорной версии команды, флаги, поля `result` и коды ошибок только
добавляются (ADR-0001).

В примерах golden-файлов `"cream": "dev"` — версия тестовой сборки, `<ROOT>`, `<TESTDATA>`, `<FX_TREE>`,
`<NEXT_SHA256>` — подстановки путей и отпечатков тестового окружения.

## Общие правила

### Разбор аргументов
- `cream <команда> [аргументы…] [флаги]`. Разбор — stdlib `flag`, по набору флагов на команду.
- Флаги можно ставить и после позиционных (`cream set teleport K=V --no-apply`); `--` прекращает разбор.
  Исключение — `cream run`: флаги cream только **до** `<ext>` (рулинг Р13, см. [ниже](#cream-run)).
- `cream help [<команда>]`, `-h`, `--help` — справка; `cream --version` — то же, что `cream version`.
- Отложенные команды `search`, `info`, `verify`, `assume`, `lineage`, `check`, `status`, `path`, `stage`,
  подкоманда `source pin` и флаги `--with-deps`, `--allow-downgrade` (`install`), `--require-pinned` (`install`,
  `resolve`), `--git`, `--ref`, `--commit` (`source add`) распознаются и отвечают
  **64 `usage.reserved`** («зарезервировано в cream/v1, не реализовано в cream 1.0»), см.
  [install-with-deps-reserved.json](../internal/cli/testdata/golden/install-with-deps-reserved.json).

### Глобальные флаги и окружение
| Флаг | Окружение | Смысл |
|---|---|---|
| `--json` | — | stdout — ровно один JSON-объект (конверт ниже); всё человекочитаемое и вывод хуков — в stderr |
| `--root DIR` | `CREAM_ROOT` | перенести все пути под корень `DIR` (абсолютный, иначе 64 `usage.root`); проверки root и прав файлов — только при `--root /` (Р9) |
| `--sources FILE` | `CREAM_SOURCES` | свой файл источников вместо `/etc/cream/sources.yaml` |
| `--cache DIR` | `CREAM_CACHE` | свой кеш источников; `sources.lock` тогда — `<DIR>/sources.lock` (Р12) |
| `--lock-timeout D` | — | ожидание блокировок, по умолчанию `2m0s`; отрицательное — 64 |
| `-q`, `-v` | — | меньше / больше сообщений в stderr |

Без root и без `--root` cream работает в режиме пользователя: источники — `$XDG_CONFIG_HOME/cream/sources.yaml`,
кеш — `$XDG_CACHE_HOME/cream/`, `sources.lock` — `$XDG_STATE_HOME/cream/`.
Имена глобальных флагов не повторяются флагами команд (Р14).

### Конверт `--json` (спека §11.4)
```json
{
  "cream": "1.0.0",
  "command": "install",
  "ok": true,
  "result": {},
  "warnings": [{"code": "source.shadowed", "message": "…", "details": {}}],
  "error": null
}
```
- `result` — своя форма у каждой команды (ниже); при ошибке — `null`.
- `warnings` — всегда массив; `details` у предупреждения — только если есть.
- `error` — `null` или `{"code", "message", "details"}`; `details` — объект или `null`.
- `ok` = нет ошибки и код выхода 0 (у `cream run` — `exit_code == 0`).
- Без `--json` предупреждения печатаются в stderr как `cream: предупреждение: <текст> (<код>)`, ошибка — как
  `cream: <текст>`, у ошибок вызова за ней — справка команды.

**Правило изменения:** формы `result` версионируются вместе с контрактом `cream/v1`. В минорных версиях поля только
добавляются; удаление или смена типа поля — только в мажорной версии. Потребители обязаны игнорировать незнакомые
поля.

### Коды выхода (спека §11.3)
Строковые `error.code` стабильны с 1.0 (ADR-0001): в минорных версиях коды только добавляются.

| Код | Смысл | `error.code` |
|---|---|---|
| 0 | успех; `changed`, `reboot_required` — в `result` | |
| 1 | `check`/`status`: не применено (команды отложены, Р1) | `check.failed` |
| 0–63 | `cream run`: код самого действия | |
| 64 | неверный вызов: флаги, аргументы, секрет в argv, отложенное | `usage.*` (`args`, `assignment`, `duplicate`, `duplicate_key`, `ext_name`, `flag`, `format`, `no_command`, `param_required`, `phase`, `ref`, `reserved`, `root`, `secret`, `source`, `source_date_epoch`, `stdin_empty`, `stdin_param`, `stdin_too_large`, `stdin_twice`, `unknown_command`, `unknown_param`), `secret.in_argv` |
| 65 | неверные данные: манифест, каталог, значение ключа, readonly, `record.json`, юнит или стадия systemd | `manifest.*`, `catalog.*` (`schema`, `invalid`, `duplicate_name`), `config.*` (`invalid`, `unknown_key`, `readonly`, `reserved`, `install_scope`, `params_file`), `record.invalid`, `record.mismatch`, `source.invalid`, `source.conflict`, `source.reserved`, `source.catalog`, `version.downgrade`, `version.same_digest`, `hook.missing_remove`, `stage.unsupported`, `unit.invalid`; с 1.1.0 — `installed.invalid`, `catalog.depends`, `catalog.depends_cycle` |
| 66 | не найдено: расширение, источник, ключ, действие, манифест, каталог, `params-file`; `@version` не совпала | `not_found.*`, `version.mismatch`, `record.not_found` |
| 67 | граф: зависимость, версия, способность, цикл, конфликт, владение файлом, есть зависимые | `deps.*` (`missing`, `version`, `requires`, `cycle`, `dependents`), `conflict.*` (`extension`, `capability`, `file`, `file_owner`, `file_unowned`) |
| 68 | неприменимо: arch, family, os, команды, `targets`, версия cream, хук вернул 20 | `platform.*` (`arch`, `arch_barrier`, `family`, `os`, `commands`, `cream_version`), `target.*` (`build`, `live`, `confirm`), `action.phase`, `hook.not_applicable` |
| 69 | источник: не синхронизирован, sha256 не совпал, сеть, архив, путь | `source.*` (`not_synced`, `missing`, `fetch`, `redirect`, `too_large`, `bad_archive`, `path`) |
| 70 | внутренняя ошибка cream | `internal` |
| 74 | ввод-вывод (Р29), реестр повреждён или новее исполнителя, блокировка не открылась | `io.read`, `io.write`, `io.stdin`, `io.stdout`, `io.stderr`, `file.stat`, `state.corrupt`, `state.newer`, `state.tree`, `state.lock_dir`, `state.lock_file` |
| 75 | блокировка занята, таймаут или ожидание прервано сигналом | `lock.timeout`, `lock.canceled`, `lock.upgrade`, `lock.changed` |
| 76 | действие нарушило протокол: код > 63, не один JSON-объект | `action.protocol` |
| 77 | нужен root или небезопасные права файла (`params-file`, кеш, `sources.yaml`, каталог настроек) | `perm.*` (`root`, `cache`, `sources`, `etc_dir`, `config`, `secrets`, `params_file`) |
| 79 | хук или действие упали, не запустились, превысили таймаут | `hook.failed` (`details.hook_exit`), `action.failed`, `action.timeout` |

Без команды — 64 `usage.no_command`; неизвестная команда — 64 `usage.unknown_command`.

### Коды предупреждений
Предупреждение не меняет код выхода; коды стабильны так же, как коды ошибок.

| Код | Когда |
|---|---|
| `source.shadowed` | одноимённое расширение есть в нескольких источниках, взято из первого |
| `source.not_synced` | источник без кеша пропущен при поиске, расширение взято из другого |
| `catalog.cream_newer` | расширению каталога нужен cream новее текущего — пропущено, синхронизация не падает (Р22) |
| `catalog.requires`, `catalog.conflicts` | с 1.1.0, `cream validate --catalog`: `requires` без поставщика в каталоге; `conflicts.extensions` на имя не из каталога |
| `catalog.depends_external` | с 1.1.1, `cream validate --catalog`: `depends` на имя не из каталога (зависимость из другого источника раскладки) |
| `validate.lock_none_set`, `validate.unit_lock`, `validate.apply_blocking` | `cream validate`: подозрительные вызовы в скриптах (§6.3, §7.3, §8.4) |
| `manifest.early_timeout` | с 1.1.1: `hooks.configure.timeout` стадии `early` больше 2 мин — зажат до 2 мин (загрузка ждёт `early` до `sysinit.target`) |
| `manifest.early_network` | с 1.1.1: `hooks.configure.after` стадии `early` на `network.target` или `network-online.target` — цикл порядка (early стоит до `sysinit.target`) |
| `platform.dev_build` | сборка cream без версии или не с тега (`git describe`: `1.0.0-3-g<хеш>`, `…-dirty`; с 1.1.0; после предрелизного тега — `1.1.0-rc.1-2-g<хеш>` — с 1.1.1) считается удовлетворяющей `cream:` манифеста |
| `platform.os_unknown` | нет os-release: `platform.os` не проверена |
| `deps.dependents_forced` | `remove --force` убрал расширение, от которого зависят установленные |
| `state.corrupt` | битая запись реестра пропущена в списке или снята `remove --forget` (Р20, Р25) |
| `state.tree` | дерево установленного расширения не читается |
| `state.history` | запись в `history.jsonl` не удалась (операция выполнена) |
| `file.modified` | файл из `files` изменён после установки — сохранён как `<dst>.cream-modified[.N]` |
| `file.not_regular` | на месте файла из `files` не обычный файл — оставлен как есть |
| `system.registry_busy` | с 1.1.1, `cream system init`: `lock/global` занята — `configure.target` реестра не обновлён (ссылки юнитов пересчитаны); повторить `cream system init` |
| `systemd.reload_failed`, `systemd.stop_failed` | `systemctl daemon-reload` / `stop` не удался (Р25) |
| `configure.no_block` | systemd ещё загружается: перезапуск configure поставлен в очередь `--no-block`, без ожидания |
| `install.build_none` | у слоя `targets.build: none` — в фазе build не ставится |
| `install.staging`, `install.prev`, `install.unplace` | не удалось прибрать временное дерево, прежнее дерево или откатить `files` |
| `upgrade.no_source` | `upgrade --all`: расширение поставлено не из источника — пропущено |
| `build.params_file`, `build.incoming`, `build.policy_rc_d` | фаза build: не удалён `params.env` / `incoming/<id>`, `policy-rc.d` заменён хуком или не снят |

### Поведение процесса
- **Сигналы.** SIGINT, SIGTERM и SIGHUP прерывают операцию: ожидание блокировки заканчивается 75 `lock.canceled`,
  группа процессов запущенного хука или действия убивается, блокировки отпускаются.
- **ETXTBSY.** Если файл хука или действия ещё открыт на запись в параллельно форкнутом процессе, старт
  повторяется до 5 раз с растущей паузой; прочие ошибки старта не повторяются.
- **Блокировки.** Операции машины берут `flock` в `/var/lib/cream/lock/{global,<ext>}`; хук получает
  `CREAM_LOCK_FD`/`CREAM_LOCK_MODE`, и вложенный вызов cream на том же `lock/<ext>` не ждёт сам себя (Р2, Р19).
  Свои источники (XDG или `--sources` вместе с `--cache`) `lock/global` машины не трогают, но `source add`, `remove`
  и `sync` сериализуются `flock` на `<кеш>/.lock` с ожиданием по `--lock-timeout` (Р24).
- **Имена.** Имя расширения `global` и префикс `cream-` запрещены (Р15). Ключ настроек и параметр действия не могут
  называться `CREAM_*` или зарезервированным именем окружения (`PATH`, `LD_PRELOAD`, `LD_LIBRARY_PATH`, `BASH_ENV`,
  `ENV`, `IFS`, `HOME`, `PWD`, `LANG`) — 65 `manifest.invalid` при разборе манифеста.
- **umask** процесса — `022`, хуки и действия его наследуют (Р7).
- Секреты не попадают в argv, вывод и журнал: stderr хуков и действий идёт через фильтр, заменяющий значения на `***`.

## Команды

### `cream version`
`cream version [--json]` — версия и контракт. Лишние аргументы — 64.
`result`: `{"version", "contract"}` — [version.json](../internal/cli/testdata/golden/version.json),
ошибка вызова — [version-usage.json](../internal/cli/testdata/golden/version-usage.json).

### `cream source …` — источники каталогов (спека §9)
```
cream source add <name> --archive <https://…|file:///…> --sha256 <hex> [--replace] [--sync]
cream source add <name> --path <абсолютный путь> [--replace] [--sync]
cream source remove <name>
cream source list
cream source show <name>
cream source sync [<name>…]
```
| Флаг | Смысл |
|---|---|
| `--archive URL` | архив каталога `tar.gz`: только `https://` или `file:///`; голый путь — 64 |
| `--sha256 HEX` | обязателен с `--archive`; без него или без `--archive` — 64 |
| `--path DIR` | каталог на диске (абсолютный путь); копируется, не ссылается |
| `--replace` | заменить источник с тем же именем и другими полями (без флага — 65 `source.conflict`) |
| `--sync` | сразу синхронизировать; при сбое синхронизации запись не сохраняется |

Флаги `add` у других подкоманд — 64. `add` идемпотентен: те же поля — 0 и `changed: false`.
Источники машины (`/etc/cream/sources.yaml`) меняет только root под `lock/global`.
`sync` — единственное место сети: загрузка, сверка sha256 **до** распаковки (69), отказ на враждебных архивах,
проверка `cream-catalog.yaml` и всех манифестов, подмена кеша одним `rename`, запись в `sources.lock`. Расширение,
которому нужен cream новее текущего, не ломает синхронизацию: предупреждение `catalog.cream_newer` (Р22).

| Golden | Форма `result` |
|---|---|
| [source-add-archive.json](../internal/cli/testdata/golden/source-add-archive.json) | `{"source": {name, type, url, sha256, synced, lock}, "changed"}` |
| [source-add-sync.json](../internal/cli/testdata/golden/source-add-sync.json) | то же с `path` и `lock` = запись `sources.lock` `{name, type, origin, resolved, synced_at, catalog_version}` |
| [source-add-unchanged.json](../internal/cli/testdata/golden/source-add-unchanged.json) | повтор `add` — `changed: false` |
| [source-list.json](../internal/cli/testdata/golden/source-list.json) | `{"sources": [<source>…]}` в порядке приоритета |
| [source-show.json](../internal/cli/testdata/golden/source-show.json) | `{"source": <source>}` |
| [source-show-missing.json](../internal/cli/testdata/golden/source-show-missing.json) | 66 `not_found.source`, `details.source` |
| [source-remove.json](../internal/cli/testdata/golden/source-remove.json) | `{"name", "removed"}` |
| [source-sync.json](../internal/cli/testdata/golden/source-sync.json) | `{"synced": [<запись sources.lock>…]}` |

### `cream validate`
`cream validate [--catalog DIR] [DIR…]` — проверка каталога (`--catalog`, с `cream-catalog.yaml`) и/или отдельных
каталогов расширений (§4.1). Без аргументов — 64. Невалидный объект — код первой ошибки (обычно 65), отчёты целиком
лежат в `error.details.reports`.
`result`: `{"reports": [{target, kind: catalog|extension, catalog?, catalog_version?, extensions: [{name, version,
path}], not_applicable: [{ext, path, exit, code, message}], errors: [тот же вид]}]}`.

**Граф каталога** (с 1.1.0, только `--catalog`; `source sync` и `catalog pack` его не проверяют — опубликованный
каталог не отвергается): `depends` с диапазоном, не включающим версию из каталога, — 65 `catalog.depends`; цикл
`depends` — 65 `catalog.depends_cycle` (`a → b → a`); `depends` на имя, которого в каталоге нет, — предупреждение
`catalog.depends_external` (с 1.1.1; в 1.1.0 — ошибка 65: раскладка машины собирается из нескольких каталогов, §9.4,
и зависимость может прийти из другого источника — её проверяет резолвер при установке); `requires` без
поставщика в каталоге (поставщиком себе самому расширение не считается) — предупреждение `catalog.requires`
(способность может дать `cream assume` или другой источник); `conflicts.extensions` на имя не из каталога — предупреждение `catalog.conflicts`. Расширение, пропущенное
как требующее cream новее (Р22), считается присутствующим.
Golden: [validate-catalog.json](../internal/cli/testdata/golden/validate-catalog.json),
[validate-dirs.json](../internal/cli/testdata/golden/validate-dirs.json),
[validate-cream-newer.json](../internal/cli/testdata/golden/validate-cream-newer.json) (Р22: не ошибка, а
`not_applicable` и предупреждение),
[validate-invalid.json](../internal/cli/testdata/golden/validate-invalid.json),
[validate-usage.json](../internal/cli/testdata/golden/validate-usage.json),
[validate-graph.json](../internal/cli/testdata/golden/validate-graph.json) (с 1.1.0: граф каталога).

### `cream catalog pack`
`cream catalog pack DIR --out F` — сначала проверяет каталог, затем пишет детерминированный `tar.gz`: сортировка,
uid/gid 0, mtime = `SOURCE_DATE_EPOCH` (неверное значение — 64 `usage.source_date_epoch`), gzip без имени и
времени. Байтовая воспроизводимость — при одной версии Go toolchain. Ошибка записи — 74 `io.write`.

### `cream resolve`
`cream resolve [--phase build|live] [--target-arch A] [--host-arch A] [--family F] [--installed FILE] <ref> [K=V…] [<ref> [K=V…]]…`
— офлайн-разрешение слоёв по кешу (без сети), для bisquite (§12.1, §12.4). Ссылка — `[source/]name[@version]`;
параметры `K=V` относятся к предыдущей ссылке (до первой ссылки — 64). Неизвестный ключ — 65 `config.unknown_key`,
неверное значение — 65 `config.invalid`, повтор ключа — 64 `usage.duplicate_key`. Секретный ключ задаётся только
именем — `KEY=-` или голое `KEY` (Р28): значение `resolve` не нужно (в `params_digest` секрет — `<secret>`), stdin
не читается; литерал, в том числе пустой, — 64 `secret.in_argv`. Голое имя несекретного ключа — 64 `usage.args`.

| Флаг | Смысл |
|---|---|
| `--phase P` | `build` или `live` (по умолчанию `live`) |
| `--target-arch A` | архитектура цели (по умолчанию — этой машины) |
| `--host-arch A` | архитектура хоста сборки; слой `exec` при `target ≠ host` — 68 `platform.arch_barrier` |
| `--family F` | семейство пакетов цели (`deb`); без него в build семейство не проверяется (Р23) |
| `--installed FILE` | с 1.1.0, только с `--phase build` (иначе 64 `usage.args`): расширения, уже установленные в базе `FROM` слоя |

**`--installed`** (с 1.1.0, для bisquite: слой `EXTENSION` поверх образа, где часть зависимостей уже стоит). `FILE` —
JSON-массив, элемент — `{"name", "version", "provides"?}` или `{"token", "provides"?}` с `token` lineage
`[<source>/]<name>@<version>[#<rev>]`; прочие поля элемента (остальной lineage: `params_digest`, `deps`, …)
игнорируются, повтор имени — побеждает последний (lineage накопительный, база первой). Эти расширения входят в граф
как установленные: закрывают `depends` (с проверкой диапазона версий — иначе 67 `deps.version`) и `requires` (по
`provides`) слоя и попадают в его `deps`, но слоями не становятся. Без флага вывод прежний: установленного в build
нет. Нет файла — 66 `not_found.installed`, неверный формат — 65 `installed.invalid`; предел размера — 1 МиБ, `token`,
расходящийся с `name` или `version`, — тоже 65.
`FILE` — путь на хосте сборки (как его дал bisquite), **не** под `--root`. Элемент несёт только имя, версию и
`provides`: `conflicts` и `files` расширений базы в граф не входят — конфликт слоя с базой и владение файлами
`resolve` не проверяет, их проверяет `install --phase build` по реестру образа.

`result`: `{"phase", "layers": [{name, version, source, source_resolved, catalog_version, ext_digest,
params_digest, deps, mode, token, pinned, secret_params, dir}]}` в порядке установки. Значения секретных
параметров не печатаются — только их имена в `secret_params`. `token` — `<source>/<name>@<version>#<rev12>`.
Golden: [resolve-depends-lib.json](../internal/cli/testdata/golden/resolve-depends-lib.json),
[resolve-params.json](../internal/cli/testdata/golden/resolve-params.json),
[resolve-deps-missing.json](../internal/cli/testdata/golden/resolve-deps-missing.json) (67 `deps.missing`),
[resolve-arch-barrier.json](../internal/cli/testdata/golden/resolve-arch-barrier.json) (68),
[resolve-installed.json](../internal/cli/testdata/golden/resolve-installed.json) (с 1.1.0: `--installed` закрывает
`depends` и `requires`).

### `cream install`
```
cream install [--source S] [--reinstall] [--accept-risk] [--allow-skip] [--params-file F] <ref>… [KEY=VALUE…] [KEY=-]
cream install --phase build --from-dir D --record F [--params-file P] [--remove-params-file] [--allow-skip]
```
| Флаг | Смысл |
|---|---|
| `--source S` | искать ссылки без источника только в источнике `S` |
| `--reinstall` | вызвать хук `install`, даже если ничего не изменилось |
| `--accept-risk` | ставить расширение с `targets.live: confirm` (без флага — 68 `target.confirm`; `live: no` — 68 `target.live`) |
| `--allow-skip` | код 20 хука — `status: skipped` без ошибки (иначе 68 `hook.not_applicable`) |
| `--params-file F` | параметры `KEY=VALUE` из файла (0600, владелец root, иначе 77 `perm.params_file`) |
| `--phase P` | `live` (по умолчанию) или `build` — только с `--from-dir` и `--record` ([фаза build](#для-bisquite-фаза-build)) |
| `--from-dir D`, `--record F`, `--remove-params-file` | только фаза build (иначе 64) |

Несколько ссылок — только без параметров (64 `usage.args`). Секрет в argv — 64 `secret.in_argv`: передавайте
`KEY=-` (значение со stdin, один такой ключ) или `--params-file`. Зависимости сами не ставятся: нет нужного — 67
`deps.missing` с кандидатами в `details`. Без root — 77 `perm.root`. Версия ниже установленной — 65
`version.downgrade`.
`result` — запись реестра (`cream-state/v1`, §10) плюс `changed`; несколько расширений — `{"extensions": [...]}`
в порядке установки.

| Golden | Что |
|---|---|
| [install-noop.json](../internal/cli/testdata/golden/install-noop.json) | установка, `changed: true` |
| [install-unchanged.json](../internal/cli/testdata/golden/install-unchanged.json) | повтор без изменений, `changed: false` |
| [install-several.json](../internal/cli/testdata/golden/install-several.json) | несколько ссылок, `{"extensions": [...]}` |
| [install-several-params.json](../internal/cli/testdata/golden/install-several-params.json) | несколько ссылок с параметрами — 64 |
| [install-deps-missing.json](../internal/cli/testdata/golden/install-deps-missing.json) | 67 `deps.missing`, `details.missing`, `details.candidates` |
| [install-with-deps-reserved.json](../internal/cli/testdata/golden/install-with-deps-reserved.json) | `--with-deps` — 64 `usage.reserved` |
| [install-build.json](../internal/cli/testdata/golden/install-build.json) | фаза build: `phase: build`, `configure` включён |
| [install-build-mismatch.json](../internal/cli/testdata/golden/install-build-mismatch.json) | 65 `record.mismatch`, `details.{ext, field, record, found}` |

### `cream upgrade`
`cream upgrade <ext>… | cream upgrade --all` — обновление до версии в кеше источника из реестра (нет источника — 69
`source.missing`, Р8). `result` — как у `install`: [upgrade.json](../internal/cli/testdata/golden/upgrade.json),
[upgrade-all.json](../internal/cli/testdata/golden/upgrade-all.json).

### `cream remove`
`cream remove [--purge|--forget] [--force] <ext>`
| Флаг | Смысл |
|---|---|
| `--purge` | удалить и `/etc/cream/<ext>`, `/var/lib/cream/ext/<ext>` |
| `--forget` | без хука `remove`: только реестр, дерево и артефакты cream |
| `--force` | удалить, хотя от расширения зависят установленные (предупреждение `deps.dependents_forced`) |

`--purge` и `--forget` вместе — 64. Есть зависимые без `--force` — 67 `deps.dependents`. Изменённый файл из `files`
сохраняется как `<dst>.cream-modified[.N]` с предупреждением.
`result`: `{ext, version, hook, purged, forgotten, dependents, reboot_required}`.
Golden: [remove.json](../internal/cli/testdata/golden/remove.json),
[remove-purge.json](../internal/cli/testdata/golden/remove-purge.json),
[remove-forget.json](../internal/cli/testdata/golden/remove-forget.json),
[remove-force.json](../internal/cli/testdata/golden/remove-force.json),
[remove-dependents.json](../internal/cli/testdata/golden/remove-dependents.json),
[remove-purge-forget.json](../internal/cli/testdata/golden/remove-purge-forget.json),
[remove-not-installed.json](../internal/cli/testdata/golden/remove-not-installed.json) (66).

### `cream list`, `show`, `get`, `set`, `config` — настройки (спека §8)
- `cream list [--json]` — установленные расширения. `result`: `{"extensions": [{name, version, kind, status, domain,
  apply, secret, actions, reboot_required}]}` — [list.json](../internal/cli/testdata/golden/list.json).
- `cream show <ext> [--json]` — ключи. `result`: `{"ext", "keys": [{key, value, origin: default|file, type, secret,
  invalid}]}`; секрет — `***`, `(задан)` или `(пусто)` — [show.json](../internal/cli/testdata/golden/show.json),
  [show-not-installed.json](../internal/cli/testdata/golden/show-not-installed.json) (66).
- `cream get [--secret] <ext> KEY` — значение; секрет — только с `--secret` и только root. `result`: `{ext, key,
  value}` — [get.json](../internal/cli/testdata/golden/get.json). Нет ключа — 66 `not_found.key`.
- `cream set [--apply|--no-apply] [--wait] [--allow-readonly] [--params-file F] <ext> KEY=VALUE… [KEY=-]`:
  все ключи проверяются до записи, запись — одной транзакцией домена; `--apply` (по умолчанию) применяет по `apply`
  расширения, `--wait` ждёт перезапуска configure; `--apply` с `--no-apply` — 64; readonly-ключ без
  `--allow-readonly` — 65 `config.readonly`; ключ `scope: install` — 65 `config.install_scope`; секрет в argv — 64.
  `result`: `{ext, changed, changed_keys, apply: none|hook|restart|skipped, reboot_required}`.
  Golden: [set.json](../internal/cli/testdata/golden/set.json),
  [set-apply-hook.json](../internal/cli/testdata/golden/set-apply-hook.json),
  [set-no-apply.json](../internal/cli/testdata/golden/set-no-apply.json),
  [set-unchanged.json](../internal/cli/testdata/golden/set-unchanged.json),
  [set-secret-in-argv.json](../internal/cli/testdata/golden/set-secret-in-argv.json).
- `cream config get [--secret] <ext> KEY` — то же, что `get`; `cream config export --shell <ext>` — строки
  `KEY='…'` для POSIX shell, без секретов (без `--shell` — 64 `usage.format`). `result`: `{ext, format, text}` —
  [config-export.json](../internal/cli/testdata/golden/config-export.json).

### `cream configure`, `cream system init`
- `cream configure [--wait] <ext>` — запустить configure расширения (юнит `cream-configure@<ext>` или хук);
  `--from-unit <ext>` — только для вызова из самого юнита. `result`: `{ext, reason, mode, reboot_required}` —
  [configure.json](../internal/cli/testdata/golden/configure.json).
- Стадии configure (спека §6.4): `system`; с 1.1.0 — `early` (`sysinit.target`, до сети; с 1.1.1 — с
  `Conflicts=`/`Before=shutdown.target` и таймаутом configure не больше 2 мин), `cloud-user`
  (с 1.1.1 — `cream-configure-cloud-user.service`: вспомогательный юнит в `multi-user.target.wants` тянет экземпляры,
  те ждут `cloud-final.service`, если cloud-init работает; в 1.1.0 — `cloud-init.target`) и `graphical` (`graphical.target`,
  после `multi-user.target` и `display-manager.service`). Манифест с новыми стадиями обязан требовать `cream: ">=1.1.0"`, иначе 65
  `manifest.invalid`; cream 1.0 отказывает ему по `cream:` — 68 `platform.cream_version` (в каталоге — пропуск с
  `catalog.cream_newer`).
- `cream system init` — каталоги §5 с режимами, шаблон юнита и `cream-configure-cloud-user.service` из встроенных
  копий, пересчёт целей configure (ссылки стадии `cloud-user` cream 1.1.0 переносятся к вспомогательному юниту);
  пересчитанная цель пишется и в `configure.target` реестра. `lock/global` берётся только для такой записи и без
  ожидания: занята — предупреждение `system.registry_busy`, реестр не меняется, код 0 (postinst пакета не падает
  рядом с другим dpkg или хуком). При работающем systemd — `daemon-reload`. `result`: `{unit, daemon_reload}` —
  [system-init.json](../internal/cli/testdata/golden/system-init.json).

### `cream run`
`cream run [--json] [--lock-timeout D] [-q] <ext> <action> [args…]` — запустить действие расширения (§7).

**Флаги cream — только до `<ext>`** (рулинг Р13): после имени расширения перестановка флагов не действует, и всё
после имени действия уходит ему как есть, включая `--json`. `cream run --json teleport status` — конверт cream;
`cream run teleport status --json` — `--json` получает действие.

- `args: none` — аргументы запрещены (64); `free` — argv как есть, без shell; `pairs` — `KEY=VALUE`, проверяются по
  `params` до запуска (неизвестный — 64 `usage.unknown_param`, обязательный пропущен — 64 `usage.param_required`,
  неверное значение — 65 `config.invalid`).
- Секретный параметр — только `KEY=-`: значение читается со stdin cream и отдаётся на stdin действия; пустой stdin —
  64 `usage.stdin_empty`; значение в argv — 64 `secret.in_argv`.
- `lock: exclusive|shared|none` — блокировка `lock/<ext>`; занята дольше `--lock-timeout` — 75. Реестр, манифест и
  значения домена читаются уже под блокировкой; если `upgrade` за время ожидания сменил дерево расширения
  (`ext_digest` записи) — 75 `lock.changed` (Р27).
- Фаза — `CREAM_PHASE` окружения (вложенный вызов из хука), иначе `live` (Р5); недопустимая — 68 `action.phase`.
- Без `--json` stdout действия идёт насквозь, stderr — через фильтр секретов.
- Код выхода cream — код действия 0–63; код вне диапазона или не ровно один JSON-объект при `output: json` — 76
  `action.protocol`; таймаут — 79 `action.timeout`.

### `cream actions`
`cream actions <ext> [--json]` — действия установленного расширения, по имени. Не установлено — 66
`not_found.extension`. Формы — в разделе ниже.

## Контракт для stvor-agent

stvor-agent вызывает cream только через `cream run --json` и `cream actions --json`. Формы ниже закреплены
golden-файлами задачи 15 и после выпуска 1.0 **меняются только добавлением полей**; удаление поля или смена его типа
— только с мажорной версией cream (контракт `cream/v2`). Агент обязан игнорировать незнакомые поля, а нужные ему
действия проверять по `name` в `cream actions --json` при старте.

### `cream run --json <ext> <action> …`
`result` = `{"action": "<ext>/<action>", "exit_code": N, "output": <объект действия | null>}`.
- `output` — объект, который действие с `output: json` напечатало в stdout; `null` у `output: text` (его stdout при
  `--json` уходит в stderr) и у упавшего действия, которое ничего не напечатало.
- `ok` = `exit_code == 0`; код выхода процесса = `exit_code`; `error` = `null`, пока действие запустилось и
  соблюло протокол.
- Ошибки до запуска и нарушения протокола — `result: null`, `error` с кодом cream 64–79.

| Golden | Случай |
|---|---|
| [run-json.json](../internal/cli/testdata/golden/run-json.json) | `output: json`, `ok: true` |
| [run-join.json](../internal/cli/testdata/golden/run-join.json) | `args: pairs` с секретом `KEY=-` со stdin |
| [run-text.json](../internal/cli/testdata/golden/run-text.json) | `output: text` — `output: null` |
| [run-exit-code.json](../internal/cli/testdata/golden/run-exit-code.json) | код действия 3: `ok: false`, `error: null`, выход 3 |
| [run-exit-high.json](../internal/cli/testdata/golden/run-exit-high.json) | код 100 — 76 `action.protocol`, `details.exit_code` |
| [run-protocol.json](../internal/cli/testdata/golden/run-protocol.json) | два JSON-объекта — 76 |
| [run-not-found.json](../internal/cli/testdata/golden/run-not-found.json) | 66 `not_found.action`, `details.{ext, action}` |
| [run-phase.json](../internal/cli/testdata/golden/run-phase.json) | 68 `action.phase`, `details.{action, phase, phases}` |
| [run-secret-in-argv.json](../internal/cli/testdata/golden/run-secret-in-argv.json) | 64 `secret.in_argv`, `details.key` |

### `cream actions --json <ext>`
`result` = `{"ext", "version", "actions": [ActionInfo…]}`, где
```json
{"name": "join", "summary": "регистрация: секрет со stdin", "args": "pairs",
 "params": [{"name": "FIXTURE_SECRET", "type": "str", "required": true, "secret": true}],
 "stdin": "secret", "lock": "exclusive", "output": "json", "timeout_seconds": 30,
 "phases": ["live"], "root": true}
```
- `args`: `none|pairs|free`; `stdin`: `none|secret|pass`; `lock`: `exclusive|shared|none`; `output`: `json|text`;
  `params` — пустой массив, если параметров нет.

Golden: [actions.json](../internal/cli/testdata/golden/actions.json),
[actions-lock-restart.json](../internal/cli/testdata/golden/actions-lock-restart.json),
[actions-not-installed.json](../internal/cli/testdata/golden/actions-not-installed.json).

## Для bisquite: фаза build

Последовательность для одного слоя `EXTENSION` с `mode: exec` (спека §12.1–§12.2, задача 16).

1. **На хосте — разрешение.** `cream --json resolve --phase build --target-arch <arch цели> --host-arch <arch
   хоста> --family <семейство> --sources <свой sources.yaml> --cache <DATA_DIR>/cream <ref> [K=V…] [SECRET…]…`
   по всем слоям VMFILE в их порядке. Каждый элемент `layers` — будущий `record.json`; `dir` — дерево в кеше. Отказы: 67
   граф, 68 `platform.arch_barrier` (чужая архитектура для `exec`), 69 источник не синхронизирован.
2. **На хосте — staging.** Каталог 0700 с `<name>/` (копия `dir`), `record.json` (элемент `layers`; можно добавить
   `source_type`, `source_origin`) и `params.env` (все параметры слоя `KEY=VALUE`, 0600). Архив —
   `tar --owner=0 --group=0 --numeric-owner` (режимы и ссылки сохраняются): без этого в госте файлы окажутся с uid
   пользователя хоста, и проверка «владелец root» отвергнет `params.env` (77). Архив удаляется после вызова.
3. **Один вызов `virt-customize` на слой** (Р30: последовательность идёт и на `virt-customize` 1.48 Debian 12, где
   нет `--tar-in`; `tar -C` в госте нужен готовый каталог, поэтому `--mkdir` обязателен):
   ```
   --copy-in /usr/lib/cream/guest/linux-<arch>/cream:/usr/bin
   --chmod 0755:/usr/bin/cream
   --run-command '/usr/bin/cream system init'
   --mkdir /var/lib/cream/incoming/<id>
   --copy-in <staging>/<id>.tar:/var/lib/cream/incoming/
   --run-command 'tar -C /var/lib/cream/incoming/<id> --numeric-owner -xf /var/lib/cream/incoming/<id>.tar
        && rm -f /var/lib/cream/incoming/<id>.tar'
   --run-command '/usr/bin/cream --json install --phase build
        --from-dir /var/lib/cream/incoming/<id>/<name>
        --record /var/lib/cream/incoming/<id>/record.json
        --params-file /var/lib/cream/incoming/<id>/params.env --remove-params-file'
   ```
   Гостевой бинарь берётся из пакета cream на хосте и копируется всегда: версия в образе равна версии хоста.
   Распаковка в госте идёт от root, `--numeric-owner` сохраняет владельца 0:0 из архива. Секретные параметры в
   шаге 1 называются только именем (`KEY` или `KEY=-`, Р28), значения — только в `params.env`.
4. **cream в госте** под `lock/global`: сверяет имя, версию, `ext_digest` дерева и `params_digest` параметров с
   `record.json` (65 `record.mismatch`, `details.field`); проверяет `platform` и граф по **реестру гостя**
   (зависимости из FROM учитываются); ставит `/usr/sbin/policy-rc.d` (exit 101) на время хука, если его не было, и
   снимает после, в том числе при сбое; выполняет хук `install`; configure только включает ссылкой в
   `*.wants`, не запуская; пишет реестр с `phase: build`; удаляет `incoming/<id>` после успеха (при сбое оставляет
   для разбора). `params.env` с `--remove-params-file` удаляется в любом исходе.

Коды фазы build: 0 — успех ([install-build.json](../internal/cli/testdata/golden/install-build.json));
64 — неполный вызов (`--from-dir`/`--record` без `--phase build`, ссылки или `K=V` в argv, `--source`/`--reinstall`/
`--accept-risk`); 65 `record.invalid`/`record.mismatch`; 66 `record.not_found`, `not_found.params_file`;
67 граф; 68 `target.build` (`targets.build: none`), `platform.*`, `hook.not_applicable`; 75 блокировка;
77 `perm.params_file` (не 0600 или не root); 79 `hook.failed`.

Параметры не идут в argv `virt-customize` и не попадают в текст ошибок cream; сеть cream в госте не использует.
