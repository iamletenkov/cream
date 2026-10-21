# cream в образах bisquite

Коротко для автора VMFILE: как bisquite 3 везёт cream в образ, как писать
строки cream и что проверяется до сборки. Подробности сборщика — в
документации [bisquite](https://github.com/iamletenkov/bisquite), устройство
фазы — [фаза build](build-phase.md).

## Что делает bisquite сам

- **Бандл.** bisquite зависит от пакетов `cream` (= пину) и
  `bisquite-cream-bundle`: в нём `.deb` и тарболы cream обеих архитектур и
  снимок каталога cream-extensions. Сети при сборке не нужно.
- **Скрытый шаг** сразу после `FROM`: ставит в гость cream пиновой версии (или
  обновляет старший), раскладывает снимок каталога в
  `/usr/share/cream-catalogs/core-<версия>/` и подключает его источником `core`
  (`type: path`). У базы с той же версией cream и каталога шаг — `noop`.
  Отключается `bs image build --no-cream` или директивой `CREAM off`.

## Строки cream в VMFILE

```
FROM ubuntu2404:dst-base
RUN_COMMAND cream install desktop@2.1.0 DESKTOP_AUTOLOGIN=1
RUN_COMMAND cream install x11vnc selkies code-server
RUN_COMMAND cream source add lab --archive https://git.example/lab-ext-1.4.0.tar.gz --sha256 9c1e… --sync
RUN_COMMAND cream install --source lab lab-tools@1.4.0
RUN_COMMAND --mount=type=secret,id=mirror cream install apt-mirror --params-file /run/secrets/mirror
```

- Одна строка — один шаг. `--phase` не пишется: в `virt-customize` cream сам
  определяет фазу `build`.
- `@версия` — утверждение: другая версия в снимке — отказ 66.
- Порядок строк задаёт автор; `--with-deps` добирает `depends` из того же
  источника.
- Секреты сборки — `--mount=type=secret` и `--params-file /run/secrets/<id>`
  (или `KEY=- < /run/secrets/<id>`): значение не попадает ни в текст команды,
  ни в журнал. Ключ расширения с `scope: build` получает значение только хук
  `install` — в образе оно не хранится
  ([манифест](../reference/manifest.md#ключи-scope-build)). Обязательный
  ключ `scope: build`, не переданный в строке, — 64 `config.build_required`.
- Директива `EXTENSION` bisquite 2.x удалена: `EXTENSION src/name@v K=V` →
  `RUN_COMMAND cream install --source src name@v K=V`.

## Проверка до сборки: `cream check`

`bs image validate -f VMFILE` и сам `bs image build` до загрузки базы зовут
один процесс `cream check` по бандлу — как hadolint для Dockerfile:

```bash
cream --json check --catalog core=/usr/share/bisquite/cream/cream-extensions-5.2.0.tar.gz \
  --os ubuntu:24.04 --arch amd64 --installed <метка cream.extensions базы> \
  --step 'install desktop@2.1.0 DESKTOP_AUTOLOGIN=1' --step 'install x11vnc selkies code-server'
```

Проверяются наличие расширения и версии, платформа, `depends`/`requires`/
`conflicts` против базы и предыдущих строк, параметры по типам, обязательные
ключи `scope: build`. Нестандартные строки (`;`, `&&`, `|`, `$…`) и шаги из
внешних каталогов — `skipped` с заметкой: их проверит гость. Без меток базы
незакрытые зависимости — предупреждения «возможно, есть в базе». Полная
справка — [cli: check](../reference/cli.md#предсборочная-проверка-образа).

## Метки образа: `/etc/image-labels.d`

cream после каждой записи реестра переписывает
`/etc/image-labels.d/cream.json` по **всему** реестру (атомарно):

```json
{"cream.version": "1.2.0",
 "cream.extensions": "[{\"name\":\"desktop\",\"version\":\"2.1.0\",\"source\":\"core\",\"digest\":\"sha256:…\",\"provides\":[\"desktop-session\"]}]",
 "cream.sources": "[{\"name\":\"core\",\"type\":\"path\",\"catalog_version\":\"5.2.0\"}]"}
```

Только имя, версия, источник, отпечаток дерева и `provides` — без параметров,
секретов и путей. В конце сборки bisquite читает `/etc/image-labels.d/*.json` в
метки OCI; файл остаётся в образе, и на живой машине тот же состав читается
без bisquite. Метка `cream.extensions` базы — это `--installed` проверки
образа-потомка. Значение больше 8 КиБ bisquite пропускает целиком — cream
предупреждает об этом (`labels.too_large`).

## Первая загрузка

В фазе build `configure` только включается. Первая загрузка машины из образа
выполняет `configure` каждого расширения на его
[стадии](configure-stages.md) и пишет отметки выполнения; дальше хук идёт
только при изменениях (или на каждой загрузке у `every_boot: true`).
