# cream: формат расширения и исполнитель

> 2026-10-18. Архитектурная спека ядра cream (контракт `cream/v1`). Входы: опись системы расширений bisquite
> (формат layout 2, резолвер, применение к образу, `bisquite-conf`, 40 расширений каталога, дефекты) и раздел агента
> bisquite-teleport на расширение `teleport` и службу `stvor-agent`. Решения владельца (§0) не пересматриваются.
> Канал обновлений ПО, Mender и OTA — вне рамок. Будущее место: `docs/specs/2026-10-18-cream-core.md` в репозитории
> `cream`. Потребитель контракта — спека
> [stvor-agent и расширение `teleport`](https://github.com/iamletenkov/stvor-agent/blob/main/docs/specs/2026-10-18-stvor-agent-go.md)
> (её допущения C1–C14 сверены с этой спекой, ответы — в её §12). Правки skeptic 2026-10-18 внесены; первый план
> реализации — §17 «MVP».

## 0. Решения владельца (вход, не пересматриваются)
1. cream — открытый проект (Apache-2.0) на Go. Один статический бинарь `linux/amd64` и `linux/arm64`. Распространяется
   пакетами `.deb`, по пакету на архитектуру.
2. cream состоит из двух частей. Первая — формат расширения. Вторая — исполнитель, который работает в двух местах:
   на живой машине (полный жизненный цикл) и в фазе build внутри `virt-customize` (cream вызывает bisquite).
   Исполнитель ведёт состояние установленного. `cream set/show/get/list` заменяют `bisquite-conf`: формат конфига
   прежний и совместим с `EnvironmentFile`, схема ручек типизирована, есть хуки apply и secret, `KEY=-` читает значение
   со stdin. Источники каталогов (бывшие `extensions.yaml` и `sources.lock`) переходят в cream.
3. bisquite сохраняет директиву VMFILE `EXTENSION [src/]name KEY=VAL` и исполняет её через cream. На хосте cream
   офлайн разрешает и проверяет ссылку по синхронизированному кешу. В гостя кладутся бинарь cream и каталог
   расширения. В госте выполняется `cream install --phase build …`. Подмодуль каталога в bisquite заменяется пином.
4. `cream-extensions` (бывший `bisquite-extensions`) — отдельный репозиторий. Расширения объявляют, с каким контрактом
   cream они совместимы.
5. Переходной совместимости нет: реальных машин нет. Пути в госте: `/opt/cream`, `/etc/cream`, `/var/lib/cream`.
   `bisquite-conf` исчезает.
6. Расширения `teleport` (бывшая логика `bisquite-teleport-agent`) и `stvor-agent` (служба портала) — потребители
   cream. `stvor-agent` меняет управляемые метки через **действие расширения `teleport`**. Это вариант 1 из описи:
   restart и его подтверждение делает владелец юнита. Поэтому в контракте есть действия: `cream run <ext> <action>`
   с машинно-читаемым выводом.

## 1. Цель и не-цели
**Цель.** Один исполнитель и один формат для двух мест применения расширения: сборки образа и живой машины.
Сейчас расширение — это скрипт `install.sh`. Bisquite копирует его в образ и запускает. Других гарантий нет. После
перехода на cream расширение получает:
- проверяемый граф зависимостей;
- реестр установленного;
- жизненный цикл с удалением и обновлением;
- типизированные настройки;
- действия с машинным выводом;
- воспроизводимую родословную (lineage).

Что должно стать верным:
- одинаковый `cream install teleport …` работает в `virt-customize` и на чистом Debian или Ubuntu;
- секрет ни на одном пути не попадает в argv, журнал или метку;
- установку можно воспроизвести по пинам источника без сети со стороны cream: cream в сеть не ходит (кроме явного
  `cream source sync`, §9.2); хукам может быть нужна сеть или зеркало (apt, тарбол Teleport) — это их параметры,
  а не обещание cream;
- bisquite больше не содержит резолвера, синхронизации и логики применения расширений.

**Не-цели:**
- канал обновлений, OTA, Mender, автообновление по расписанию;
- решатель версий (SAT): у источника одна версия каждого расширения, выбирать версию — задача пина источника;
- другие семейства пакетов (`rpm`, `apk`, `openwrt`): поле `family` есть, но в v1 допустимо только `deb`;
- выполнение build-хуков для чужой архитектуры (qemu-user/binfmt) и раскладка без запуска гостя: в v1 расширение
  собирается на машине той же архитектуры, что цель; режим `build: files` зарезервирован на будущую минорную
  версию (§4.4, §19 В4);
- управление пакетами ОС: apt остаётся делом хуков;
- оркестрация многих машин: на машине работает один процесс, удалённого API нет;
- Stvor-часть бутстрапа (скрипт установки, зеркало, пины в `.env`): здесь описано только то, что от cream нужно.

## 2. Термины
| Термин | Значение |
|---|---|
| **Расширение** | каталог с `extension.yaml`, хуками, действиями и файлами; единица установки |
| **Манифест** | `extension.yaml` со `schema: cream/v1` |
| **Каталог** | дерево расширений с `cream-catalog.yaml` в корне (репозиторий `cream-extensions` или его снимок) |
| **Источник** | откуда берётся каталог: `git`, `path`, `archive`; синхронизируется в кеш только командой `cream source sync` |
| **Пин** | неизменяемая ссылка: коммит git или sha256 архива |
| **Способность** (capability) | токен из `provides`/`requires`; у неё может быть несколько поставщиков |
| **Фаза** | где идёт установка: `build` — в госте `virt-customize`, systemd не работает; `live` — работающая машина |
| **Хук** | исполняемый файл `hooks/<имя>` с фиксированным именем и контрактом (§6) |
| **Действие** | исполняемый файл `actions/<имя>`, который вызывается через `cream run` (§7) |
| **Домен настроек** | настройки одного расширения: `/etc/cream/<ext>/config` и `secrets`; имя домена — имя расширения |
| **Реестр** | `/var/lib/cream/installed/<ext>.json`: что и откуда установлено (§10) |
| **Стадия configure** | точка загрузки, в которой юнит `cream-configure@<ext>` запускает хук `configure` (§6.4) |
| **Отпечаток дерева** | sha256 по отсортированным записям дерева (§9.5) |

## 3. Место в экосистеме
```
                          ┌────────────────────────── cream (Go, Apache-2.0) ───────────────────────────┐
                          │ формат cream/v1 · исполнитель · cream set/show · источники · реестр · .deb  │
                          └──────▲────────────────────▲─────────────────────────▲───────────────────────┘
                     пин версии  │  контракт cream/v1 │ cream validate в CI      │ cream_<v>_<arch>.deb
                                 │                    │                          │ (зеркало Stvor по пину)
   bisquite (Python) ────────────┤        cream-extensions (каталог) ◄── stvor-agent (Go, .deb per arch)
   VMFILE EXTENSION →            │        teleport, stvor-agent,           релиз → agent-bump PR
   cream resolve (хост)          │        docker, x11vnc, … (40)           (пин версии и sha256 по arch)
   cream install --phase build   │               │ архив cream-extensions-<v>.tar.gz + sha256
   (гость)                       │               ▼
   пин каталога (архив+sha256) ◄─┘        Stvor: зеркало /dist раздаёт .deb cream и архив каталога;
                                          скрипт: dpkg -i cream → source add + sync → cream install
                                          teleport → cream run teleport join (секрет со stdin) → cream
                                          install stvor-agent → stvor-agent enable (токен со stdin)
```
Направление зависимостей: cream ни от кого не зависит. Каталог зависит от контракта cream (поле `cream:` в
манифестах). bisquite зависит от cream (пакет на хосте) и от пина каталога. stvor-agent во время работы зависит
от действий расширения `teleport`. Stvor зависит от пинов cream, каталога и stvor-agent.

**Цепочка выпусков.** Её форма сохраняется: release-please в каждом репозитории, PR-бампы пинов по `workflow_dispatch`,
как сейчас `agent-bump` и `extensions-bump`.

| Репозиторий | Выпуск | Что после выпуска |
|---|---|---|
| `cream` | release-please (`release-type: go`), тег `vX.Y.Z`; сборка из тега: `cream_<v>_{amd64,arm64}.deb`, `cream_<v>_linux_{amd64,arm64}.tar.gz`, `SHA256SUMS` | dispatch `cream-bump.yml` в `cream-extensions` (пин версии cream для CI каталога) и в `bisquite` (минимальная версия cream и версия в CI). PR открывается токеном, CI на ветке запускается вручную, как в `agent-bump` сейчас |
| `cream-extensions` | release-please (`simple`), один тег `vX.Y.Z` на каталог; артефакт `cream-extensions-<v>.tar.gz` (детерминированный `cream catalog pack`) + `SHA256SUMS` | dispatch `extensions-bump.yml` в `bisquite`: пин архива (URL и sha256) в умолчании источников. Stvor поднимает пин вручную в `.env` |
| `stvor-agent` | release-please, `.deb` по архитектурам + `SHA256SUMS` | dispatch `agent-bump.yml` в `cream-extensions`: пин версии и sha256 по arch в расширении `stvor-agent` |

Версия расширения (`version` в манифесте) и тег каталога независимы, как сейчас. Версию расширения поднимает автор.
CI каталога требует, чтобы версия выросла при любом изменении дерева расширения (сравнение отпечатка с прошлым тегом).
Это закрывает «version ни с чем не связана».

## 4. Формат манифеста cream/v1

### 4.1 Раскладка расширения
```
<name>/
  extension.yaml        обязательно; schema: cream/v1
  hooks/                install (обязателен, кроме kind: library), upgrade, configure, apply,
                        secret, remove, check — только перечисленные в манифесте
  actions/<action>      исполняемые файлы действий
  files/…               прочее содержимое: юниты, шаблоны, обёртки (имя каталога — конвенция, не контракт)
  README.md
```
Корень каталога содержит `cream-catalog.yaml`:
```yaml
schema: cream/v1
name: cream-extensions
version: 4.0.0                # тег каталога; release-please правит его сам
roots: [extensions]           # где искать extension.yaml
```
Правила каталога:
- **имя каталога расширения равно `name`**, иначе это ошибка валидации и синхронизации (латентный дефект копирования
  из описи §3 закрывается здесь);
- имя уникально во всём каталоге, а не внутри `family`;
- символические ссылки в дереве расширения допускаются только внутрь самого дерева;
- файлы в `hooks/` и `actions/` обязаны иметь бит исполнения;
- исполняемые файлы — только UTF-8-скрипты с shebang или ELF;
- префикс `cream-` в именах юнитов systemd и файлов, которые расширение раскладывает через `files`, зарезервирован
  за cream (`cream-configure@`); юниты расширения называются по расширению (`teleport-join.timer`), а не по cream.

### 4.2 Полная схема
| Поле | Тип, умолчание | Правило |
|---|---|---|
| `schema` | строка, обязательно | ровно `cream/v1`. Иное значение — отказ «неизвестная схема». Layout 1/2 не поддерживаются (R1) |
| `name` | строка, обязательно | `[a-z0-9][a-z0-9._-]{0,63}` (строже layout 2: без заглавных и без `_` в начале, потому что имя становится частью имён юнитов). Зарезервированы `cream`, `lib`, `ext`, `installed`, `lock` |
| `version` | semver `X.Y.Z[-pre]`, обязательно | сравнивается при upgrade/downgrade, пишется в реестр и lineage |
| `summary` | строка ≤ 120 знаков, обязательно | однострочное описание для `cream search` |
| `cream` | диапазон semver, обязательно | минимальная версия исполнителя, например `">=1.0.0"`. Если cream старее, это отказ (код 68). Мажор диапазона обязан совпадать с мажором схемы |
| `kind` | `extension` \| `library`, умолчание `extension` | `library` означает только файлы: хуков нет, ставится по `depends` (§4.5) |
| `platform.family` | `deb`, обязательно | v1 знает только `deb` |
| `platform.arch` | непустой список из `amd64`, `arm64` | `aarch64` и `x86_64` канонизируются |
| `platform.os` | список `{id, version}`, умолчание — любой | `id` и `VERSION_ID` из `/etc/os-release`. `version` — диапазон (`">=12"`). Проверяется в госте и на машине |
| `platform.commands` | список, умолчание `[]` | команды, которые должны быть в `PATH` до хука `install` (требования к базе, открытая проблема описи §9.4) |
| `targets.build` | `exec` \| `none`, умолчание `exec` | **как** ставится при сборке: `exec` — хук в госте (нужна архитектура хоста = цели); `none` — при сборке не ставится. Значение `files` (раскладка без запуска гостя) **зарезервировано** на будущую минорную версию: v1 отвергает его ошибкой «зарезервировано» (65), а не «неизвестное значение» |
| `targets.live` | `yes` \| `confirm` \| `no`, умолчание `yes` | можно ли ставить на живую машину. `confirm` требует `--accept-risk`; текст риска — в `targets.live_note` (например, docker удаляет `docker.io`, firewall может отрезать SSH) |
| `targets.live_note` | строка | обязательна при `confirm` и `no` |
| `provides` | список способностей, `[]` | токены `[a-z0-9][a-z0-9._-]{0,63}` |
| `requires` | список способностей, `[]` | каждая должна быть у кого-то из установленного (или в `cream assume`) **до** установки |
| `depends` | список `{name, version?}`, `[]` | зависимость **по имени** с диапазоном версий, например `teleport >=4.0.0 <5`. Нужна библиотекам и контрактам между конкретными расширениями |
| `conflicts.extensions` | список имён, `[]` | конфликт по имени; симметрия не требуется: проверяются обе стороны |
| `conflicts.capabilities` | список способностей, `[]` | «несовместим с любым **другим** поставщиком X». Так выражается взаимоисключение `gnome/xfce4/lxde` (`desktop-session`) без перечня имён |
| `hooks.<имя>` | объект `{timeout?}` или `{}` | объявленный хук обязан существовать файлом, а файл без объявления — ошибка валидации |
| `hooks.configure.stage` | `early` \| `system` \| `cloud-user` \| `graphical`, обязательно при `configure` | §6.4 |
| `hooks.configure.after`/`before`/`wants` | списки имён юнитов, `[]` | дополнительный порядок, например `display-manager.service` у x11vnc |
| `config.keys` | словарь `KEY → описание ключа` | §8.2; заменяет файл `knobs` |
| `actions.<имя>` | объект (§7.1) | |
| `files` | список `{src, dst, mode?}` | файлы, которые раскладывает **cream**: юниты, правила udev, `/etc/…`. Они попадают в реестр с sha256, удаляются при `remove` и проверяются `cream verify`. Если `dst` уже принадлежит другому расширению — конфликт (код 67) |
| `bin` | список `{name, target}` | ссылки `/usr/local/sbin/<name> → /opt/cream/ext/<ext>/<target>`. Владение явное: у одной ссылки один владелец (раньше действовало правило «последний установивший побеждает») |

Скаляр вместо списка **не** принимается (в layout 2 принимался). Неизвестные поля — ошибка, за одним исключением:
поля с префиксом `x-` игнорируются, это задел для расширения формата без новой схемы. Совместимые добавления
внутри `cream/v1` (новое необязательное поле) требуют поднять минимальную версию в `cream:` у манифеста, который
их использует. Старый cream откажет по `cream:` с понятной ошибкой, а не по «неизвестному полю».

### 4.3 Пример (`teleport`, сокращено)
```yaml
schema: cream/v1
name: teleport
version: 4.0.0
summary: Узел Teleport — регистрация, рендер teleport.yaml, управляемые метки
cream: ">=1.0.0"
platform:
  family: deb
  arch: [amd64, arm64]
  os: [{id: debian, version: ">=12"}, {id: ubuntu, version: ">=22.04"}]
  commands: [curl, tar]
targets: {build: exec, live: "yes"}
provides: [teleport-node]
conflicts: {capabilities: [teleport-node]}
hooks:
  install: {timeout: 20m}
  upgrade: {}
  remove: {}
  apply: {}
config:
  keys:
    TELEPORT_VERSION: {type: "re:^[0-9]+\\.[0-9]+\\.[0-9]+$", default: "18.10.0", scope: install,
                       description: "версия бинаря Teleport"}
    TELEPORT_SHA256:  {type: "re:^[0-9a-f]{64}$", default: "", empty: true, scope: install,
                       description: "sha256 тарбола архитектуры машины"}
    TELEPORT_MIRROR:  {type: str, default: "", empty: true, scope: install,
                       description: "база зеркала тарболов; пусто — cdn.teleport.dev"}
    TELEPORT_TARBALL: {type: path, default: "", empty: true, scope: install,
                       description: "локальный тарбол (офлайн); взаимоисключим с TELEPORT_MIRROR"}
    TELEPORT_PROXY:   {type: host, default: "", empty: true, readonly: true, description: "адрес proxy кластера; пишет join"}
    TELEPORT_ENV:     {type: "re:^[a-z0-9][a-z0-9-]{0,62}$", default: "", empty: true, description: "метка env"}
    TELEPORT_LABELS:  {type: "list:re:^[a-z0-9_.-]+=[a-z0-9-]+$", default: "", empty: true, description: "метки оператора"}
    TELEPORT_MANAGED_LABELS: {type: "list:re:^[a-z][a-z0-9_]{0,62}=[a-z0-9][a-z0-9._-]{0,62}$", readonly: true,
                              default: "", empty: true,
                              description: "метки внешнего управляющего; пишет только действие set-managed"}
actions:
  join:            {args: pairs, params: {TELEPORT_PROXY: {required: true},   # прочие ключи join — спека stvor-agent §10.5
                                          TELEPORT_REGISTRATION_SECRET: {type: str, secret: true}},
                    stdin: secret, lock: exclusive, timeout: 5m, output: json, phases: [live]}
  join-retry:      {lock: exclusive, timeout: 5m, phases: [live]}       # teleport-join.timer и ручной запуск
  set-managed:     {args: free, lock: exclusive, timeout: 3m, output: json, phases: [live]}
  managed-status:  {lock: none, output: json, phases: [live]}
  status:          {lock: none, phases: [live]}
  leave:           {lock: exclusive, output: json, phases: [live]}
  render:          {lock: none, phases: [build, live]}   # ExecStartPre/ExecReload teleport.service (§7.3)
  reload-apps:     {lock: none, phases: [live]}          # ExecStart teleport-apps.service (§7.3)
files:
  - {src: files/teleport.service,      dst: /etc/systemd/system/teleport.service,      mode: "0644"}
  - {src: files/teleport-apps.path,    dst: /etc/systemd/system/teleport-apps.path,    mode: "0644"}
  - {src: files/teleport-apps.service, dst: /etc/systemd/system/teleport-apps.service, mode: "0644"}
```
В `params` действия тип ключа берётся из `config.keys`, если имя совпадает, поэтому повторять его не нужно. Секретный
параметр действия, которого нет в `config.keys`, объявляется в `params` с `type` и `secret: true` и никуда не
пишется: регистрационный секрет живёт только на stdin `join` (§7.2). `set-managed` принимает `args: free`, потому что
ключи управляемых меток заранее не известны; набор проверяет само действие по типу `TELEPORT_MANAGED_LABELS`.
Ключи `scope: install` — параметры бинаря: их смена — повтор `cream install` (§8.2, §8.5). У `teleport` нет хука
`configure`: юниты включает `install`, регистрация — действие `join`, на загрузке переприменять нечего.

### 4.4 Ось «когда» против оси «нужен ли гость»
Открытая проблема описи §9.1 решается разделением полей.
- `targets.build` отвечает, **как** ставить при сборке: `exec` (хук в госте) или `none`. Барьер архитектуры в v1
  остаётся: слой `exec` собирается на машине той же архитектуры, что цель. `nocloud-cidata` и `cloud-growroot` —
  `exec` (у первого — `live: no`); `raspios-normalized` на arm64 собирается на arm64-хосте. Раскладка без запуска
  гостя (`build: files`, `cream stage`) — не v1: значение зарезервировано, решение — в будущей минорной версии
  (§19, В4). Открытая проблема описи §9.1 в v1 **не** закрывается, а явно откладывается.
- `targets.live` отвечает, применимо ли расширение к работающей машине.
- «Первая загрузка» больше не фаза. Это хук `configure`, который cream запускает на каждой загрузке в объявленной
  стадии (§6.4). Объявление «только firstboot» невозможно по построению: выключенный в bisquite режим исчезает.

### 4.5 Библиотеки вместо `lib/` источника (R10)
Механизм `lib/` с копией по sha256 и ссылкой `<ext>/lib` в cream/v1 **упраздняется**. Его содержимое разносится так:

| Сейчас (`lib/`) | Становится | Почему |
|---|---|---|
| `bisquite-conf` (956 строк bash) | встроено в cream (`cream set/show/get/list` и `cream config` для скриптов) | решение владельца; разбор `EnvironmentFile` на Go проверяется одной сюитой |
| `bisquite-desktop` + `knobs/desktop{,.apply}` | расширение `desktop` (домен `desktop`, хук `configure` стадии `graphical` вместо `bisquite-desktop.service`, действия `apply`, `status`, `defaults`); `gnome`, `xfce4`, `lxde`, `cloud-user-desktop` получают `depends: [{name: desktop}]` | у домена один владелец, и он переживает `remove` одного из столов |
| `get_cloud_user.sh` | `kind: library` `cloud-user` (путь `/opt/cream/ext/cloud-user/get_cloud_user.sh`) | 12 потребителей; версия видна в реестре; один экземпляр вместо россыпи `lib/<sha>` |
| `l4t` | `kind: library` `l4t` | то же |

Библиотека ставится как обычное расширение без хуков. Потребитель объявляет её в `depends` с диапазоном версий
и обращается к ней по стабильному пути `${CREAM_ROOT}/opt/cream/ext/<lib>/…`. Путь печатает `cream path <lib>`.
Удалить библиотеку, пока у неё есть зависимые, нельзя.

### 4.6 Отличия от layout 2 (сводка)
- Схема объявлена явно (`schema`), и есть требование к исполнителю (`cream`).
- `phase` заменён парой `targets.build`/`targets.live` и стадией `configure`.
- Граф **исполняется**. `conflicts` разведены: по именам и по способностям. Появился `depends` по имени с версией.
- Файл `knobs` перенесён в `config.keys`. `ro:`, `secret:`, `?` стали полями `readonly`, `secret`, `empty`.
- `knobs.apply` стал `hooks/apply`, `knobs.secret` — `hooks/secret`, `install.sh` — `hooks/install`,
  `configure.sh` — `hooks/configure`, `update.sh` — `hooks/upgrade`. Самописные `configure-<ext>.service` удаляются.
- CLI расширений (`bisquite-firewall`, `bisquite-teleport`, `bisquite-desktop`) стали действиями. Если ссылка в `PATH`
  всё же нужна, её объявляют в `bin`.
- `lib/` упразднён (§4.5). Скаляры вместо списков не принимаются. Имя каталога обязано совпадать с `name`.

### 4.7 Миграция каталога
Конвертер — одноразовый скрипт `tools/convert-v1.py` в `cream-extensions`. Он удаляется после миграции и не входит
в cream. Конвертер механически переносит поля, `knobs`, имена хуков и пути `/opt/bisquite` → `/opt/cream/ext`,
`/etc/bisquite` → `/etc/cream`, `/var/lib/bisquite` → `/var/lib/cream/ext`. Разбор `targets.live` по таблице описи §6
и замена самописных юнитов стадиями делаются руками, по расширению. Каталог получает мажор (4.0.0), каждое
расширение — свой мажор. Порядок работ — в §16.

## 5. Раскладка на машине
| Путь | Что | Владелец | Режим |
|---|---|---|---|
| `/usr/bin/cream` | бинарь | `.deb` или bisquite (§12) | 0755 |
| `/usr/lib/systemd/system/cream-configure@.service` | шаблон юнита configure | `.deb` или `cream system init` | 0644 |
| `/usr/lib/cream/guest/linux-{amd64,arm64}/cream` | бинари для гостя (только на хосте сборки) | `.deb` | 0755 |
| `/opt/cream/ext/<ext>/` | дерево расширения, как в источнике; рабочий каталог хуков и юнитов | cream | root, 0755/0644 по биту исполнения |
| `/etc/cream/sources.yaml` | источники | `cream source …` (можно править руками) | 0644 |
| `/etc/cream/<ext>/config` | несекретные настройки, `EnvironmentFile` | cream | 0644 |
| `/etc/cream/<ext>/secrets` | секреты с `secret: file` | cream | 0600 |
| `/etc/cream/<ext>/…` | прочие файлы расширения (например, `teleport/apps.d/`) | расширение | — |
| `/etc/systemd/system/cream-configure@<ext>.service.d/10-stage.conf` | порядок стадии | cream | 0644 |
| `/var/lib/cream/installed/<ext>.json` | реестр (§10) | cream | 0644 |
| `/var/lib/cream/history.jsonl` | журнал операций без секретов | cream | 0644 |
| `/var/lib/cream/assumed.json` | объявленные вручную способности | cream | 0644 |
| `/var/lib/cream/sources.lock` | наблюдаемые ревизии источников | cream | 0644 |
| `/var/lib/cream/ext/<ext>/` | состояние расширения | расширение | 0755 (секретное — 0600 внутри) |
| `/var/lib/cream/lock/{global,<ext>}` | файлы `flock` | cream | 0600 |
| `/var/lib/cream/incoming/<id>/` | приёмник build-фазы (§12) | bisquite → cream удаляет | 0700 |
| `/var/cache/cream/sources/<source>/` | кеш источников | cream | 0755 |

Блокировки лежат в `/var/lib/cream/lock`, а не в `/run`. Так они одинаково работают в appliance `virt-customize`.
Хост сборки без root работает в режиме пользователя: `$XDG_CONFIG_HOME/cream/sources.yaml`,
`$XDG_CACHE_HOME/cream/`, `$XDG_STATE_HOME/cream/sources.lock`. bisquite передаёт свои пути явно (`--sources`,
`--cache`, §12). Глобальный `--root DIR` (`CREAM_ROOT`) переносит все пути под корень: это нужно для тестов без root
и для фикстур. Он заменяет `BISQUITE_CONF_ROOT`.

## 6. Жизненный цикл и хуки (контракт)

### 6.1 Хуки
| Хук | Когда | Обязателен | Таймаут по умолчанию |
|---|---|---|---|
| `install` | первая установка; повтор — при `status: failed`, при смене ключа `scope: install` или с `--reinstall` (§6.5) | да (кроме `library`) | нет (сборка ядра идёт часами); явный `timeout` рекомендован |
| `upgrade` | установлена версия ниже новой. Если хука нет, вызывается `install` с `CREAM_PREV_VERSION` | нет | как у `install` |
| `configure` | на каждой загрузке (юнит `cream-configure@<ext>`, стадия §6.4); после `install`/`upgrade` на живой машине; по `apply` без своего хука | нет | 15 мин |
| `apply` | после `cream set` с изменёнными ключами (§8.4) | нет | 60 с |
| `secret` | `cream set`/`install` ключа с `secret: hook`. На stdin — значение, `$1` — ключ. Хук печатает пары производных `readonly`-ключей | при наличии такого ключа | 60 с |
| `remove` | `cream remove` | нет: без хука `remove` отказывает, остаётся `--forget` | 30 мин |
| `check` | `cream check`/`cream status`. Только читает: 0 — применено и здорово, 1 — нет, stdout — необязательный JSON-объект | нет | 30 с |

### 6.2 Коды возврата хуков
- `0` — успех.
- `10` — успех, нужна перезагрузка. cream пишет `reboot_required` в реестр и в JSON-ответ и создаёт
  `/run/reboot-required` на живой машине.
- `20` — неприменимо в этой среде: нет устройства, ядра, GPU. cream не считает это сбоем, если вызвал `--allow-skip`.
  Иначе это ошибка «неприменимо» (код cream 68), и в реестре остаётся `status: skipped`.
- Прочие ненулевые коды — сбой.

Коды `10` и `20` выбраны вне диапазона `1`/`2`, чтобы `set -e` не давал их случайно.

### 6.3 Окружение хука
cream запускает хук напрямую через `exec`, без оболочки:
- cwd — `/opt/cream/ext/<ext>`;
- `umask 022`, stdin — `/dev/null` (кроме `secret`);
- `PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin`, `LANG=C.UTF-8`;
- окружение очищено.

Переменные:
- `CREAM_PHASE` — `build|live`;
- `CREAM_EXT`, `CREAM_EXT_VERSION`, `CREAM_PREV_VERSION` (upgrade);
- `CREAM_EXT_DIR`, `CREAM_ETC_DIR` (`/etc/cream/<ext>`), `CREAM_STATE_DIR` (`/var/lib/cream/ext/<ext>`), `CREAM_ROOT`;
- `CREAM_CONFIG_FILE`, `CREAM_SECRETS_FILE`;
- `CREAM_REASON` — для configure `boot|install|upgrade|apply`;
- `CREAM_CHANGED_KEYS` — для apply, через пробел;
- `CREAM_LOCK_FD` — для вложенных вызовов, см. ниже;
- **все несекретные ключи домена** под их именами. Так `install.sh` из layout 2 переносится почти без правок.

Секретные ключи в окружение **не попадают**. Хук читает их из `CREAM_SECRETS_FILE` или командой
`cream config get --secret <ext> KEY`.

Вывод хука (stdout и stderr) cream пропускает через фильтр: известные ему значения секретов заменяются на `***`.
Затем вывод идёт в stderr cream (в фазе build — на консоль) и в журнал systemd, когда cream запущен юнитом.

**Блокировки.**
- `install`, `upgrade`, `remove` и `assume` держат `lock/global` (эксклюзивно) и `lock/<ext>`.
- `set`, `apply`, `configure` и действия с `lock: exclusive` держат `lock/<ext>` эксклюзивно; `lock: shared` — разделяемо.
- Действия с `lock: none` не берут ничего; такое действие обязано только читать домен (запись из него — нарушение
  контракта, валидатор каталога ищет `cream set` в действиях с `lock: none`).
- Вложенный `cream` из хука или действия (например, `cream set --allow-readonly` из действия) наследует блокировку.
  Родитель передаёт дескриптор в `CREAM_LOCK_FD`, и cream проверяет, что это тот же файл. Наследование работает
  только по цепочке процессов: юнит systemd, который владелец блокировки запустил или перезапустил, — **не** потомок
  и дескриптора не получает. Отсюда правило §7.3 о `lock: none` для действий из `Exec*` юнитов.
- Ожидание — `--lock-timeout` (по умолчанию 120 с), затем код 75.

Так снимается гонка «оператор `cream set teleport` против `stvor-agent` → `set-managed`» (опись агента, В7).

**Идемпотентность — обязанность хука.** Повторный `install`/`configure` с теми же входами не должен менять систему.
Резервные копии нельзя перезаписывать: дефект `l4t-board-fdt` из описи — пример того, что запрещено. CI каталога
прогоняет каждый хук дважды на фикстурной машине (§14), где это возможно.

### 6.4 Юнит configure и стадии (R5)
Расширение не ставит собственные `configure-*.service`. В cream один шаблон:
```
[Unit]
Description=cream: configure %i
ConditionPathExists=/var/lib/cream/installed/%i.json
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/bin/cream configure --from-unit %i
```
cream при установке включает экземпляр ссылкой в каталог `*.wants` выбранной цели. Это работает без запущенного
systemd, в том числе в build. Порядок cream пишет в drop-in `10-stage.conf`:

| Стадия | Подключение | Порядок | Пример |
|---|---|---|---|
| `early` | `sysinit.target.wants` | `DefaultDependencies=no`, `After=local-fs.target`, `Before=network-pre.target sysinit.target`, `Wants=network-pre.target` | firewall |
| `system` | `multi-user.target.wants` | `After=network.target` (+ `after` манифеста) | docker, chromium-kiosk, rustdesk |
| `cloud-user` | `cloud-init.target.wants`, если в системе есть `cloud-final.service`, иначе `multi-user.target.wants` | `After=cloud-final.service` | code-server, wallpaper, jetson-stats |
| `graphical` | `graphical.target.wants` | `After=display-manager.service` (+ `after` манифеста) | desktop, kiosk, x11vnc, selkies, pixelflux-* |

Стадия `cloud-user` кодирует урок описи. `cloud-final` упорядочен `After=multi-user.target`, поэтому
`WantedBy=multi-user` вместе с `After=cloud-final` даёт цикл. Выбор цели по наличию cloud-init закрывает и случай
«на живой машине без cloud-init configure не запускается никогда». cream пересчитывает цель при каждом
`cream system init` и `upgrade`.

Почему один шаблон, а не один общий юнит на все расширения: стадии разные (firewall до сети, рабочий стол после DM),
а сбой одного расширения не должен останавливать остальные. Отвергнутое — в §18.

**На живой машине** после успешного `install`/`upgrade` cream делает `daemon-reload` (если раскладывал юниты через
`files`) и синхронно выполняет `configure` (`CREAM_REASON=install|upgrade`). Это не делается, если
`systemctl is-system-running` возвращает `starting|initializing`: тогда экземпляр ставится в очередь
`systemctl --no-block restart`, а cream об этом предупреждает. Так закрывается «install включает, но не запускает».
**В build** cream только включает экземпляр, configure не вызывается: так ведёт себя и нынешняя сборка.

Сбой `configure` при загрузке — это `failed` юнит (видно в `systemctl --failed`) и запись в `history.jsonl`.
Реестр при этом не меняется.

### 6.5 Операции жизненного цикла
- **install.** Последовательность:
  1. проверки §9.4;
  2. копия дерева в `/opt/cream/ext/.<ext>.new`, затем `rename`;
  3. раскладка `files` и `bin`;
  4. запись параметров в домен (§8);
  5. хук `install`;
  6. включение configure;
  7. запись реестра.

  При сбое хука реестр получает `status: failed`, дерево и файлы остаются для повтора или `remove`.
  **Повтор `cream install` той же версии и того же отпечатка** решается по реестру:
  - `status: installed` и ни один ключ `scope: install` не меняет значения — хук не вызывается (код 0,
    `changed: false`); прочие переданные параметры применяются как `set --apply` (§8.5);
  - `status: installed`, но передан ключ `scope: install` с новым значением — хук `install` перезапускается
    (в окружении — новые значения); так меняется, например, версия бинаря Teleport;
  - `status: failed` — хук `install` перезапускается автоматически, без флагов (повтор после сбоя сети — обычная
    операция, а не особый режим); `status: failed-upgrade` — так же автоматически повторяется операция, которая
    сорвалась (хук `upgrade`, а без него — `install` с `CREAM_PREV_VERSION`);
  - `--reinstall` — хук `install` перезапускается всегда.

  Хук обязан быть идемпотентным (§6.3), поэтому автоматический повтор безопасен.
- **upgrade.** Если версия ниже новой, прежнее дерево переносится в `.<ext>.prev`, новое ставится на место, вызывается
  хук `upgrade`. При успехе `.prev` удаляется. При сбое реестр получает `status: failed-upgrade` и прежнюю версию,
  `.prev` остаётся. Отката нет: его заменяет `cream install --reinstall` нужного пина.
  Понижение версии разрешено только с `--allow-downgrade`. Та же версия с другим отпечатком (пересборка без подъёма
  версии) отклоняется, но в каталоге это ловит CI (§3).
- **remove.** Отказ, если от расширения зависят установленные (`depends` или единственный поставщик `requires`);
  `--force` снимает отказ. Затем:
  1. хук `remove`;
  2. снятие configure и drop-in;
  3. удаление `files`/`bin` (если файл изменён, он сохраняется как `<dst>.cream-modified` с предупреждением);
  4. удаление дерева и реестра.

  `/etc/cream/<ext>` и `/var/lib/cream/ext/<ext>` сохраняются; `--purge` удаляет и их. `--forget` удаляет только
  реестр, дерево и артефакты cream без хука: это выход для расширений без `remove`.

## 7. Действия расширения
### 7.1 Объявление
```yaml
actions:
  <имя>:                      # [a-z0-9][a-z0-9-]{0,63}; файл actions/<имя>
    summary: "…"
    args: none | pairs | free # none — без аргументов; pairs — KEY=VALUE с проверкой по params; free — как есть
    params: {KEY: {type?, required?, secret?}}   # для pairs; тип по умолчанию — из config.keys
    stdin: none | secret | pass                   # secret — значение KEY=- уходит на stdin действия
    lock: exclusive | shared | none               # умолчание exclusive
    output: json | text                           # умолчание text
    timeout: 2m                                   # умолчание 2m
    phases: [live]                                # где допустимо; умолчание [live]
    root: true                                    # умолчание true
```

### 7.2 Вызов и вывод
`cream run [флаги cream] <ext> <action> [args…]`. Флаги cream (`--json`, `--lock-timeout`, `-q`) стоят **до**
`<ext>`: всё после имени действия — аргументы действия, перестановка флагов §11.1 на них не распространяется, и
`--json` после имени действия уходит действию как обычный аргумент. Аргументы `pairs` проверяются по типам до запуска,
`free` передаются argv как есть; shell не участвует. stdin действия — `/dev/null` при `stdin: none`, секрет при
`stdin: secret`, поток cream как есть при `stdin: pass`. stdout и stderr действия идут насквозь (stderr — через
фильтр секретов §6.3); собственные сообщения cream — только в stderr.

Секретный параметр допускается **только** как `KEY=-`. Значение читается со stdin cream и отдаётся на stdin действия.
Пустой stdin — отказ. Секретное значение в argv отклоняется кодом 64 с подсказкой (R3).

Окружение — как у хука (§6.3), плюс `CREAM_ACTION`, `CREAM_OUTPUT=json|text`.

При `output: json` действие обязано напечатать в stdout **ровно один JSON-объект**, а журнал — в stderr. cream
проверяет разбор. С `--json` cream оборачивает результат в общий конверт §11.4 (`command: "run"`):
`result` = `{"action": "<ext>/<action>", "exit_code": N, "output": <объект действия или null>}`, `ok` = `exit_code == 0`.
Без `--json` stdout пропускается как есть.

**Коды возврата.** `cream run` возвращает код действия как есть, если действие запустилось. Действиям отведён диапазон
`0–63`, и их смысл документирует расширение. Например, `set-managed`: 0 — применено или без изменений,
1 — restart не подтверждён (маркер остаётся), 2 — неверный ввод. Код вне диапазона и невалидный JSON
при `output: json` дают код 76. Ошибки до запуска (нет расширения, действия, блокировки) — коды cream 64–79 (§11.3).

`cream actions <ext> [--json]` перечисляет действия с описанием, параметрами и фазами. Так `stvor-agent` при старте
проверяет, что нужные ему действия есть (контракт `depends: teleport >=4.0.0` дублируется проверкой во время работы).

### 7.3 Что это даёт Stvor и stvor-agent (без Stvor-части)
- регистрация: `printf %s "$SECRET" | cream run teleport join TELEPORT_PROXY=… TELEPORT_REGISTRATION_SECRET=- …`;
- метки: `cream run --lock-timeout 20s teleport set-managed env=prod space=sp-1` и
  `cream run teleport managed-status` (оба `output: json`: stdout — ровно объект действия, без конверта; конверт
  `--json` — для людей и скриптов, которым нужны и коды cream). Ответ — схема `managed-status` расширения `teleport`
  (спека stvor-agent §5.7). Метка версии агента — не управляемая, а динамическая (там же, §4.4);
- restart с подтверждением, маркер pending и блокировка живут внутри расширения `teleport` (вариант 1 описи агента);
- юниты расширения тоже вызывают действия через cream, ради общей блокировки. Например, таймер повтора регистрации:
  `ExecStart=/usr/bin/cream run teleport join-retry`.

Правила для авторов действий:
- синхронный `systemctl restart` допустим только в действиях с `phases: [live]`. Звать их можно только из юнитов,
  упорядоченных после цели restart. Это требование к вызывающей стороне, cream его не проверяет;
- **взаимная блокировка через systemd.** Действие, которое вызывается из `Exec*` юнита, способного запуститься или
  перезапуститься, пока блокировку домена держит другой процесс, объявляется `lock: none`. Пример: `set-managed`
  держит `lock/teleport` и синхронно делает `systemctl restart teleport.service`; `ExecStartPre=/usr/bin/cream run
  teleport render` ждал бы ту же блокировку, а `set-managed` — конца restart: тупик до таймаута. Поэтому `render`
  (`ExecStartPre`, `ExecReload` `teleport.service`) и `reload-apps` (`ExecStart` `teleport-apps.service`) —
  `lock: none`. Наследование `CREAM_LOCK_FD` (§6.3) здесь не помогает: юнит — не потомок владельца блокировки;
- действие с `lock: none` только читает домен (файлы cream пишет атомарно через `rename`, поэтому чтение
  согласовано по файлу); писать домен оно не может (§6.3);
- `cream validate` предупреждает, если `ExecStartPre`/`ExecStart`/`ExecReload` юнита из `files` вызывает
  `cream run <ext> <action>` с `lock`, отличным от `none`.

## 8. Конфигурация
### 8.1 Файлы
Домен расширения — `/etc/cream/<ext>/config` (0644) и `/etc/cream/<ext>/secrets` (0600, только при ключах
`secret: file`). Формат — `KEY=VALUE` в грамматике `EnvironmentFile` systemd: порт `parse_env_file_internal()`, без
`source`.

Запись атомарная: временный файл в том же каталоге, затем `fsync` и `rename`. Комментарии и порядок строк
сохраняются. Юнит подключает оба файла: `EnvironmentFile=/etc/cream/<ext>/config` и
`EnvironmentFile=-/etc/cream/<ext>/secrets`.

Разделение на два файла заменяет правило «весь файл 0600, если есть секрет». Несекретные настройки становятся
читаемыми без root, например для `cream show` от пользователя.

### 8.2 Описание ключа (`config.keys`)
| Поле | Значение |
|---|---|
| `type` | `bool`, `int`, `port`, `host`, `path`, `str` (без управляющих знаков, `$` и обратных кавычек), `enum:a,b`, `re:<ERE>` (RE2 Go; синтаксис ERE за пределами RE2 запрещён валидатором), `list:<тип>` (через пробел) |
| `default` | строка; пустое значение допускается только при `empty: true` |
| `empty` | `true` — пустое значение допустимо (бывший суффикс `?`) |
| `secret` | `file` — хранится в `secrets`, маскируется; `hook` — не хранится, значение уходит хуку `secret` (бывший `secret:hook`) |
| `readonly` | `cream set` отказывает; писать могут хук, действие или владелец через `--allow-readonly` (бывший `ro:`) |
| `description` | обязательно |
| `pattern` | `true` для ключей вида `PREFIX_*`; конкретные ключи проверяются по типу шаблона |
| `scope` | `config` (умолчание) или `install`. `install` — параметр установки (версия и сумма бинаря, зеркало): `cream set` такого ключа отказывает (65, `config.install_scope`) с подсказкой `cream install <ext> KEY=…`; `cream install` с новым значением перезапускает хук `install` (§6.5). `--reinstall` у `set` нет: переустановка — операция `install` |

### 8.3 Команды
- `cream list [--json]` — установленные расширения: версия, статус, наличие домена, `apply`, `secret`, действий.
  Заменяет `bisquite-conf list`.
- `cream show <ext> [--json]` — ключ, значение, откуда (`default|file`), тип. Секреты показываются как `***`
  (или `(задан)`/`(пусто)`). Битые значения в файле помечаются `invalid`.
- `cream get <ext> KEY` — значение (секрет — только с `--secret` и только root).
- `cream set [--apply|--no-apply] [--wait] <ext> KEY=VALUE… [KEY=-] [--params-file F]`:
  - все ключи проверяются до записи;
  - запись идёт одной транзакцией домена;
  - `KEY=-` читает значение со stdin, и такой ключ может быть только один;
  - несколько секретов передаются через `--params-file` (файл в формате env, 0600, владелец root, иначе отказ 77);
  - секретный ключ со значением в argv — отказ 64;
  - `readonly`-ключ — отказ 65, если нет `--allow-readonly`;
  - ключ `scope: install` — отказ 65 (§8.2), `--allow-readonly` его не снимает.

Для скриптов есть `cream config export <ext> --shell`: безопасное цитирование `KEY='…'`, без секретов. Хукам он
обычно не нужен, потому что ключи уже в окружении.

### 8.4 Применение
После записи изменённых ключей:
- `--no-apply` — ничего;
- иначе, если есть `hooks/apply`, он вызывается синхронно под блокировкой домена с `CREAM_CHANGED_KEYS`;
- иначе, если есть `configure` и systemd работает, выполняется `systemctl --no-block restart cream-configure@<ext>`
  (`CREAM_REASON=apply` передаётся маркером `/run/cream/<ext>.reason`);
- иначе (build, нет systemd) — ничего: значения подхватит configure на загрузке.

Если systemd запущен, `--wait` делает restart синхронным. В стадии загрузки `--wait` переходит в `--no-block`
с предупреждением: так исключается взаимная блокировка заданий systemd. Это тот же урок, что «хук обязан быть
неблокирующим».

Хук `apply` обязан не ждать заданий systemd. Валидатор каталога ищет `systemctl` без `--no-block`, как сейчас.
Если хук отказал, запись уже сделана, а код — 79.

По умолчанию применение — это перезапуск configure. Этого достаточно для 7 из 11 нынешних `knobs.apply`
(`chromium-kiosk`, `code-server`, `kiosk`, `rustdesk`, `vino-vnc`, `firewall` с переносом юнита в configure, `desktop`).
Собственный `apply` остаётся у `x11vnc`, `selkies`, `can`, `locale`, `sensing`.

### 8.5 Параметры install
`cream install <ref> KEY=VAL…` и `EXTENSION name KEY=VAL` записывают значения в домен до хука `install` по правилам
`set` и с теми же проверками (кроме запрета `scope: install` — здесь это их законное место). Это бывший
`conf_init --env`. Повторный `install` перезаписывает только **переданные** ключи и не трогает правки оператора;
изменился ли ключ `scope: install`, cream решает сравнением с записанным значением — от этого зависит, вызывается ли
хук (§6.5). Ключи, которых нет в схеме, дают отказ 65. Опечатка в VMFILE больше не уходит
молча в окружение.

## 9. Источники и резолвер
### 9.1 `sources.yaml`
```yaml
sources:
  - name: stvor                       # [a-z0-9][a-z0-9._-]{0,63}; каталог кеша
    type: archive
    url: https://portal.example/dist/cream/cream-extensions-4.0.0.tar.gz   # https:// или file://
    sha256: 9f2c…                     # обязательно
  - name: core
    type: git
    url: https://github.com/iamletenkov/cream-extensions.git
    ref: v4.0.0                       # ветка или тег
    commit: 1a2b3c…                   # пин; если задан, перевешивает ref
  - name: dev
    type: path
    path: /home/me/cream-extensions   # только абсолютный
```
Порядок задаёт приоритет. Поля сверяются с типом в обе стороны. `oci` в v1 нет. Команды:
`cream source add|remove|list|show|sync|pin`.
- `add` задаёт тип флагом: `cream source add <name> --archive <https://…|file:///…> --sha256 <hex>`,
  `--git <url> [--ref R] [--commit C]` или `--path <абсолютный путь>`. Голый путь вместо `file://` — отказ 64
  (форма одна, чтобы URL и путь не путались).
- `add` идемпотентен: те же поля — 0, другие — отказ без `--replace` (так Stvor поднимает пин каталога).
- `add` не синхронизирует: кеш появляется только после `cream source sync <name>` (§9.2). Флаг `add --sync`
  делает обе операции одной командой, и при сбое синхронизации запись источника не сохраняется.
- `pin` записывает наблюдаемый коммит в `commit`.

### 9.2 Синхронизация (единственное место сети, R6)
`cream source sync [name…]` собирает новый кеш в частном каталоге рядом и подменяет его одним `rename`, как сейчас
в bisquite.
- **git**: внешний `git` (зависимость только для git-источников, `Suggests: git`). Клон полный. Окружение без
  интерактива и хуков: `GIT_CONFIG_NOSYSTEM`, `core.hooksPath=/dev/null`. Порядок разрешения ref:
  `origin/<ref>` → тег → как написано. Кеш, который не принадлежит текущему пользователю или доступен группе,
  переклонируется. Креды в URL вырезаются из вывода.
- **archive**:
  - загрузка `net/http`: только `https://` (системные CA) или `file://`; редиректы допускаются только https→https,
    не больше 5; лимит 256 МиБ; таймауты;
  - sha256 сверяется **до** распаковки, несовпадение — код 69;
  - распаковка `tar.gz`: отказ на абсолютных путях, `..`, ссылках наружу, устройствах и FIFO, hardlink наружу;
  - верхний каталог архива снимается, если он один.
- **path**: копия (не ссылка), отказ на ссылках наружу, отпечаток дерева.

После синхронизации cream проверяет `cream-catalog.yaml` и все манифесты (§4.1) и пишет в `sources.lock`:
`{name, type, origin, resolved, synced_at, catalog_version}`. `resolved` — коммит, sha256 архива или отпечаток
дерева path. Битый каталог не подменяет прежний кеш.

### 9.3 Поиск
Ссылка — `[source/]name[@version]`. Без источника они просматриваются по порядку. Первый найденный побеждает,
затенение выводится предупреждением. `@version` — **утверждение**, а не выбор: если найдена другая версия, это
отказ 66. Решателя нет. Фильтры поиска: `family`, `arch`. «Не синхронизировано» и «нет в источниках» — разные
ошибки. Install, validate, resolve работают **только по кешу**, без сети (R6).

### 9.4 Проверки перед установкой (R2)
1. Манифест: схема, `cream:` против своей версии.
2. Платформа: `family`, `arch` (цель), `os`, `commands`.
3. Применимость: для фазы `build` — `targets.build ≠ none`; для `live` — `targets.live ≠ no`, а `confirm` требует
   `--accept-risk`.
4. Граф на множестве «установленное ∪ запрошенное»:
   - `depends`: имя есть и версия в диапазоне;
   - `requires`: у способности есть поставщик или она объявлена в `assumed.json`;
   - `conflicts.extensions` в обе стороны;
   - `conflicts.capabilities`: другой поставщик этой способности;
   - владение `files`/`bin`.
5. Порядок: топологическая сортировка по рёбрам `depends` и `requires`, где поставщик идёт раньше потребителя.
   Цикл — отказ 67.

Отсутствующая зависимость — отказ 67 со списком кандидатов из источников. `--with-deps` добавляет недостающие:
`depends` — по имени, `requires` — единственный поставщик в источниках. Если поставщиков несколько, это отказ
со списком (R2).

`cream assume add|remove|list CAP` объявляет способность, которую дало не cream, например голый `INSTALL xorg`
в VMFILE. Запись идёт в `assumed.json` и в lineage. Это открытая проблема описи §9.3.

### 9.5 Отпечаток дерева
Это sha256 потока отсортированных записей:
- `F <путь>\0<режим: 0644|0755>\0<sha256 содержимого>\n`;
- `L <путь>\0<цель>\n`;
- `D <путь>\n`.

Алгоритм тот же, что у `_library_digest` в bisquite, плюс бит исполнения. Отпечаток вычисляется для дерева
расширения (`ext_digest`) и для path-источника.

## 10. Состояние (R4)
Реестр — по файлу на расширение: `/var/lib/cream/installed/<ext>.json`. Запись атомарная (временный файл, `fsync`,
`rename`, `fsync` каталога) под `lock/global`. Файл на расширение не требует переписывать общий документ и
переживает обрыв посередине установки нескольких расширений.
```json
{
  "schema": "cream-state/v1",
  "name": "teleport", "version": "4.0.0", "kind": "extension",
  "status": "installed",                      // installed | failed | failed-upgrade | skipped
  "source": {"name": "stvor", "type": "archive", "origin": "https://…/cream-extensions-4.0.0.tar.gz",
             "resolved": "sha256:9f2c…", "catalog_version": "4.0.0"},
  "ext_digest": "sha256:…",
  "params_digest": "sha256:…",                 // sha256 отсортированных "KEY=VALUE\n" параметров install; вместо
                                               // значения секрета — литерал "<secret>" ("KEY=<secret>\n"):
                                               // в дайджест входит только факт наличия, хеш секрета нигде не хранится
  "phase": "live",                             // где поставлено впервые: build | live
  "installed_at": "2026-10-18T10:00:00Z", "updated_at": "…",
  "cream_version": "1.0.0",
  "files": [{"dst": "/etc/systemd/system/teleport.service", "sha256": "…"}],
  "bin": ["…"], "configure": {"stage": "system", "target": "multi-user.target"},
  "reboot_required": false
}
```
- `history.jsonl` — дописываемый журнал `{time, op, ext, from, to, status, phase}` без параметров. В FROM-цепочке
  образов он наследуется вместе с образом.
- cream читает реестр той же мажорной схемы. Более новая мажорная схема — отказ 74 «реестр новее исполнителя».
- `cream lineage --json` печатает все записи реестра в компактной форме (§12.4).

## 11. CLI
### 11.1 Библиотека разбора (R9)
Разбор — stdlib `flag`: по `flag.FlagSet` на подкоманду и диспетчер подкоманд (~200 строк). Сверху — своя
перестановка аргументов, чтобы флаги принимались и после позиционных (`cream set teleport K=V --apply`); `--`
прекращает разбор.

cobra отвергнута (§18): она тянет `pflag`, генерацию автодополнения и стиль справки, которые здесь не нужны, а
каждая зависимость — это аудит и Renovate.

Внешние зависимости модуля:
- `go.yaml.in/yaml/v3` — поддерживаемый форк `gopkg.in/yaml.v3`; разбор манифестов идёт строго, с `KnownFields`;
- больше ничего: semver-диапазоны — свой код (~150 строк), `flock` — `syscall.Flock`, tar/gzip/http/sha256 — stdlib.

### 11.2 Команды
| Команда | Назначение | root |
|---|---|---|
| `cream version [--json]` | версия, контракт `cream/v1`, сборка | нет |
| `cream source add\|remove\|list\|show\|sync\|pin` | источники (§9) | для `/etc` — да |
| `cream search [QUERY]`, `cream info <ref>` | поиск по кешу, манифест | нет |
| `cream validate [--catalog DIR] [DIR…]` | проверка манифестов и графа каталога (замена `validate-extensions.py`) | нет |
| `cream resolve [--phase build\|live] [--target-arch A] [--host-arch A] [--family F] <ref[ K=V…]>…` | офлайн-разрешение для bisquite: порядок, пути в кеше, отпечатки, режим build, ошибки | нет |
| `cream catalog pack DIR --out F` | детерминированный архив каталога: сортировка, mtime = `SOURCE_DATE_EPOCH`, uid/gid 0, `gzip -n` | нет |
| `cream install [--phase P] [--source S] [--with-deps] [--reinstall] [--allow-downgrade] [--accept-risk] [--allow-skip] [--params-file F [--remove-params-file]] [--from-dir D --record F] <ref> [K=V…]` | установка; несколько ссылок — только без параметров | да |
| `cream upgrade [<ext>…\|--all]` | обновление до версии в кеше | да |
| `cream remove [--purge\|--forget] [--force] <ext>` | удаление | да |
| `cream list`, `cream show`, `cream get`, `cream set` | настройки (§8.3) | `set` — да |
| `cream config export <ext> --shell` | для скриптов | нет |
| `cream configure <ext> [--wait]` / `--from-unit <ext>` | запустить configure (второе — только из юнита) | да |
| `cream run <ext> <action> [args]`, `cream actions <ext>` | действия (§7) | по `root` действия |
| `cream check [<ext>]`, `cream status [<ext>]` | хук check; статус = реестр + check + состояние юнита configure | нет* |
| `cream verify [<ext>]` | сверка `files`, `bin` и дерева с реестром | нет |
| `cream assume add\|remove\|list CAP` | §9.4 | да |
| `cream path <ext>` | путь дерева | нет |
| `cream lineage` | описание применённого для меток (§12.4) | нет |
| `cream system init` | каталоги, шаблон юнита из встроенной копии (`go:embed`), пересчёт целей configure | да |

\* Хук `check` запускается от root; без root `status` показывает только реестр.

Глобальные флаги: `--json`, `--root DIR`, `--sources FILE`, `--cache DIR`, `--lock-timeout D`, `-q`, `-v`.
Окружение: `CREAM_ROOT`, `CREAM_SOURCES`, `CREAM_CACHE`.

### 11.3 Коды выхода
| Код | Смысл | Строковый `error.code` (примеры) |
|---|---|---|
| 0 | успех; `changed`, `reboot_required` — в JSON | |
| 1 | `check`/`status`: не применено или нездорово | `check.failed` |
| 0–63 | `cream run`: код действия (§7.2) | |
| 64 | неверный вызов: флаги, аргументы, секрет в argv | `usage.*`, `secret.in_argv` |
| 65 | неверные данные: манифест, значение ключа, readonly | `manifest.*`, `config.invalid`, `config.readonly` |
| 66 | не найдено: расширение, источник, ключ, действие; `@version` не совпала | `not_found.*`, `version.mismatch` |
| 67 | граф: зависимость, конфликт, цикл, владение файлом, есть зависимые | `deps.*`, `conflict.*` |
| 68 | неприменимо: arch, family, os, команды, `targets`, версия cream, хук вернул 20 | `platform.*`, `target.*` |
| 69 | источник: не синхронизирован, пин не совпал, сеть при `sync` | `source.*` |
| 70 | внутренняя ошибка cream | `internal` |
| 74 | ввод-вывод, реестр, реестр новее исполнителя | `state.*` |
| 75 | блокировка занята или таймаут | `lock.timeout` |
| 76 | действие нарушило протокол (код > 63, невалидный JSON) | `action.protocol` |
| 77 | нужен root, или небезопасные права файла (params, cache, sources) | `perm.*` |
| 79 | хук завершился сбоем; код хука — в `error.details.hook_exit` | `hook.failed` |

### 11.4 `--json`
С `--json` stdout содержит ровно один объект. Весь человекочитаемый вывод и вывод хуков идут в stderr.
```json
{"cream": "1.0.0", "command": "install", "ok": true,
 "result": {...},                     // своя форма у каждой команды; описана в docs/cli.md и закреплена golden-тестами
 "warnings": [{"code": "source.shadowed", "message": "…"}],
 "error": null}                       // при ok=false: {"code","message","details"}
```
Формы `result` версионируются вместе с контрактом. Поля добавляются в минорных версиях, удаляются только в мажорных.
Для `install`/`upgrade` `result` — запись реестра (§10) плюс `changed`.

## 12. Фаза build и интеграция с bisquite
### 12.1 Что делает bisquite и что cream
| Шаг | bisquite | cream |
|---|---|---|
| Источники | держит свой `sources.yaml` (умолчание — архив каталога по пину), зовёт `cream source sync --sources … --cache <DATA_DIR>/cream` (`bs extension sync` — обёртка) | синхронизация, `sources.lock` |
| Разбор VMFILE | парсит `EXTENSION [src/]name[@version] K=V…` | — |
| Проверка (`bs image validate`) | вызывает `cream resolve --phase build --target-arch <LABEL arch> --host-arch <хост> --family <virt-inspector> --json` по **всем** EXTENSION-слоям в порядке VMFILE; ошибки — громко, `--strict` больше не нужен | порядок, граф внутри VMFILE, барьер архитектуры для `exec` (чужая архитектура — отказ 68), отпечатки, `params_digest`; секретные параметры помечены |
| Слой `exec` | один вызов `virt-customize` на слой (§12.2) | в госте: `cream install --phase build …` |
| Lineage | метки из ответа `resolve` (§12.4) | — |
| Бинарь для гостя | берёт `/usr/lib/cream/guest/linux-<target>/cream` из пакета cream на хосте | — |

Код bisquite, который уходит: `extension_manifest.py`, `infrastructure/extensions/{catalog,repository,sync}.py`,
`config/extensions.py`, `config/sources_lock.py`, применение в `linux.py` (`_apply_extension_layer`,
`_apply_staged_extension`, `_library_digest`, `_stage_tree` — сборка staging-архива слоя пишется заново под §12.2) и соответствующие тесты. Остаются парсер VMFILE,
вызов процесса cream и обработка его JSON. Пакет bisquite объявляет зависимость `cream (>= X.Y)` и проверяет версию
при старте сборки.

### 12.2 Слой `exec` в одном вызове `virt-customize`
1. `--copy-in <guest-cream>:/usr/bin` и `--chmod 0755:/usr/bin/cream`. Копия делается всегда: версия в образе
   совпадает с хостом. Реестр совместим внутри мажора.
2. `--run-command '/usr/bin/cream system init'`.
3. `--tar-in <staging>/<id>.tar:/var/lib/cream/incoming/<id>`. Архив bisquite собирает на хосте
   `tar --owner=0 --group=0 --numeric-owner` (режимы сохраняются, ссылки — как ссылки): `--copy-in` переносит в гостя
   uid/gid пользователя хоста, и проверка «владелец root» (§13) отвергла бы `params.env`. Внутри — дерево расширения
   из кеша, `record.json` (источник и отпечатки из `resolve`) и `params.env`. `params.env` — все параметры слоя, 0600;
   staging и архив на хосте — 0700/0600, архив удаляется после вызова.
4. `--run-command` с потоком на консоль, как сейчас (`_stream_to_console`):
   `cream install --phase build --from-dir /var/lib/cream/incoming/<id>/<name> --record …/record.json --params-file …/params.env --remove-params-file`.
5. cream в госте:
   - заново проверяет манифест и отпечаток против `record.json`;
   - проверяет `platform` и граф против **реестра гостя**, так что зависимости, унаследованные через FROM, учитываются;
   - ставит `/usr/sbin/policy-rc.d` (exit 101) на время хука, если его не было, и убирает после;
   - выполняет хук, включает configure ссылками, пишет реестр, удаляет `incoming/<id>`.

Параметры больше **не идут в argv** `virt-customize` и не попадают в текст `BuildError`/`RuntimeError`. bisquite
печатает команду с `params.env` вместо значений (R3).

Сеть: cream в госте в сеть не ходит. Хуки ходят (apt, curl) через сеть appliance, как сейчас.

Отдельный открытый вопрос — литеральные секреты в самой строке VMFILE, которая едет в образ (§19, В2).

### 12.3 Чужая архитектура цели
В v1 слой `exec` собирается только на хосте той же архитектуры, что цель: `resolve` отвечает отказом 68
(`platform.arch_barrier`) и называет слой. bisquite не пытается обойти это qemu-user. Раскладка без запуска гостя
(`build: files`, `cream stage --root`) — кандидат будущей минорной версии (§19, В4); поле `targets.build` уже
резервирует значение `files`, так что манифесты v1 не придётся менять, кроме подъёма `cream:` у тех, кто его возьмёт.

### 12.4 Lineage (R8)
`cream resolve --json` возвращает на каждый слой:
```json
{"name": "x11vnc", "version": "4.0.0", "source": "core", "source_resolved": "git:1a2b3c4d…",
 "catalog_version": "4.0.0", "ext_digest": "sha256:…", "params_digest": "sha256:…",
 "deps": ["desktop@4.0.0"], "mode": "exec", "token": "core/x11vnc@4.0.0#1a2b3c4d"}
```
- `token` — каноническая короткая строка для меток: `<source>/<name>@<version>#<rev12>`, где `rev12` — 12 hex
  коммита или sha256 архива.
- bisquite кладёт в `bisquite.lineage.extensions` список токенов, а полный JSON — в отдельную аннотацию OCI.
  Имена меток и аннотаций решает спека bisquite.
- Унаследованное через FROM берётся, как сейчас, из меток базы. Сверку можно сделать по `cream lineage` образа.
- Дайджест слоя `EXTENSION` в bisquite становится sha256 от `token + params_digest`, а не строки VMFILE: смена
  коммита источника меняет `chain_digest`.

## 13. Безопасность
- **root.** cream выполняет хуки и действия от root: это исполнение произвольного кода из источника. Граница доверия —
  источник и его пин. Отсюда правила:
  - `install`/`resolve` не ходят в сеть;
  - `sync` — только явно;
  - архив без sha256 не принимается;
  - git-источник без `commit` разрешён, но `cream source list` и `resolve --json` помечают его как незакреплённый
    (`pinned: false`), а `--require-pinned` (рекомендуется bisquite и Stvor) делает это отказом 69.
- **Права файлов.** cream отказывает (77), если `sources.yaml`, `params-file`, кеш или `/etc/cream/<ext>` доступны на
  запись не-root или группе (при работе от root). Дерево в `/opt/cream/ext` и кеш принадлежат root. Копирование —
  без следования ссылкам. Распаковка архива — с защитой от обхода путей (§9.2).
- **Секреты (R3):**
  - никогда не попадают в argv, ни у cream, ни у хука (хук `secret` получает значение по stdin);
  - не попадают в окружение хуков;
  - не попадают в реестр (там только факт наличия в `params_digest`), в `history.jsonl`, в `--json` (маска), в lineage;
  - вывод хуков фильтруется по известным значениям секретов;
  - `--params-file` принимается только с правами 0600 и владельцем root.

  Остаточный риск, который cream не закрывает: хук сам передаёт секрет в argv сторонней программы (`x11vnc
  -storepasswd`). Валидатор каталога предупреждает о таких местах по списку известных команд.
- **Подписи.** В v1 только sha256-пины, доставленные доверенным каналом: `.env` Stvor, пин bisquite в git, PR-бампы.
  Подпись архива и тегов — открытый вопрос §19, В1. Формат источника резервирует поля `signature` и `signers`
  как `x-`-расширение, чтобы их можно было добавить без новой схемы.
- **Цепочка поставки cream.** Сборка воспроизводима: `CGO_ENABLED=0 -trimpath -buildvcs=false`, фиксированный
  `SOURCE_DATE_EPOCH`, детерминированный `.deb`. Кроме того: `govulncheck` в CI; одна внешняя зависимость;
  `SHA256SUMS` в релизе.

## 14. Тестирование и CI (R11)
- **Unit, table-driven.** Покрывают:
  - разбор манифеста (каждое правило §4.2 — строка таблицы, включая отказы);
  - semver-диапазоны;
  - граф (топосортировка, циклы, оба вида конфликтов, `--with-deps`, неоднозначный поставщик);
  - типы ключей;
  - разбор `EnvironmentFile`, сверяемый с эталонными выводами systemd 249, 252, 255, 257 (фикстуры сняты один раз
    `systemd-run`/`systemd-analyze` в контейнерах и лежат в `testdata`; расхождение 249/257 на `\` закрепляется
    тестом);
  - атомарная запись (инъекция сбоя между `write` и `rename`);
  - отпечаток дерева;
  - распаковка враждебных архивов;
  - фильтр секретов;
  - коды выхода;
  - golden-формы `--json`.

  Всё это запускается без root через `--root`.
- **Фикстуры.** Каталог `testdata/catalog/` в репозитории cream: `noop`, `with-config` (все типы),
  `secret-hook`, `apply-hook`, `configure-stages` (по расширению на стадию), `action-json`, `action-bad-protocol`,
  `depends-lib` + `lib-fixture`, `lock-restart` (§7.3), `install-scope` (`scope: install`), `conflict-name`, `conflict-cap`, `requires-cap`, `files-mode`, `failing-install`,
  `reboot-required`, `not-applicable`, `upgrade-v1`/`upgrade-v2`, `live-confirm`.
- **Интеграция (живая машина).** Контейнеры `debian:12`, `debian:13`, `ubuntu:22.04`, `ubuntu:24.04` с systemd
  как PID 1 (`--privileged`, cgroupns хоста или podman). Сценарий:
  1. `dpkg -i` собранного `.deb`;
  2. `source add` архива фикстур по `file://` с sha256;
  3. install, set, apply, run, upgrade, remove;
  4. перезапуск контейнера, чтобы проверить configure на загрузке и стадии;
  5. проверка, что секретов нет в `ps`, журнале, реестре и `history`;
  6. двойной install — `changed: false`; install после `failed` — хук вызван повторно; install с новым значением
     ключа `scope: install` — хук вызван, `cream set` того же ключа — отказ 65;
  7. **взаимная блокировка через systemd** (фикстура `lock-restart`: юнит с `ExecStartPre=/usr/bin/cream run
     lock-restart render` и действие `set-managed` с `lock: exclusive`, которое синхронно делает `systemctl restart`
     этого юнита): `cream run lock-restart set-managed` завершается за время restart, а не по `--lock-timeout`; та же
     фикстура с `render: {lock: exclusive}` падает по таймауту — тест закрепляет, что правило §7.3 нужно.

  В CI на каждый PR: amd64 — полная матрица; arm64 — `debian:13` и `ubuntu:24.04` на arm-раннере GitHub.
- **Build-фаза.** `make test-build` на фикстурном qcow2: Debian genericcloud, кешируется по sha256. Проверяется
  `virt-customize` со слоем `exec`, доставка `--tar-in` с владельцем root (§12.2), реестр, `policy-rc.d`, отсутствие параметров в argv (`ps` во время сборки
  и текст ошибки). Запуск ручной и ночной, не в каждом PR.
- **cream-extensions.** `tools/check.sh` = `cream validate --catalog .` (пин версии cream) + shellcheck + нынешние
  bash-тесты хуков на подменённом корне (`CREAM_ROOT`). Плюс проверка «отпечаток изменился, а версия нет» (§3).
  Ночной живой прогон выбранных расширений `targets.live: yes` в контейнерах (сеть нужна) — отдельный workflow.
- **bisquite.** Unit-тесты разбора JSON cream на фикстурах ответов. Сборка примеров с расширениями остаётся ручной,
  как сейчас.

## 15. Упаковка и выпуск
- **Сборка.** `go build` в матрице `GOARCH=amd64|arm64`; Makefile — `make build`, `make deb`, `make test`,
  `make test-integration`, `make test-build`.
- **`.deb` — nfpm** (CLI с пином версии, конфиг `packaging/nfpm.yaml`). Состав:
  - `/usr/bin/cream`;
  - `/usr/lib/systemd/system/cream-configure@.service`;
  - `/usr/lib/cream/guest/linux-{amd64,arm64}/cream` — оба бинаря, чтобы хост сборки офлайн имел гостевой cream той
    же версии; это +~10 МБ, остальное — в §18;
  - `/usr/share/doc/cream/{copyright,changelog.gz}`.

  Метаданные пакета:
  - `Architecture: amd64|arm64`;
  - `Depends:` нет, бинарь статический;
  - `Suggests: git, libguestfs-tools`.

  Сценарии сопровождающего:
  - `postinst` делает `cream system init` и `daemon-reload`;
  - `prerm` при установленных расширениях только предупреждает;
  - `postrm purge` удаляет `/var/cache/cream`. `/etc/cream` и `/var/lib/cream` он удаляет, только если реестр пуст.
- **Воспроизводимость.** `SOURCE_DATE_EPOCH` = время коммита тега, `-trimpath -buildvcs=false`, mtime в nfpm.
  CI собирает дважды и сравнивает sha256.
- **goreleaser не берём.** Релиз создаёт release-please, а сборку и загрузку делают `gh release upload` и nfpm.
  Меньше движущихся частей, и схема та же, что у агента (§18).
- **Артефакты релиза:** `cream_<v>_amd64.deb`, `cream_<v>_arm64.deb`, `cream_<v>_linux_{amd64,arm64}.tar.gz`
  (бинарь, лицензия, шаблон юнита), `SHA256SUMS`.

## 16. План миграции (крупно; каждый шаг — свой план)
1. **cream MVP** — состав и матрица в §17. Выход: релиз `v1.0.0`; проверка `make test-build` — в конце шага.
2. **Пилот в `cream-extensions`** (ветка, без релиза каталога):
   - `cloud-user` (`kind: library`, §4.5): от неё зависит `docker` (`configure.sh` берёт имя пользователя из
     `get_cloud_user.sh`), без неё пилот `docker` не собирается;
   - `docker`: `depends: [{name: cloud-user}]`, configure стадии `system` (`after: [docker.service]`) вместо
     самописного юнита; `live: confirm`;
   - `teleport`: перенос CLI `bisquite-teleport` в хуки и действия `join`, `join-retry`, `leave`, `status`, `render`,
     `reload-apps`, `set-managed`, `managed-status` (§4.3); `os-logins` и командные метки — файлы расширения, которые
     зовёт сам Teleport (`ssh_service.commands`); юниты через `files`;
   - обёртка `stvor-agent`: `depends: [{name: teleport, version: ">=4.0.0 <5"}]`, пины `.deb` по arch, drop-in
     командной метки `stvor_agent` (спека stvor-agent §11);
   - `cream validate` в CI, `cream catalog pack`.

   Приёмка: живой install в контейнерах и на тестовой ВМ; бутстрап как в R7 с `file://`-архивом.
   Разблокирует спеку Stvor «stvor-agent».
3. **Весь каталог** (мажор каталога 4.0.0):
   - конвертер §4.7;
   - `lib/` → `desktop`, `cloud-user`, `l4t` (§4.5);
   - `knobs` → `config.keys`;
   - самописные `configure-*.service` → стадии;
   - пути `/opt|etc|var/lib/bisquite` → cream, переименование nft-таблицы и цепочек firewall
     (`inet cream`, `CREAM-INPUT`);
   - `targets.live` по таблице описи;
   - удаление `validate-extensions.py`, `test-conf.sh` (переехал в cream) и `agent-bump` для teleport;
   - `agent-bump` нацеливается на `stvor-agent`.

   Судьба `extensions/openwrt/` — вопрос §19, В3.
4. **bisquite**:
   - `EXTENSION` через cream (§12);
   - подмодуль → архивный источник по пину; `extensions-bump` поднимает пин архива;
   - удаление резолвера, синхронизации и применения;
   - метки lineage с версией и ревизией;
   - правка 32 примеров VMFILE (`teleport-agent` → `teleport` + `stvor-agent`), документации, `make test-build`
     на примере.

Шаги 2 и 4 частично параллельны после релиза cream 1.0.0. Шаг 3 блокирует релиз bisquite с cream.

## 17. MVP (первый план реализации)
Всё, что в этой спеке не попало в «входит», поле или флаг **резервирует**: манифест с таким полем отвергается
понятной ошибкой «зарезервировано, нужен cream >= X» (65/68), а не «неизвестное поле». Возвращается каждое отложенное
минорной версией cream с подъёмом `cream:` у манифестов, которые его используют (§4.2).

**Входит в `v1.0.0`:**
- формат `cream/v1` целиком в части полей ниже, `cream validate`, `cream catalog pack`;
- источники `archive` и `path`, `cream source add|remove|list|show|sync` (`add --sync`, `--replace`);
- резолвер и граф: `depends`, `requires`, `provides`, оба вида `conflicts`, топосортировка, `cream resolve --json`;
- `install`/`upgrade`/`remove` (`--purge`, `--forget`, `--force`) live и `install --phase build --from-dir --record
  --params-file --remove-params-file`, `policy-rc.d`; повтор install по §6.5, `scope: install`;
- `kind: library` (`cloud-user` — в пилоте), `files`;
- хуки `install`, `upgrade`, `remove`, `apply`, `configure` со стадией `system`; шаблон `cream-configure@`,
  `cream configure`, `cream system init`;
- `cream set|show|get|list`, `cream config get|export`, типы ключей §8.2, `secret: file`, `readonly`, `scope`,
  `--allow-readonly`, `KEY=-`, `--params-file`;
- действия: `cream run`, `cream actions`, `args`, `params` (в том числе `secret: true`), `stdin`, `lock`
  (`exclusive|shared|none`), `output`, `timeout`, `phases`;
- реестр `cream-state/v1`, `history.jsonl`, `--json`, коды §11.3, блокировки и `CREAM_LOCK_FD`;
- `.deb` по архитектурам, воспроизводимая сборка, unit-тесты, фикстуры §14, интеграция в контейнерах.

**Откладывается** (поле или команда резервируется, позже — минорная версия с подъёмом `cream:`):

| Что | Почему можно позже |
|---|---|
| git-источник (`type: git`, `pin`, `--require-pinned` для git) | роботам и bisquite хватает архива по пину; разработчику — `path` |
| `targets.build: files`, `cream stage` | §4.4, §12.3, §19 В4 |
| `bin` | пилоту ссылки в `PATH` не нужны; действия зовутся через `cream run` |
| `cream assume` | голый `INSTALL xorg` в пилоте не встречается |
| `install --with-deps` | Stvor и bisquite перечисляют расширения явно в порядке зависимостей |
| `cream verify` | реестр с sha256 файлов пишется с первого дня, сверку можно добавить без миграции |
| команда `cream lineage` | lineage для bisquite отдаёт `resolve --json` (§12.4) |
| `cream search`, `cream info` | поиск человеком — удобство, не контракт |
| хуки `secret`, `check` и команды `cream check`/`cream status` | секреты пилота идут через stdin действий (`join`) или в `secret: file`; здоровье — действие `status` |
| стадии `early`, `cloud-user`, `graphical` | пилоту (`docker`, `teleport`, `stvor-agent`) хватает `system`; firewall и рабочие столы — шаг 3 §16 |
| `--allow-downgrade` | откат — `remove` + `install` нужного пина |

**Матрица MVP.** На каждый PR: `debian:12` и `ubuntu:24.04` на amd64, `debian:13` на arm64 (контейнеры с systemd,
сценарий §14), плюс ручной `make test-build` перед релизом. Полная матрица §14 (`debian:13`/`ubuntu:22.04` amd64,
`ubuntu:24.04` arm64) включается после MVP. `platform.os` расширений при этом не сужается: `ubuntu >= 22.04` у
`teleport` остаётся объявлением, которое проверяет живой прогон каталога, а не CI cream. Jetson/L4T
(`ubuntu:22.04` arm64) вне MVP.

**Сверка с потребителем.** Спека stvor-agent (§12, C1–C14) опирается только на «входит»: действия со `stdin: secret`
и `lock: none`, `depends` по имени, `files`, `scope: install`, повтор install по §6.5, архивный источник с
`file://` для офлайна. Отложенное ей не нужно.

## 18. Отвергнутые варианты
| Вариант | Почему нет |
|---|---|
| Оставить `bisquite-conf` bash-библиотекой, а cream сделать обёрткой | два разборщика `EnvironmentFile`, третья копия в пакетах; решение владельца — `cream set/show` |
| cobra/pflag для CLI | лишние зависимости ради автодополнения; stdlib `flag` + перестановка аргументов закрывают нужное |
| go-git вместо внешнего `git` | десятки транзитивных зависимостей; git нужен только git-источникам, на роботах источник — архив |
| Принимать layout 2 параллельно с cream/v1 | переходной совместимости не нужно (решение владельца); два формата — два набора багов |
| Самописные `configure-<ext>.service` у каждого расширения (как сейчас) | 19 копий шаблона, уже был цикл с `cloud-final`, нет снятия при remove; стадии cream кодируют урок один раз |
| Один общий юнит configure для всех расширений | стадии разные (firewall до сети, рабочий стол после DM); сбой одного блокировал бы остальных |
| configure «один раз» с маркером | фактическая и проверенная семантика каталога — «на каждой загрузке» с идемпотентными скриптами; one-shot потерял бы переприменение настроек после правки файла вне cream |
| `lib/` по sha256 (как сейчас) | копии копятся в FROM-цепочке и не чистятся; неявное владение ссылками; заменено библиотеками-расширениями с версией |
| Конфликты только по именам или только по способностям | первое не выражает «любой другой рабочий стол», второе не выражает точечную несовместимость; держим оба явно |
| Решатель версий, несколько версий в источнике | сложность без потребителя; версию фиксирует пин источника, `@version` — утверждение |
| `oci`-источник | раскладка артефакта не выбрана; архив по URL+sha256 закрывает офлайн и зеркало Stvor |
| Единый `state.json` или SQLite | общий документ переписывается при каждой операции; SQLite — cgo или крупная зависимость; файл на расширение проще и атомарен |
| goreleaser | дублирует release-please, больше конфигурации; nfpm + `gh` проще и совпадает со схемой агента |
| Гостевой бинарь качать по отдельному пину в bisquite | ещё одна пара пин+сеть на хосте; версия хоста и гостя могла бы разойтись |
| Ставить в образ `.deb` cream через dpkg | на хосте только `.deb` своей архитектуры; голый бинарь + `cream system init` дают те же файлы, а последующий `apt install cream` их перекрывает |
| Действия как свободные CLI в `/usr/local/sbin` (как `bisquite-teleport`) | нет общей блокировки, проверки параметров, единого JSON и кодов; ссылка в `PATH` остаётся возможной через `bin` |
| Вариант 2 описи агента (restart силами stvor-agent) | решение владельца — вариант 1: restart и подтверждение у владельца юнита |
| Секрет как `KEY=value` в argv с маскировкой в логах | argv виден в `/proc` всем процессам; маскировка не спасает |
| `knobs` отдельным файлом с позиционной грамматикой | описания с пробелами и кавычками ломают разбор; YAML уже разбирается для манифеста; одна схема вместо двух |
| Повтор `install` после `status: failed` только с `--reinstall`, без него — код 79 | повтор после сбоя сети — обычная операция; скрипт Stvor и оператор должны просто запустить ту же команду ещё раз |
| Действия из `Exec*` юнитов под той же блокировкой домена (`render` с `lock: exclusive`) | тупик: владелец блокировки синхронно ждёт restart юнита, юнит ждёт блокировку (§7.3); юнит не потомок и `CREAM_LOCK_FD` не наследует |
| `managed-status` под `lock: shared` | читатель ждал бы долгий `join` (до 5 мин) и срывал тик управляющего; файлы пишутся атомарно, чтению блокировка не нужна |
| `build: files` и `cream stage` в v1 | второй путь установки без хуков ради двух расширений; барьер архитектуры в v1 снимается сборкой на машине цели (§19 В4) |

## 19. Открытые вопросы (владелец — с рекомендацией)
| № | Вопрос | Рекомендация |
|---|---|---|
| В1 | Подписи источников: только sha256-пины в v1 или сразу подпись тегов и архивов? | v1 — sha256 (каналы доставки пинов уже доверенные: git, `.env`). В v1.x — SSH-подписи (`ssh-keygen -Y verify` формат, проверка через `golang.org/x/crypto/ssh` — вторая зависимость) с `signers` в источнике; поле зарезервировано |
| В2 | Литеральные секреты в строке `EXTENSION` VMFILE: VMFILE едет в образ | bisquite запрещает литеральное значение для ключа `secret:*` (по ответу `resolve`) и принимает его только ссылкой на файл/секрет сборки вне VMFILE; решает спека bisquite |
| В3 | `extensions/openwrt/` (не расширения, `UPLOAD` из подмодуля) | перенести в репозиторий bisquite (примеры OpenWrt), из каталога убрать — иначе пин архива каталога тащит не-расширения |
| В4 | Сборка под чужую архитектуру: режим `build: files` (`cream stage` на хосте, раскладка без запуска гостя — для `nocloud-cidata`, `cloud-growroot`) и/или `exec` через qemu-user/binfmt | не в v1: значение `files` зарезервировано (§4.2), в v1 собирать на машине цели, как сейчас. Вернуться минорной версией с подъёмом `cream:`, когда понадобится `raspios-normalized` на arm64 со сборкой на amd64; qemu-user — не брать (медленно, хуки с apt ненадёжны) |
| В5 | Переименование nft-таблицы и цепочек firewall и прочих `bisquite-*` имён на устройствах | переименовать сейчас (машин нет), вместе с шагом 3 |
| В6 | Нужен ли пользовательский (не root) доступ к `cream run … managed-status` для `stvor-agent` | нет: `stvor-agent` — root-служба с hardening юнита (решает спека stvor-agent); `root: false` у действия остаётся возможностью формата |
