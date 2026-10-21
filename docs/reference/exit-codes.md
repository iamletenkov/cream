# Коды выхода и предупреждения

Код выхода — число для скриптов; `error.code` в [выводе `--json`](json-output.md)
— точная причина. Строковые коды стабильны с 1.0: в минорных версиях они только
добавляются. Без `--json` ошибка печатается в stderr как `cream: <текст>`, у
ошибок вызова за ней — справка команды.

## Коды выхода

| Код | Смысл | `error.code` |
|---|---|---|
| 0 | успех; изменилось ли что-то — `changed` в `result` | |
| 1 | `upgrade`: в терминале ответили «нет» на план — ничего не изменено | `upgrade.declined` |
| 0–63 | `cream run`: код самого действия | |
| 64 | неверный вызов: флаги, аргументы, секрет в argv, зарезервированная команда | `usage.*` (`args`, `assignment`, `duplicate`, `duplicate_key`, `ext_name`, `flag`, `format`, `no_command`, `param_required`, `phase`, `ref`, `reserved`, `root`, `secret`, `source`, `source_date_epoch`, `stdin_empty`, `stdin_param`, `stdin_too_large`, `stdin_twice`, `unknown_command`, `unknown_param`), `secret.in_argv` |
| 65 | неверные данные: манифест, каталог, значение ключа, `readonly`, запись слоя, юнит | `manifest.*`, `catalog.*` (`schema`, `invalid`, `duplicate_name`, `depends`, `depends_cycle`), `config.*` (`invalid`, `unknown_key`, `readonly`, `reserved`, `install_scope`, `params_file`), `installed.invalid`, `record.invalid`, `record.mismatch`, `source.invalid`, `source.conflict`, `source.reserved`, `source.catalog`, `version.downgrade`, `version.same_digest`, `hook.missing_remove`, `stage.unsupported`, `unit.invalid` |
| 66 | не найдено: расширение, источник, ключ, действие, манифест, файл; `@версия` не совпала | `not_found.*`, `version.mismatch`, `record.not_found` |
| 67 | граф: нет зависимости, не та версия, нет способности, цикл, конфликт, чужой файл, есть зависимые | `deps.*` (`missing`, `version`, `requires`, `cycle`, `dependents`, `failed` — поставщик не обновился в этом `upgrade`), `conflict.*` (`extension`, `capability`, `file`, `file_owner`, `file_unowned`) |
| 68 | неприменимо: архитектура, семейство, ОС, нет команд, `targets`, версия cream, хук вернул 20, расширение закреплено | `platform.*` (`arch`, `arch_barrier`, `family`, `os`, `commands`, `cream_version`), `target.*` (`build`, `live`, `confirm`), `hold.held`, `action.phase`, `hook.not_applicable` |
| 69 | источник: не синхронизирован, sha256 не совпала, сеть, архив, путь | `source.*` (`not_synced`, `missing`, `fetch`, `redirect`, `too_large`, `bad_archive`, `path`) |
| 70 | внутренняя ошибка cream | `internal` |
| 74 | ввод-вывод; реестр повреждён или новее cream | `io.read`, `io.write`, `io.stdin`, `io.stdout`, `io.stderr`, `file.stat`, `state.corrupt`, `state.newer`, `state.tree`, `state.lock_dir`, `state.lock_file` |
| 75 | блокировка занята дольше `--lock-timeout`, ожидание прервано сигналом | `lock.timeout`, `lock.canceled`, `lock.upgrade`, `lock.changed` |
| 76 | действие нарушило протокол: код больше 63 или не один JSON-объект при `output: json` | `action.protocol` |
| 77 | нужен root или у файла небезопасные права | `perm.*` (`root`, `cache`, `sources`, `etc_dir`, `config`, `secrets`, `params_file`) |
| 79 | хук или действие упали, не запустились или превысили таймаут | `hook.failed` (код хука — `details.hook_exit`), `action.failed`, `action.timeout` |

### Частичный исход `upgrade`

