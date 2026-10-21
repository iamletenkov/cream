# Вывод `--json`

С глобальным флагом `--json` любая команда печатает в stdout ровно один
JSON-объект; сообщения cream и вывод хуков уходят в stderr. Эталонные ответы
каждой команды лежат в тестах —
[`internal/cli/testdata/golden/`](../../internal/cli/testdata/golden/)
(`"cream": "dev"` и `<ROOT>`, `<TESTDATA>` в них — подстановки тестового окружения).

## Конверт

```json
{
  "cream": "1.1.4",
  "command": "install",
  "ok": true,
  "result": {},
  "warnings": [{"code": "source.shadowed", "message": "…", "details": {}}],
  "error": null
}
```

| Поле | Значение |
|---|---|
| `cream` | версия cream |
| `command` | команда (`install`, `source`, `run`, …) |
| `ok` | нет ошибки и код выхода 0 (у `cream run` — `exit_code == 0`) |
| `result` | форма своя у каждой команды; при ошибке — `null` |
| `warnings` | всегда массив; `details` — только если есть |
| `error` | `null` или `{"code", "message", "details"}` — [коды](exit-codes.md) |

**Совместимость.** В пределах `cream/v1` поля только добавляются; удаление или
смена типа — только с новой мажорной версией. Потребитель обязан игнорировать
незнакомые поля.

## Формы `result`

| Команда | `result` | Пример |
|---|---|---|
| `version` | `{version, contract}` | [version.json](../../internal/cli/testdata/golden/version.json) |
| `source add` | `{source: {name, type, url\|path, sha256, synced, lock}, changed}` | [source-add-sync.json](../../internal/cli/testdata/golden/source-add-sync.json) |
| `source list` | `{sources: [<source>…]}` в порядке приоритета | [source-list.json](../../internal/cli/testdata/golden/source-list.json) |
| `source show` | `{source: <source>}` | [source-show.json](../../internal/cli/testdata/golden/source-show.json) |
| `source remove` | `{name, removed}` | [source-remove.json](../../internal/cli/testdata/golden/source-remove.json) |
| `source sync` | `{synced: [{name, type, origin, resolved, synced_at, catalog_version}…]}` | [source-sync.json](../../internal/cli/testdata/golden/source-sync.json) |
| `validate` | `{reports: [{target, kind, catalog?, catalog_version?, extensions, not_applicable, errors}]}` | [validate-catalog.json](../../internal/cli/testdata/golden/validate-catalog.json) |
| `resolve` | `{phase, layers: [{name, version, source, source_resolved, catalog_version, ext_digest, params_digest, deps, mode, token, pinned, secret_params, dir}]}` | [resolve-params.json](../../internal/cli/testdata/golden/resolve-params.json) |
| `install`, `upgrade` | запись реестра (`cream-state/v1`) и `changed`; несколько расширений — `{extensions: […]}` | [install-noop.json](../../internal/cli/testdata/golden/install-noop.json), [install-several.json](../../internal/cli/testdata/golden/install-several.json) |
| `remove` | `{ext, version, hook, purged, forgotten, dependents, reboot_required}` | [remove.json](../../internal/cli/testdata/golden/remove.json) |
| `list` | `{extensions: [{name, version, kind, status, domain, apply, secret, actions, reboot_required}]}` | [list.json](../../internal/cli/testdata/golden/list.json) |
| `show` | `{ext, keys: [{key, value, origin, type, secret, invalid}]}` | [show.json](../../internal/cli/testdata/golden/show.json) |
| `get`, `config get` | `{ext, key, value}` | [get.json](../../internal/cli/testdata/golden/get.json) |
| `set` | `{ext, changed, changed_keys, apply: none\|hook\|restart\|skipped, reboot_required}` | [set.json](../../internal/cli/testdata/golden/set.json) |
| `config export` | `{ext, format, text}` | [config-export.json](../../internal/cli/testdata/golden/config-export.json) |
| `configure` | `{ext, reason, mode, reboot_required}` | [configure.json](../../internal/cli/testdata/golden/configure.json) |
| `system init` | `{unit, daemon_reload}` | [system-init.json](../../internal/cli/testdata/golden/system-init.json) |
| `run` | `{action, exit_code, output}` — ниже | [run-json.json](../../internal/cli/testdata/golden/run-json.json) |
| `actions` | `{ext, version, actions: [ActionInfo…]}` — ниже | [actions.json](../../internal/cli/testdata/golden/actions.json) |

Ошибка — `result: null` и заполненный `error`, например
[install-deps-missing.json](../../internal/cli/testdata/golden/install-deps-missing.json)
(`details.missing`, `details.candidates`).

## Контракт для агентов: `run` и `actions`

Программы на машине (например,
[stvor-agent](https://github.com/iamletenkov/stvor-agent)) вызывают cream через
`cream run --json` и `cream actions --json`. Нужные действия агент проверяет по
`name` в `cream actions --json` при старте.

### `cream run --json <ext> <action> …`

```json
{"action": "action-json/status", "exit_code": 0, "output": {"ok": true, "ext": "action-json", "phase": "live"}}
```

- `output` — объект, который действие с `output: json` напечатало в stdout;
  `null` у `output: text` (его stdout при `--json` уходит в stderr) и у упавшего
  действия, которое ничего не напечатало.
- Код выхода процесса = `exit_code`; `ok` = `exit_code == 0`; `error` — `null`,
  пока действие запустилось и соблюло протокол.
- Ошибки до запуска и нарушение протокола — `result: null`, `error` с кодом
  cream 64–79.

| Случай | Пример |
|---|---|
| действие вернуло 3: `ok: false`, `error: null`, выход 3 | [run-exit-code.json](../../internal/cli/testdata/golden/run-exit-code.json) |
| `output: text` — `output: null` | [run-text.json](../../internal/cli/testdata/golden/run-text.json) |
| секрет `KEY=-` со stdin | [run-join.json](../../internal/cli/testdata/golden/run-join.json) |
| код 100 — 76 `action.protocol` | [run-exit-high.json](../../internal/cli/testdata/golden/run-exit-high.json) |
| два JSON-объекта — 76 | [run-protocol.json](../../internal/cli/testdata/golden/run-protocol.json) |
| нет действия — 66 `not_found.action` | [run-not-found.json](../../internal/cli/testdata/golden/run-not-found.json) |
| недопустимая фаза — 68 `action.phase` | [run-phase.json](../../internal/cli/testdata/golden/run-phase.json) |
| секрет в argv — 64 `secret.in_argv` | [run-secret-in-argv.json](../../internal/cli/testdata/golden/run-secret-in-argv.json) |

### `cream actions --json <ext>`

Элемент `actions`:

```json
{"name": "join", "summary": "регистрация: секрет со stdin", "args": "pairs",
 "params": [{"name": "FIXTURE_SECRET", "type": "str", "required": true, "secret": true}],
 "stdin": "secret", "lock": "exclusive", "output": "json", "timeout_seconds": 30,
 "phases": ["live"], "root": true}
```

`args`: `none|pairs|free`; `stdin`: `none|secret|pass`; `lock`:
`exclusive|shared|none`; `output`: `json|text`; `params` — пустой массив, если
параметров нет.
