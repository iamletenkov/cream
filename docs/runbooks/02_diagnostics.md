# 02. Диагностика cream на машине

Отдельной команды `doctor` в cream нет: состояние собирается из `cream list`/`show`/`actions`, журналов systemd,
реестра и блокировок. Пути — системная раскладка (спека §5); с `--root`/`CREAM_ROOT` они лежат под этим корнем.

## 1. Что установлено и в каком состоянии
```bash
cream version
cream list                       # name, version, status (installed | failed | failed-upgrade | skipped), reboot_required
cream list --json | jq '.result.extensions[] | select(.status | startswith("failed"))'
cream show <ext>                 # ключи домена: значение, origin (default|file), invalid; секреты — ***
cream actions <ext>              # действия и их lock
```
Ожидаемо: `status` — `installed` (или `skipped`: хук вернул 20), у ключей нет `invalid: true`. `failed` и
`failed-upgrade` повторяются следующей операцией с расширением: `sudo cream install <источник>/<ext>`.

## 2. Журналы configure и операций
```bash
systemctl list-units 'cream-configure*' --all
systemctl status cream-configure@<ext>.service
journalctl -u cream-configure@<ext>.service -b --no-pager | tail -50
journalctl -u cream-configure-cloud-user.service -b --no-pager   # стадия cloud-user (с 1.1.1)
tail -n 20 /var/lib/cream/history.jsonl | jq -c '{time, op, ext, from, to, status}'
```
Ожидаемо: юнит `active (exited)` или `inactive (dead)` с `status=0/SUCCESS`; последняя запись истории по
расширению — успешная. Код хука 79 `hook.failed` — смотреть вывод хука в журнале юнита или в stderr команды.

## 3. Блокировки
Операции берут `flock` на `/var/lib/cream/lock/global` и `/var/lib/cream/lock/<ext>`. Код выхода 75
(`lock.timeout`) — блокировку держит другой процесс.
```bash
sudo ls -l /var/lib/cream/lock/
sudo fuser -v /var/lib/cream/lock/global /var/lib/cream/lock/<ext>    # кто держит (PID)
ps -o pid,etime,cmd -p <PID>
```
Ожидаемо: держатель — живая операция cream или хук (дождаться). Если держатель ждёт задание systemd юнита, чей
`Exec*` сам зовёт `cream run` под той же блокировкой, — это тупик §7.3: `systemctl list-jobs` покажет висящее
задание; расширение исправляется объявлением действия `lock: none` (его ловит `cream validate`, предупреждение
`validate.unit_lock`). `cream system init` с занятой `lock/global` не ждёт: предупреждение `system.registry_busy`,
повторить позже.

## 4. Коды выхода
Сводная таблица кодов и `error.code` — [docs/cli.md «Коды выхода»](../cli.md#коды-выхода-спека-113). Частые:
64 — неверный вызов, 65 — неверные данные (манифест, значение ключа), 66 — не найдено, 67 — граф зависимостей,
68 — неприменимо (arch, версия cream), 69 — источник, 75 — блокировка, 77 — нужен root, 79 — упал хук или действие.
С `--json` код и причина — в `error.code` и `error.details`:
```bash
sudo cream install --json core/<ext> 2>/dev/null | jq '.error'
```

## 5. Каталог и источники
```bash
cream source list
cream source show <name>          # запись sources.lock: что синхронизировано
sudo cream source sync <name>     # 69 source.* — сеть, sha256 архива, путь
```