Новый код выхода для «обновлено не всё» не заводится: `upgrade` с `--all` или
несколькими именами возвращает код **худшего исхода** — первого упавшего
обновления (например, 79 `hook.failed`), а если упавших нет — первого пропущенного
(`blocked`: 68 риск или платформа, 67 зависимость, 69 и 66 источник, 65 версия).
Так скрипт, проверяющий `$? -eq 0`, видит неуспех, а причина остаётся той же, что
у одиночной команды. Закреплённые (`held`) и поставленные не из источника при
`--all` код не портят. В `--json` при этом `ok: false`, `error: null`, а причины —
в `result.plan[].error` и `result.summary.exit_code`. С одним именем
(`cream upgrade <ext>`) код и `error.code` — этого расширения, как в 1.1.

Частые случаи и что делать — [диагностика](../guides/diagnostics.md).

## Коды хуков

| Код хука | Значение для cream |
|---|---|
| 0 | успех |
| 10 | успех, нужна перезагрузка: `reboot_required: true`, файл `/run/reboot-required` |
| 20 | неприменимо здесь: с `--allow-skip` — статус `skipped`, иначе код 68 `hook.not_applicable` |
| другой | сбой: код 79 `hook.failed`, статус `failed` |

## Предупреждения

Предупреждение не меняет код выхода. Без `--json` — строка в stderr
`cream: предупреждение: <текст> (<код>)`, с `--json` — элемент `warnings`.

| Код | Когда |
|---|---|
| `source.shadowed` | одноимённое расширение есть в нескольких источниках — взято из первого |
| `source.not_synced` | источник без кеша пропущен при поиске |
| `catalog.cream_newer` | расширению в каталоге нужен cream новее — оно пропущено, синхронизация не падает |
| `catalog.requires`, `catalog.conflicts`, `catalog.depends_external` | `validate --catalog`: `requires` без поставщика в каталоге; `conflicts` или `depends` на имя не из каталога |
| `validate.lock_none_set`, `validate.unit_lock`, `validate.apply_blocking` | `validate`: действие с `lock: none` пишет настройки; возможна взаимная блокировка через юнит systemd; хук `apply` ждёт systemd |
| `manifest.early_timeout` | таймаут `configure` стадии `early` больше 2 мин — уменьшен до 2 мин |
| `manifest.early_network` | стадия `early` упорядочена после сети — цикл порядка на загрузке |
| `platform.dev_build` | cream собран не с тега — требование `cream:` манифеста считается выполненным |
| `platform.os_unknown` | нет `/etc/os-release` — `platform.os` не проверена |
| `deps.dependents_forced` | `remove --force` удалил расширение, от которого зависят другие |
| `state.corrupt` | битая запись реестра пропущена или снята `remove --forget` |
| `state.tree` | дерево установленного расширения не читается |
| `state.history` | запись в `history.jsonl` не удалась (операция выполнена) |
| `file.modified` | файл из `files` изменён после установки — сохранён как `<dst>.cream-modified[.N]` |
| `file.not_regular` | на месте файла из `files` не обычный файл — оставлен как есть |
| `system.registry_busy` | `system init`: шла другая операция cream — реестр не обновлён, повторите `sudo cream system init` |
| `systemd.reload_failed`, `systemd.stop_failed` | `systemctl daemon-reload` или `stop` не удался |
| `configure.no_block` | systemd ещё загружается — перезапуск configure поставлен в очередь без ожидания |
| `install.build_none` | у слоя `targets.build: none` — при сборке не ставится |
| `install.staging`, `install.prev`, `install.unplace` | не удалось убрать временное или прежнее дерево, откатить `files` |
| `upgrade.no_source` | `upgrade --all`: расширение поставлено не из источника — пропущено (`skipped`, `no_source`) |
| `build.params_file`, `build.incoming`, `build.policy_rc_d` | фаза build: не удалены `params.env` или `incoming/<id>`; `policy-rc.d` заменён хуком или не снят |
