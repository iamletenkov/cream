# Поставить и настроить расширение

Все команды этой страницы, кроме чтения (`list`, `show`, `actions`), требуют root.
Источник должен быть подключён и синхронизирован — [подключить каталог](add-catalog.md).

## Установка

```bash
sudo cream install core/locale                        # источник/имя
sudo cream install locale                             # первый источник, где оно есть
sudo cream install core/locale@2.0.0                  # утверждение версии: другая — код 66
sudo cream install core/locale LOCALE=ru_RU.UTF-8 KEYBOARD_LAYOUTS=us,ru
```

Перед запуском хука cream проверяет:

- **платформу** — семейство пакетов, архитектуру, ОС из `/etc/os-release`, нужные
  команды в `PATH`, минимальную версию cream (код 68);
- **зависимости и конфликты** — `depends`, `requires`, `conflicts`, владение файлами
  (код 67);
- **параметры** — ключ есть в манифесте и значение подходит под тип (код 65).

Зависимости сами не ставятся. cream перечислит недостающие и где они есть:

```
cream: depends-lib: нет зависимостей lib-fixture >=1.0.0 <2 — поставьте их раньше; есть в источниках: fx/lib-fixture@1.0.0
```

Несколько ссылок в одной команде ставятся в порядке зависимостей (параметры
`KEY=VALUE` тогда не принимаются):

```bash
sudo cream install core/apt-guard core/btop
```

## Повтор и переустановка

`cream install` безопасно повторять:

| Состояние | Что делает повтор |
|---|---|
| `installed`, параметры те же | ничего (`без изменений`) |
| `installed`, новые значения обычных ключей | записывает их и применяет, как `cream set` |
| `installed`, новое значение ключа установки (`scope: install`) | перезапускает хук `install` |
| `failed`, `failed-upgrade` | повторяет сорвавшуюся операцию |

`--reinstall` перезапускает хук `install` всегда.

## Рискованные и неприменимые расширения

- Расширение с `targets.live: confirm` (файрвол, Docker, киоск) меняет машину
  заметно для оператора; cream печатает предупреждение автора и ставит только с
  `--accept-risk`. `live: no` — только при сборке образа.
- Хук может ответить «неприменимо здесь» (нет устройства, GPU). Без
  `--allow-skip` это код 68; с ним — статус `skipped` без ошибки.

## Секреты

Секрет нельзя передать в аргументах — он виден в `ps` и истории оболочки (код 64
`secret.in_argv`). Один секрет — со stdin через `KEY=-`, несколько — файлом:

```bash
printf '%s' "$TOKEN" | sudo cream install core/<ext> TOKEN=-
sudo install -m 0600 /dev/null /root/params.env      # файл 0600, владелец root
sudoedit /root/params.env                            # KEY=VALUE по строке
sudo cream install --params-file /root/params.env core/<ext>
```

Секреты хранятся в `/etc/cream/<ext>/secrets` (0600), `cream show` печатает вместо
них `(задан)` или `(пусто)`, значение отдаёт только `sudo cream get --secret <ext> KEY`.

## Настройки

```bash
cream show <ext>                      # ключ, значение, откуда (default | file), тип
cream get <ext> KEY                   # одно значение
sudo cream set <ext> KEY=VALUE…       # проверить все, записать одной транзакцией, применить
sudo cream set --no-apply <ext> K=V   # только записать
sudo cream set --wait <ext> K=V       # дождаться перезапуска configure
cream config export --shell <ext>     # KEY='…' для скриптов, без секретов
```

Как применяются изменения, решает расширение: свой хук `apply`, перезапуск
`cream-configure@<ext>` или ничего (значения подхватит следующая загрузка) — это
видно в выводе: `применение: hook | restart | none | skipped`.

Отказы `set`:

| Код | Причина |
|---|---|
| 65 `config.invalid` | значение не подходит под тип |
| 65 `config.unknown_key` | ключа нет в манифесте |
| 65 `config.readonly` | ключ пишет само расширение; владелец может переписать с `--allow-readonly` |
| 65 `config.install_scope` | ключ установки: меняется через `cream install <ext> KEY=…` |
| 64 `secret.in_argv` | секрет в аргументах |

Файлы `/etc/cream/<ext>/config` и `secrets` — в формате `EnvironmentFile` systemd;
их можно читать из своих юнитов (`EnvironmentFile=/etc/cream/<ext>/config`).
Править их руками можно, но проверки типов тогда нет — используйте `cream set`.

## Действия

```bash
cream actions <ext>                          # что умеет расширение
sudo cream run <ext> <действие> [аргументы…]
printf '%s' "$SECRET" | sudo cream run <ext> join PROXY=host:443 SECRET=-
sudo cream run --json --lock-timeout 20s <ext> status
```

Флаги cream (`--json`, `--lock-timeout`, `-q`) — только **до** имени расширения;
после имени действия всё уходит действию как есть. Код выхода `cream run` — код
действия (0–63); коды 64–79 — ошибки самого cream
([коды выхода](../reference/exit-codes.md)).

## Обновление

```bash
sudo cream source add core --archive "$NEW_URL" --sha256 "$NEW_SUM" --replace --sync   # новый каталог
sudo cream upgrade <ext>              # до версии из кеша того же источника
sudo cream upgrade --all
```

Отката нет: при сбое хука `upgrade` расширение получает статус `failed-upgrade`,
прежнее дерево сохраняется, повтор — та же команда. Понизить версию (или
заменить сборку той же версии с другим отпечатком) можно только явно:
`sudo cream install --allow-downgrade <ext>` после переключения источника на
прежний каталог — без флага код 65 `version.downgrade` / `version.same_digest`.
Понижение идёт путём upgrade: хук `upgrade` с `CREAM_PREV_VERSION`, при сбое —
`failed-upgrade` и прежняя версия. План (`--dry-run`), закрепление версии
(`cream hold`), риск (`--accept-risk`) и коды частичного исхода —
[обновить расширения](upgrade-extensions.md).

## Удаление

```bash
sudo cream remove <ext>               # хук remove, юниты и файлы расширения; настройки остаются
sudo cream remove --purge <ext>       # и /etc/cream/<ext>, /var/lib/cream/ext/<ext>
sudo cream remove --forget <ext>      # без хука: только записи cream (если хук сломан или его нет)
sudo cream remove --force <ext>       # хотя от него зависят другие
```

Файл, который расширение разложило и который вы изменили, не удаляется, а
сохраняется рядом как `<файл>.cream-modified`.
