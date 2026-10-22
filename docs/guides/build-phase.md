# Фаза build: расширения при сборке образа

Для авторов сборщиков образов и тех, кто зовёт cream внутри `virt-customize`,
`chroot` или контейнера. С cream 1.2.0 **основной путь** — обычная команда
`cream install` в госте: cream сам понимает, что идёт сборка, берёт дерево
расширения из источника, как на живой машине, и применяет правила фазы build.
Как это устроено в bisquite — [cream в образах bisquite](bisquite-images.md).

## 1. Что нужно в госте

- пакет cream (`dpkg -i cream_<v>_<arch>.deb`; `postinst` выполнит
  `cream system init`);
- синхронизированный источник — обычно каталог на диске гостя:

```bash
cream source add core --path /usr/share/cream-catalogs/core-5.2.0 --replace --sync
```

`--sync` для `--path` сети не требует. Внешний каталог по URL (`--archive …
--sha256 …`) потребует сеть у appliance — как любой `apt-get` в сборке.

## 2. Команды

```bash
cream install desktop@2.1.0 DESKTOP_AUTOLOGIN=1
cream install --with-deps x11vnc selkies
cream install --params-file /run/secrets/mirror apt-mirror     # секреты и ключи scope: build
cream upgrade --all
cream remove gnome
```

Флаг `--phase` не нужен: фаза определяется сама (раздел 3). `@версия` —
проверка: другая версия в источнике — 66. Зависимость, которой нет ни в
образе, ни в этой команде, — 67; `--with-deps` добирает `depends` из того же
источника.

## 3. Как cream узнаёт фазу

Порядок: флаг `--phase build|live` → `CREAM_PHASE` родителя (вложенный вызов из
хука) → автоопределение по PID 1:

| Что видит cream | Фаза |
|---|---|
| `/proc/1/comm` = `systemd` и есть `/run/systemd/system` | `live` |
| `/proc/1/comm` прочитан, это не systemd (init appliance `virt-customize`, контейнер без systemd) | `build` |
| `/proc` не смонтирован или не читается | отказ 64 `phase.undetermined` — задайте `--phase` |

`--json` и журнал называют фазу и причину: `"phase": "build", "reason":
"pid1=init"` (или `flag`, `env`). Docker-контейнер без systemd тоже даёт
`build` — это верно: службы там не стартуют.

## 4. Что меняется в фазе build

1. Расширение с `targets.build: none` не ставится (предупреждение
   `install.build_none`); `targets.build: exec` — ставится.
2. На время хуков cream ставит `/usr/sbin/policy-rc.d` (exit 101), если его не
   было, и снимает после, в том числе при сбое, — пакеты не запускают службы в
   образе.
3. Хук `configure` **не запускается**: cream включает юнит ссылкой в `*.wants`
   на его [стадии](configure-stages.md). Отметок выполнения в образе нет —
   первая загрузка машины из образа выполнит все `configure`.
4. systemd не трогается (`daemon-reload`, перезапуски).
5. Реестр пишет `phase: build`; `/etc/image-labels.d/cream.json` описывает
   весь реестр образа — так потомок видит, что стоит в базе.
6. Ключи `scope: build` (токен зеркала, пароль реестра) уходят только в
   окружение хука `install`/`upgrade` и нигде не хранятся
   ([манифест](../reference/manifest.md#ключи-scope-build)). Секрет домена
   (`secret: file` без `scope: build`), заданный при сборке, остаётся в образе
   — так задумано контрактом расширения; токены сборки делайте `scope: build`.

## 5. Проверка до сборки

`cream check` проверяет строки cream будущего образа без root, сети и хуков —
как hadolint для Dockerfile ([cli: check](../reference/cli.md#предсборочная-проверка-образа)):

```bash
cream --json check --catalog core=cream-extensions-5.2.0.tar.gz --os ubuntu:24.04 --arch amd64 \
  --installed base-extensions.json \
  --step 'install desktop@2.1.0 DESKTOP_AUTOLOGIN=1' --step 'install --with-deps x11vnc'
```

`--installed` — значение метки `cream.extensions` базового образа. Ошибка шага
— тот же код и `error.code`, что ответит гость.

## Коды фазы build

| Код | Причина |
|---|---|
| 0 | успех |
| 64 | `phase.undetermined` (`/proc` не читается — задайте `--phase`); `config.build_required` — обязательный ключ `scope: build` не передан (`KEY=…`, `KEY=-` со stdin, `--params-file`); `config.scope_build` — `get`/`set` такого ключа; `secret.in_argv` — значение секрета в аргументах; прочие `usage.*` |
| 65 | `manifest.invalid`, `config.invalid`, `version.downgrade` |
| 66 | нет расширения, версии или источника |
| 67 | граф по реестру образа: `deps.missing`, `deps.version`, `deps.requires`, `conflict.*` |
| 68 | `platform.*`, `hook.not_applicable`, `hold.held` |
| 69 | источник не синхронизирован или битый |
| 75 | блокировка |
| 77 | `perm.params_file`: файл не 0600 или не root |
| 79 | `hook.failed` |

## Устаревший путь: слой с хоста (`--from-dir`, `--record`)

bisquite 2.x разрешал слои на хосте (`cream resolve --phase build`), упаковывал
дерево с `record.json` и `params.env` и звал в госте

```
cream --json install --phase build --from-dir <dir>/<name> --record <dir>/record.json \
  --params-file <dir>/params.env --remove-params-file
```

Этот путь работает весь cream 1.x (контракт `cream/v1`) и уйдёт в cream 2.0;
новый код его не использует. Его коды: 64 — `--from-dir`/`--record` без
`--phase build`, ссылки или `K=V` в argv, `--source`/`--reinstall`/`--accept-risk`
вместе с `--from-dir`; 65 — `record.invalid`, `record.mismatch`; 66 —
`record.not_found`; 68 — `target.build` (`targets.build: none`). Пример ответа —
[install-build.json](../../internal/cli/testdata/golden/install-build.json).
