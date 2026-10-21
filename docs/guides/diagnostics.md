# Диагностика

Отдельной команды `doctor` в cream нет: состояние собирается из `cream list`,
`show`, `actions`, журналов systemd, реестра и блокировок. Пути — системные; с
`--root` или `CREAM_ROOT` они лежат под этим корнем
([все пути](../reference/files-and-environment.md)).

## 1. Что установлено и в каком состоянии

```bash
cream version
cream list                       # имя, версия, статус, способ применения настроек
cream list --json | jq '.result.extensions[] | select(.status | startswith("failed"))'
cream show <ext>                 # ключи: значение, откуда (default | file), тип; секреты скрыты
cream actions <ext>              # действия и их блокировки
```

Ожидаемо: статус `installed` (или `skipped` — хук сказал «неприменимо здесь»),
у ключей нет `invalid`. Статусы `failed` и `failed-upgrade` чинятся повтором той
же операции: `sudo cream install <источник>/<ext>` или `sudo cream upgrade <ext>`.

## 2. Журналы настройки и операций

```bash
systemctl list-units 'cream-configure*' --all
systemctl status cream-configure@<ext>.service
journalctl -u cream-configure@<ext>.service -b --no-pager | tail -50
journalctl -u cream-configure-cloud-user.service -b --no-pager     # стадия cloud-user
tail -n 20 /var/lib/cream/history.jsonl | jq -c '{time, op, ext, from, to, status}'
```

Ожидаемо: юнит `active (exited)` или `inactive (dead)` с `status=0/SUCCESS`,
последняя запись истории по расширению — успешная. Код 79 `hook.failed` —
причина в выводе хука: в журнале юнита или в stderr команды.

Сбой `configure` на загрузке виден как упавший юнит (`systemctl --failed`) и
запись в `history.jsonl`; запись реестра при этом не меняется.

`configure` не пошёл на загрузке — с cream 1.2.0 это обычно совпавшая отметка
выполнения: в журнале юнита `configure <ext>: пропуск, отметка совпала`.

```bash
cream show --json <ext> | jq .result.configured     # {at, matches, every_boot}
sudo cream configure --wait --json <ext> | jq .result.status   # ok | skipped; 79 — упал
sudo cream configure --force <ext>                  # снять отметку и запустить сейчас
```

Код 20 хука configure — «пропущено» (`status: skipped`, юнит успешен, отметка
снята, повтор на следующей загрузке). Подробнее —
[отметка выполнения](configure-stages.md#отметка-выполнения-и-every_boot).

## 3. Блокировки (код 75)

Операции берут `flock` на `/var/lib/cream/lock/global` и
`/var/lib/cream/lock/<ext>`. Код 75 `lock.timeout` — блокировку держит другой
процесс дольше `--lock-timeout`.

```bash
sudo ls -l /var/lib/cream/lock/
sudo fuser -v /var/lib/cream/lock/global /var/lib/cream/lock/<ext>   # кто держит
ps -o pid,etime,cmd -p <PID>
```

Обычно держатель — живая операция cream или хук: дождаться. Если держатель ждёт
задание systemd юнита, чей `Exec*` сам зовёт `cream run` под той же блокировкой,
это взаимная блокировка: `systemctl list-jobs` покажет висящее задание.
Исправляется в расширении — действие из такого юнита объявляется `lock: none`;
`cream validate` предупреждает о таких местах (`validate.unit_lock`).

`cream system init` при занятой `lock/global` не ждёт: предупреждение
`system.registry_busy`, повторить позже.

## 4. Коды выхода

Полная таблица — [коды выхода](../reference/exit-codes.md). Частые:

| Код | Что проверить |
|---|---|
| 64 | вызов: флаги, аргументы, секрет в argv (`KEY=-` со stdin); `config.build_required` — не передан обязательный ключ `scope: build`; `phase.undetermined` — задайте `--phase` |
| 65 | значение ключа, манифест; текст ошибки называет поле |
| 66 | имя расширения, ключа, действия; синхронизирован ли источник |
| 67 | зависимости и конфликты: `details.missing`, `details.candidates` |
| 68 | архитектура, ОС, команды, версия cream; `--accept-risk` для `live: confirm` |
| 69 | источник: сеть, sha256 архива, путь |
| 77 | запуск не от root; права `--params-file` (0600, root) |
| 79 | вывод упавшего хука или действия |

С `--json` код и подробности — в `error.code` и `error.details`:

```bash
sudo cream install --json core/<ext> 2>/dev/null | jq '.error'
```

## 5. Источники

```bash
cream source list
cream source show <name>          # что синхронизировано: sha256, версия каталога, время
sudo cream source sync <name>     # 69 source.* — сеть, sha256 архива, путь
```
