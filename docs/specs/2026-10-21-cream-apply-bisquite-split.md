# `cream apply` и разделение bisquite ↔ cream: «bisquite строит образы, cream ставит софт»

> **Черновик на утверждение владельца, 2026-10-21; редакция 2 — по итогам skeptic.** Плана реализации нет; план
> пишется после решений по §16. Вход: направление владельца «bisquite строит образы, cream ставит софт». Код на момент
> написания: cream `1ea3017` (1.1.4), bisquite `2dcf890` (2.2.0, `origin/main`), cream-extensions `8e9d63f` (5.2.0),
> openwrt-builder `v0.3.0` (`206df8a`), Stvor `e1262de7`. Связанные документы: спека
> [cream-core](2026-10-18-cream-core.md) (§0 п. 3 и §12 эта спека **пересматривает**), спека bisquite
> `docs/specs/2026-10-19-extensions-via-cream.md` (её §4–§5 уходят целиком), отложенная спека OpenWrt
> (`docs/specs/2026-10-20-openwrt.md`, ветка `docs/openwrt-spec`, PR #16 — из неё берётся только ядро cream, без
> Teleport и Stvor).
>
> **Что изменила редакция 2 (рулинги контроллера после skeptic):** первая версия — без `cream.lock` (пины прямо в
> `cream.yaml`), без `ANNOTATE` в bisquite, без `bs vmfile migrate`; `--prune`, `--state-name`, `managed_by`,
> `--phase auto` отложены (§17). Исправлены: инвариант пина cream в Stvor (§10), сужена цель по секретам (§1, §6),
> `UPLOAD` каталога заменён на `COPY_IN` (§8.3), бутстрап читает отдельный `cream-bootstrap.env` (§7).

## 0. Вход и что пересматривается
**Направление владельца (на утверждение):**
1. bisquite — только сборщик: `FROM`, копирование файлов, выполнение команд, кеш по шагам. Ни каталога, ни версий
   расширений, ни cream на хосте, ни зависимости `cream` в `.deb`.
2. cream — провижнер **внутри гостя**: декларативный `cream apply -f cream.yaml` (желаемое состояние). Файл сам
   несёт пины: версия и sha256 cream, sha256 архива источника или commit git-источника. Отдельного `cream.lock` в
   первой версии нет (§17).
3. Секреты сборки — монтированием на время одного шага (как `RUN --mount=type=secret` в BuildKit), не файлами в
   слоях.
4. cream попадает в гостя сам (бутстрап по версии и sha256 из `cream.yaml`), без участия хоста.
5. Семейство OpenWrt 25.12 — cream и первые расширения (без Teleport и Stvor).

**Что этим отменяется в действующих решениях:**
| Документ | Пункт | Было | Станет |
|---|---|---|---|
| cream-core §0 п. 3 | bisquite и `EXTENSION` | bisquite исполняет `EXTENSION` через cream, хост разрешает офлайн | bisquite про cream не знает; `EXTENSION` удаляется (§8.4) |
| cream-core §12 | фаза build | `cream resolve` на хосте + `install --phase build --from-dir --record` в госте | `cream apply --phase build` в госте; `resolve`/`--from-dir` остаются только для совместимости 1.x (§4.8) |
| cream-core §3 | цепочка выпусков | `cream-bump`, `extensions-bump` → bisquite | bisquite из цепочки выпадает; Stvor в ней остаётся (§10, §11.3) |
| bisquite spec 2026-10-19 §4.1 | `Depends: cream (= пин)` | точная версия на хосте | зависимости нет |
| cream-core §0 п. 1 | поставка | `.deb` | `.deb` (deb-семейство) **и** `tar.gz` (OpenWrt) — tar уже выпускается |

Нужен ADR в cream: «формат `cream.yaml` и команда `apply` — часть контракта cream/v1» (номер — при слиянии; ADR-0003
занят черновиком OpenWrt). В bisquite — ADR «bisquite не знает о cream» (breaking 3.0.0).

## 1. Цели и не-цели
**Что должно стать верным:**
- `bs image build` на хосте без cream собирает образ с расширениями: в VMFILE — `COPY_IN` каталога `cream/` и одна
  команда «бутстрап && `cream apply`».
- Одинаковые `cream.yaml` + база дают одинаковый набор расширений (версии, деревья, несекретные параметры) на любой
  машине сборки и на живой машине: источники в файле пинуются (archive — sha256, git — commit), cream — версией и
  sha256; разрешение графа детерминировано снимком каталога.
- Выпуск cream или каталога **не требует** выпуска bisquite. Выпуск bisquite не требует подъёма пинов cream.
- Секрет сборки не попадает ни в argv, ни в журнал сборки, ни в метки и аннотации. **В qcow2** (включая свободные
  блоки) не попадают секреты ключей `scope: build` и секреты, которые расширение по своему контракту не сохраняет.
  Секрет, который расширение хранит на машине (файл `secrets` домена), остаётся в образе — это контракт расширения,
  а не утечка (§6.1).
- Тот же `cream apply -f` работает на живой машине (Debian/Ubuntu, затем OpenWrt 25.12).
- Скрипт Stvor (`cream install … teleport`, `cream run teleport join`) работает без изменений; инвариант «cream в
  образе ≤ пин Stvor» соблюдается порядком выпусков (§10, §11.1).

**Не-цели:**
- Решатель версий: как и в cream-core, у источника одна версия расширения.
- `cream.lock`, удаление лишнего (`--prune`), автоопределение фазы, lineage в OCI-аннотациях — §17 «Позже».
- Оркестрация многих машин, pull-агент «держать состояние» (это `stvor-agent`/Stvor, вне спеки).
- Teleport и Stvor на OpenWrt (отложены владельцем 2026-10-20).
- Сборка чужой архитектуры (qemu-user): барьер остаётся, как у любого `RUN_COMMAND`.
- Материализованный кеш слоёв в bisquite (qcow2 backing files) — отдельная отложенная спека bisquite
  `2026-09-04-layer-snapshots-deferred.md`; здесь — только ключи шагов, на которые он ляжет (§8.5).

## 2. Как сейчас (факты)
### 2.1 bisquite 2.2.0 — что знает о cream
| Место | Что делает |
|---|---|
| `packaging/deb/DEBIAN/control.in` | `Depends: cream (= @CREAM_VERSION@)` |
| `src/bisquite/data/cream-pin.env` (+ ссылка `tools/cream-pin.env`) | версия cream и sha256 `.deb` по архитектурам; `Makefile` `cream-deb`, `test-deb`, `test-build` |
| `src/bisquite/data/extensions.yaml` | умолчание `cream.sources` — архив каталога по пину (поднимает `extensions-bump`) |
| `domains/build/ports/cream.py` (`CreamPort`: `version`, `resolve`, `probe`, `list_sources`, `sync`, `catalog`, `guest_deb`, `stage_guest_deb`, `pinned_version`, `fetch_guest_deb`) | порт домена к процессу cream |
| `infrastructure/cream/{cli,errors,staging,guest_deb}.py` (~1400 строк) | адаптер: процесс `cream --json`, сопоставление кодов 64–79, staging `record.json`/`params.env`, tar 0:0, `.deb` по пину |
| `domains/build/services/extension_router.py` (408) | пробы `resolve`, итоговый `resolve`, проверка топологичности порядка VMFILE, `--installed` из меток базы |
| `domains/build/models/{cream,extensions,extension_ref}.py` | модели слоёв, ссылок `[src/]name[@version] K=V` |
| `domains/build/provenance.py` | метки `bisquite.lineage.{extensions,cream,root}`, `cream_layer_digest` = sha256(token+params_digest) |
| `infrastructure/build/builders/linux.py` | применение cream-слоя (последовательность Р30: `dpkg -i` пакета пина, `system init`, `incoming/<id>`, `install --phase build`, чтение `result.json` через `virt-cat` на отказе) |
| `infrastructure/config/extensions.py` | чтение `extensions.yaml` (системный/пользовательский/умолчание) |
| `infrastructure/parsers/vmfile_parser.py` | директива `EXTENSION` |
| `cli/commands/extension.py` (537) | `bs extension sources|sync|ls|check` |
| `application/services/system_check.py` | компонент self-check «Расширения cream» (версия хоста = пин) |
| `infrastructure/build/builders/openwrt.py` | `EXTENSION` запрещён («расширения рассчитаны на apt и systemd») |

### 2.2 Цена связки (почему менять)
- **Тройной пин одной версии.** Хост, пакет в госте и `Depends` обязаны совпадать (Р30). Каждый выпуск cream —
  PR `cream-bump` и выпуск bisquite; каждый выпуск каталога — `extensions-bump` и выпуск bisquite.
- **Знание о каталоге на хосте.** `bs extension sync` от того же euid, что сборка; кеш cream хоста — условие сборки.
- **Дайджест шага — по строке VMFILE.** `COPY_IN`/`UPLOAD`/`RUN_*` получают `sha256(layer.content)`
  (`linux.py`, `_apply_layer_by_type`): смена содержимого копируемого файла **не меняет** `chain_digest`. Только у
  cream-слоя дайджест содержательный (`token + params_digest`).
- **Один `virt-customize` на расширение.** Десять `EXTENSION` — десять запусков appliance и десять `dpkg -i` cream.
- **OpenWrt закрыт.** Резолвер на хосте, `dpkg` пакета в госте и `family: deb` делают `EXTENSION` на OpenWrt
  невозможным без новой ветки в bisquite.
- Материализованного кеша шагов **нет** (отложенная спека): падение на шаге N — сборка с нуля. Практика —
  цепочки `FROM` (`dst-base → dst-desktop → dst-robot-ws`, `debian13:gnome → nvidia`).

### 2.3 cream 1.1.4 — на что опирается `apply`
- Источники и кеш: `internal/source` (`sources.go`, `sync.go`, `fetch.go`, `extract.go`, `lockfile.go` —
  `sources.lock` с `resolved`/`catalog_version`), синхронизация — единственное место сети.
- Граф и порядок: `internal/resolve` (`graph.go`, `layer.go`, `installed.go`, `host.go`), `internal/catalog`.
- Установка/обновление/удаление: `internal/engine` (`install.go`, `upgrade.go`, `remove.go`, `build.go` с
  `policy-rc.d`, `configure.go`), реестр `internal/state`, настройки `internal/config` (`params.env`, `KEY=-`).
- Отпечатки: `internal/fsx/digest.go` (дерево, §9.5 cream-core), `internal/state/digest.go` (`params_digest`).
- CLI: `internal/cli` (таблица команд `commands.go`, по файлу `cmd_<группа>.go` на группу).

## 3. Целевая архитектура
```
 автор образа                         машина сборки (bisquite 3.0, cream НЕ нужен)           гость (virt-customize)
 ─────────────                        ───────────────────────────────────────────           ──────────────────────
 cream/robot.yaml  (пины внутри)      VMFILE:
 cream/cream-bootstrap.env  ◄─ cream    FROM base
 cream/cream-bootstrap.sh      bootstrap COPY_IN cream:/etc/cream  ── ключ: отпечаток дерева ─► /etc/cream/cream/{robot.yaml,
                               export                                                            cream-bootstrap.env, .sh}
                                        RUN_COMMAND [--mount=type=secret,id=…] \
                                          sh /etc/cream/cream/cream-bootstrap.sh && \      ─► .deb / tar.gz по env, sha256
                                          cream apply --phase build -f /etc/cream/cream/robot.yaml
                                                                                           ─► source sync (сеть appliance) →
                                                                                              граф по реестру гостя →
                                                                                              install/upgrade/set → реестр →
                                                                                              /etc/cream/applied/robot.json
 Stvor (живой робот): curl … | sudo bash → dpkg -i cream → cream install teleport …   (без изменений)
 живая машина (опция):  cream apply -f robot.yaml [--secret id=…,src=…]
```
| Кто | Отвечает за | Не знает о |
|---|---|---|
| bisquite | база, копирование, команды, секрет-монтирование на шаг, ключи шагов по содержимому, lineage шагов, хранилище, деплой | cream, каталоге, версиях расширений, `cream.yaml` |
| cream | желаемое состояние, пины, источники, граф, установка, реестр, бутстрап самого себя | bisquite, qcow2, OCI-метках |
| cream-extensions | расширения и снимки каталога | bisquite |
| автор образа | `cream/*.yaml`, `cream-bootstrap.env` (командой `cream bootstrap export`), VMFILE | — |

**Граф зависимостей проверяется только в госте.** Реестр установленного в базе `FROM` виден лишь внутри гостя,
поэтому ни хост, ни машина автора граф не разрешают — и не должны: `cream apply` строит граф на «реестр гостя ∪
файл» в момент шага. Это закрывает все существующие схемы примеров:
- цепочки `FROM`: `dst-base → dst-desktop → dst-robot-ws`, `debian13:gnome → nvidia` — потомок видит в реестре всё,
  что поставила база, и его `depends` закрываются поставленным;
- перемежённые шаги одного VMFILE (`jetson-nano-base`: `EXTENSION yq`, `nocloud-cidata`, затем `RUN_COMMAND`
  netplan/purge, затем `tensorrt`, `l4t-*`, `firefox`): несколько файлов (`cream/jetson-nano-base-pre.yaml`,
  `cream/jetson-nano-base.yaml`) и несколько шагов `apply` подряд — каждый следующий видит реестр гостя после
  предыдущего. Ограничение то же, что у `RUN` в Dockerfile: порядок шагов задаёт автор.

## 4. `cream apply`
### 4.1 Файл желаемого состояния `cream.yaml`
Один формат — YAML; разбирает его **только cream** (Go, строгий режим). Скрипт бутстрапа YAML не читает (§7).
```yaml
schema: cream-apply/v1             # отдельная схема в контракте cream/v1 (минорное расширение, ADR-0001)
cream:                             # каким cream исполнять этот файл; бутстрап ставит ровно его
  version: 1.2.0
  sha256:                          # из SHA256SUMS релиза; ключ — <os>-<arch>-<формат>
    linux-amd64-deb: 8f…           # нужны только платформы, на которых файл применяется
    linux-arm64-deb: 1c…
    linux-amd64-tar: 4a…           # OpenWrt (после cream 1.3)
sources:                           # как sources.yaml cream-core §9.1; порядок — приоритет
  - name: core
    type: archive
    url: https://github.com/iamletenkov/cream-extensions/releases/download/v5.2.0/cream-extensions-5.2.0.tar.gz
    sha256: 21121c16…              # обязательно
  - name: lab
    type: git
    url: https://git.example/lab-extensions.git
    commit: 3e0b…(40 hex)          # обязательно; ветка или тег — отказ 64 source.unpinned
extensions:                        # порядок — подсказка, не закон: устанавливает граф (§4.3)
  - name: cloud-user
  - name: desktop
    params: {DESKTOP_AUTOLOGIN: "1"}
  - ref: core/gnome@4.2.0          # короткая форма: [source/]name[@version] — @version утверждение (66 при расхождении)
  - name: some-ext
    params:
      MIRROR_TOKEN: {secret: mirror_token}   # значение — из секрета сборки/запуска (§6), не из файла
options:
  with_deps: false                 # true — добирать depends/requires как --with-deps (единственный поставщик)
  accept_risk: []                  # имена расширений с targets.live: confirm, разрешённые явно
```
Правила:
- Разбор строгий (`KnownFields`), как у манифестов. Литерал значения ключа, который манифест объявил `secret`,
  — отказ 64 `secret.in_file` (зеркало `secret.in_argv`): файл едет в образ.
- `cream.version` — точная версия; `apply` сверяет её со своей (`cream --version`): другая — 69
  `cream.version_mismatch` («бутстрап поставил не тот cream или файл исполняют руками другой версией»). На живой
  машине `--any-cream` снимает проверку (эксперименты), в build-фазе флаг запрещён (64).
- Источник `type: path` (каталог, скопированный в гостя тем же `COPY_IN`) пинуется отпечатком дерева `digest:` —
  для офлайн-сборки.
- Источники `apply` регистрирует в `/etc/cream/sources.yaml` под их именами: потом работают `cream upgrade`,
  `cream source list`. Одноимённый источник с другим пином **приводится к пину файла** (план показывает
  `source update core: 5.1.0 → 5.2.0`) — так потомок в цепочке `FROM` поднимает каталог базы. Одноимённый источник
  другого типа (`archive` против `git`) — отказ 69 `source.conflict`. Источник Stvor называется `stvor` и с
  `core` не пересекается.
- Параметры — те же ключи `config.keys`, что у `cream install K=V`; запись в домен настроек — тем же
  `internal/config` (типы, readonly, хуки `apply`/`secret`).

### 4.2 Семантика
- **Аддитивно:** «эти расширения должны стоять с этими версиями и параметрами». Остальное установленное не
  трогается. Так `apply` складывается по цепочке `FROM` и по нескольким файлам подряд (§3). Удаления в первой
  версии нет (§17).
- Действие по каждому расширению: нет — `install`; другая версия в источнике — `upgrade` (понижение — только
  `--allow-downgrade`, как в cream-core); те же версия, `ext_digest` и `params_digest` — ничего (`changed: false`);
  другие несекретные параметры — `set` + хук `apply` расширения (как `cream set --apply`), без переустановки.
- **Секретные ключи применяются всегда:** `params_digest` содержит только имя секрета (`secret:<id>`), не значение,
  поэтому сменившийся секрет `apply` не отличит от прежнего. Если в файле у расширения есть секретный ключ,
  `apply` каждый раз делает `set` этого ключа + хук `apply` и отвечает `changed: true` (причина в выводе:
  `secret_reapplied`). Хуки `apply`/`secret` расширений обязаны быть идемпотентными — это уже контракт cream/v1.
- Идемпотентность: повтор с тем же файлом — код 0, `changed: false` у всех расширений без секретных ключей.
- `--dry-run` — план действий (`install`/`upgrade`/`set`/`noop`, `source update`) без изменений, `--json` для машин.
- `--validate` — только разбор и проверка файла (схема, пины, `secret.in_file`), без root и без реестра — для CI
  автора.

### 4.3 Порядок и граф
Граф строится тем же `internal/resolve` на множестве «реестр машины ∪ файл» (§3: только в госте). Порядок —
топологический (поставщик раньше потребителя), независимые — по порядку в файле (а не по имени, как сейчас в
`resolve`: bisquite это мешало, см. `extension_router.py`). Зависимости, стоящие в базе `FROM`, закрываются
**реестром гостя** — флаг `--installed` и метки `bisquite.lineage.cream` больше не нужны.

### 4.4 Фазы
`--phase build|live` — **обязательный** флаг первой версии (автоопределение — §17). В `build` — `policy-rc.d`
(deb), никаких restart, `targets.build` проверяется как сейчас; хук, вернувший 20 в build, — `skipped` только с
`allow_skip` в файле. Все примеры и guide пишут `--phase build` явно.

### 4.5 Вывод, коды, состояние
Коды — таблица cream-core §11.3 без новых номеров; новые строковые коды: `secret.missing`, `secret.in_file`,
`source.unpinned`, `source.conflict`, `cream.version_mismatch`, `bootstrap.mismatch`. `--json` —
`{"plan": [...], "results": [...], "file": {"name": "robot", "digest": "sha256:…"}}`; формы закрепляются
golden-тестами, как остальные команды.

После успеха `apply` пишет `/etc/cream/applied/<имя файла без .yaml>.json`: digest файла, `cream.version`, пины
источников, итоговые токены (`source/name@version#tree`) с несекретными `params_digest`. Имя — по фиксированному
правилу, без флага; одноимённый файл в потомке `FROM` перезаписывает запись базы, поэтому примеры называют файлы
по образу (`cream/dst-desktop.yaml`). Файл читается без cream и без загрузки образа: `virt-cat -a image.qcow2
/etc/cream/applied/dst-desktop.json` — по требованию, bisquite его не разбирает и в метки не кладёт.

### 4.6 Место в коде cream
| Новое | Что |
|---|---|
| `internal/applyspec` | разбор и проверка `cream.yaml` (пины, `secret.in_file`, `source.unpinned`) |
| `internal/apply` | план (diff реестр ↔ желаемое), исполнение через `engine.Install/Upgrade/Set` под одним `lock/global`, запись `/etc/cream/applied/*.json` |
| `internal/secretmount` | чтение секретов из каталога (§6) с проверкой прав |
| `internal/bootstrap` | `cream bootstrap export` (§7.1): `cream-bootstrap.env` из поля `cream:`, скрипт из встроенного ресурса; `--check` |
| `internal/cli/cmd_apply.go`, `cmd_bootstrap.go` | `apply`, `bootstrap export` |
| `scripts/cream-bootstrap.sh` (встраивается `embed` и выкладывается ассетом релиза) | §7 |
Меняется: `internal/source` (регистрация источников из файла, `commit`/`digest` пины, синхронизация без сети, если
кеш совпал с пином), `internal/resolve` (порядок независимых — по входу), `internal/config` (`scope: build`, §6.1).
Не меняется формат манифеста `cream/v1` (кроме необязательного `scope` у ключа — минор).

### 4.7 Пример живой машины
`sudo cream apply --phase live -f /etc/cream/cream/robot.yaml --secret id=mirror_token,src=/root/token` — тот же
файл, что в сборке; секрет — файл или `env=`, не argv.

### 4.8 Что остаётся от интерфейса bisquite в cream 1.x
`cream resolve` и `install --phase build --from-dir --record` **не удаляются в 1.x** (контракт, ADR-0001), но
помечаются устаревшими в `docs/cli.md`; их потребитель (bisquite 2.x) остаётся на старом пине. Удаление — cream
2.0. Фикстуры контракта для bisquite (`tests/fixtures/cream/<версия>/` в bisquite) перестают обновляться.

## 5. Пины без lock
- **Что пинуется и где:** cream — `cream.version` + `cream.sha256.<платформа>`; каталог — `sha256` архива или
  `commit` git; офлайн-каталог — `digest` дерева. Всё — в `cream.yaml`, который автор коммитит рядом с VMFILE.
- **Почему этого хватает для воспроизводимости:** у источника одна версия расширения (не-цель «решатель»), снимок
  каталога зафиксирован sha256/commit, поэтому граф и замыкание `with_deps` на той же базе детерминированы. lock
  добавил бы только (а) заранее разрешённый граф — бесполезный, потому что реестр базы виден лишь в госте (§3), и
  (б) пины деревьев отдельных расширений — их уже покрывает sha256 архива.
- **Подмена ловится в госте:** sha256 архива источника и sha256 пакета cream сверяются до использования; расхождение
  — 69 (`source.mismatch` — существующий код синхронизации, `bootstrap.mismatch`).
- **Поднять каталог или cream** — правка `cream.yaml` руками (новые URL и sha256 из `SHA256SUMS` релиза);
  `cream bootstrap pin 1.2.0` печатает готовый блок `cream:` по `SHA256SUMS` релиза, чтобы не копировать хеши
  вручную. Автоматический подъём (`cream lock --update`) — §17.

## 6. Секреты сборки и запуска
### 6.1 В cream
- Ссылка в `cream.yaml`: `KEY: {secret: <id>}`; `id` — `[a-z0-9_]{1,64}`.
- Источник значения при `apply`: каталог `--secrets-dir` (умолчание `/run/secrets`, конвенция BuildKit/Compose)
  файл `<id>`; на живой машине ещё `--secret id=…,src=FILE|env=VAR`. Нет файла — 66 `secret.missing` до любых
  изменений.
- Значение идёт в домен через существующий путь секретов (`KEY=-`/хук `secret`, файл `secrets` 0600), в argv и
  журнал — нет (фильтр `internal/runner/filter.go` получает значения для вымарывания).
- Секретные ключи переприменяются на каждом `apply` (§4.2), потому что их значение не входит в `params_digest`.
- **Что остаётся в образе:**
  | Ключ | В qcow2 после сборки |
  |---|---|
  | `scope: build` (новое необязательное поле `config.keys`, минор): токен приватного зеркала, пароль реестра на время установки | **нет** — cream отдаёт значение хуку только переменной окружения процесса, в домен (`secrets`) не пишет |
  | секрет, который расширение по своему контракту не сохраняет (использует в хуке и забывает) | **нет** |
  | секрет, который расширение хранит на машине (`secrets` домена, конфиг сервиса) | **да** — это назначение расширения; такой секрет не следует задавать при сборке образа, его задают на живой машине |
  Цель «секрет не в qcow2» и её тест относятся только к первым двум строкам.

### 6.2 В bisquite (универсально, без знания о cream)
- CLI: `bs image build --secret id=mirror_token,src=./token|env=MIRROR_TOKEN` (многократно). Web: только ссылки на
  файлы в `/etc/bisquite/secrets.d/<id>` (значения через браузер не принимаются).
- VMFILE: `RUN_COMMAND --mount=type=secret,id=mirror_token[,target=/run/secrets/mirror_token] <команда>`; без
  `target` — `/run/secrets/<id>`. Секрет, не переданный в CLI, — отказ до сборки; переданный и не использованный —
  предупреждение.
- Механика в `virt-customize` (один вызов на шаг): `--run-command 'mkdir -p /run/secrets && mount -t tmpfs -o
  mode=0700,size=1m bisquite-secrets /run/secrets'`, затем `--upload <host-tmp>:/run/secrets/<id>` (запись идёт в
  tmpfs ядра appliance, на диск гостя не попадает), затем команда шага, затем
  `--run-command 'umount /run/secrets; rmdir /run/secrets 2>/dev/null || true'`. Временный файл на хосте — 0600 в
  каталоге 0700, удаляется в `finally`. Значение секрета **никогда** не входит в текст команды: трасса
  `virt-customize -x`/`-v` (её bisquite пишет в журнал при `--debug`) печатает аргументы `--run-command`.
- **Проверка обязательна до принятия** (риск №1, §15): тест на ключе `scope: build` — «маркер секрета не встречается
  в сыром qcow2 (`qemu-img convert -O raw` + `grep -c`), в журнале сборки (включая `--debug`) и в OCI-манифесте».
  Запасного пути через текст команды нет: если `--upload` пишет мимо tmpfs, опция `--mount=type=secret` не входит
  в 3.0.0 до найденного и проверенного тем же тестом способа.
- Ключ шага включает `id` секретов, **не** значения (как BuildKit): смена секрета не инвалидирует шаг. Это
  документируется; `--no-cache-step N` пересобирает принудительно.

## 7. Доставка cream в гостя (бутстрап)
### 7.1 `cream-bootstrap.env` и `cream-bootstrap.sh`
`cream bootstrap export -f cream/robot.yaml [--dir cream/]` (cream на машине автора или контейнер, без root)
пишет рядом с файлом два файла:
- `cream-bootstrap.env` — производное от поля `cream:`, строго `KEY=VALUE` по строке, без кавычек и подстановок:
  ```
  CREAM_VERSION=1.2.0
  CREAM_SHA256_LINUX_AMD64_DEB=8f…
  CREAM_SHA256_LINUX_ARM64_DEB=1c…
  CREAM_URL_BASE=https://github.com/iamletenkov/cream/releases/download/v1.2.0
  ```
- `cream-bootstrap.sh` — копия скрипта той версии cream, что его экспортировала (встроен в бинарь).

`cream bootstrap export --check` (CI автора) — отказ, если `.env` или скрипт не совпадают с тем, что дал бы
экспорт; `cream apply` в госте сверяет `.env` в каталоге файла с полем `cream:` и при расхождении отвечает 69
`bootstrap.mismatch` — устаревший `.env` не проходит молча.

Почему отдельный файл, а не разбор `cream.yaml` в `sh`: YAML и даже JSON в `ash` busybox без `yq`/`jq` разбираются
только хрупким `awk`; `KEY=VALUE` читается встроенным `read` без внешних утилит.

### 7.2 Алгоритм скрипта
POSIX `sh` (годится для `ash` busybox). Вызов: `sh cream-bootstrap.sh [--env FILE] [--from DIR] [--mirror URL]`
(умолчание `--env` — `cream-bootstrap.env` рядом со скриптом); для живых машин без файла — явные
`--version V --sha256 H [--url U]`.
1. Чтение `.env`: `while IFS='=' read -r k v`, только известные ключи; значение проверяется `case` (версия —
   `[0-9]*.[0-9]*.[0-9]*`, sha256 — 64 hex); файл **не** исполняется через `.`/`source`. Неизвестный ключ или
   кривое значение — отказ (код 2).
2. Семейство и архитектура: `/etc/os-release` (`ID`/`ID_LIKE`), `uname -m` → `amd64|arm64`.
3. Формат: deb-семейство → `deb` (пакет нужен в dpkg: `stvor-agent` и другие `.deb` расширений объявляют
   `Depends: cream`); openwrt → `tar` (распаковка в `/usr/bin`, `cream system init`). Нет sha256 для платформы —
   отказ «файл не рассчитан на <платформа>».
4. Уже стоит cream: та же версия — ничего; старше — обновление; **новее** — отказ (код 3) «база собрана с cream
   новее файла — поднимите `cream.version`» (как сейчас `dpkg --compare-versions` в bisquite и в скрипте Stvor).
5. Загрузка: `--from DIR` (офлайн: пакет заранее положен в `cream/` и скопирован тем же `COPY_IN`) → `--mirror` →
   `CREAM_URL_BASE`; `curl`, иначе `wget`/`uclient-fetch`; sha256 сверяется **до** установки (`sha256sum -c`).
Сеть: шаг идёт в сети appliance `virt-customize`, как любые `apt-get`/хуки сейчас. Для офлайн-сборки — `--from` и
источник `type: path`.

### 7.3 Почему не статический бинарь без пакета
На deb-семействе `.deb` нужен: расширения ставят свои пакеты с `Depends: cream (>= 1.0.0)` — так упала приёмка
Task 16 bisquite (`dst-robot-ws`), когда cream лежал голым бинарём. На OpenWrt пакетов нет (решение В5 спеки
OpenWrt) — tar.gz.

## 8. bisquite 3.0.0
### 8.1 Удаляется
| Что | Файлы |
|---|---|
| Порт и адаптер cream | `domains/build/ports/cream.py`, `infrastructure/cream/` целиком (`cli.py`, `errors.py`, `staging.py`, `guest_deb.py`) |
| Маршрутизация и модели | `domains/build/services/extension_router.py`, `domains/build/models/{cream,extensions,extension_ref}.py` |
| Источники | `infrastructure/config/extensions.py`, `src/bisquite/data/extensions.yaml`, переменные `BISQUITE_CREAM`, `BISQUITE_CREAM_GUEST_DIR` |
| Пин cream | `src/bisquite/data/cream-pin.env`, `tools/cream-pin.env`, `Makefile` `cream-deb`, `Depends: cream` в `control.in`, `pyproject.toml` (`data/cream-pin.env`), `tests/fixtures/cream/` |
| Применение | в `infrastructure/build/builders/linux.py` — ветка `EXTENSION` (Р30, `_CREAM_*`, чтение `result.json`) |
| Lineage cream | `provenance.py`: `cream_layer_digest`, `merge_cream_lineage`, `parse_cream_lineage`, метка `bisquite.lineage.cream`; `bisquite.lineage.extensions` |
| CLI | `cli/commands/extension.py` (`bs extension sources|sync|ls|check`), регистрация в `cli/commands/root.py`, `application/container.py` |
| self-check | компонент «Расширения cream» в `application/services/system_check.py` |
| CI/выпуск | `.github/workflows/{cream-bump,extensions-bump}.yml`, `tools/{cream-bump,extensions-bump,cream-fixtures,test-cream-bump,test-extensions-bump}.sh`, `make test-build` в нынешнем виде |
Порядок: удаление — одной серией PR внутри 3.0.0, тесты удаляются вместе с кодом.

### 8.2 Добавляется (универсально)
1. **Ключи шагов по содержимому:** дайджест `UPLOAD`/`COPY_IN` = sha256(директива + отпечаток дерева источника
   по алгоритму cream-core §9.5: путь, режим, sha256 содержимого); `RUN_SCRIPT` — плюс содержимое скрипта;
   `RUN_COMMAND` — строка + `id` секретов. Сейчас — только строка (§2.2): это исправление само по себе.
2. **Секреты** — §6.2: флаг `--secret`, опция `--mount=type=secret` у `RUN_COMMAND`/`RUN_SCRIPT`, тест «секрет не
   в образе».
3. **Объединение шагов** не нужно: `cream apply` — один шаг на файл состояния.

### 8.3 Пример VMFILE после
```
FROM ubuntu2404:dst-base
LABEL os=ubuntu
LABEL arch=amd64
RESIZE size=16G device=/dev/sda idx=1
COPY_IN cream:/etc/cream
RUN_COMMAND sh /etc/cream/cream/cream-bootstrap.sh && cream apply --phase build -f /etc/cream/cream/dst-desktop.yaml
```
- `COPY_IN` — это `virt-customize --copy-in LOCAL:REMOTEDIR`: каталог кладётся **внутрь** целевого, поэтому
  `cream/` рядом с VMFILE оказывается в `/etc/cream/cream/`. `UPLOAD` для каталога не годится — `--upload` копирует
  один файл. Подкаталог `/etc/cream/cream/` не пересекается с собственными файлами cream (`/etc/cream/sources.yaml`,
  `/etc/cream/<домен>/`).
- В `cream/`: `dst-desktop.yaml`, `cream-bootstrap.env`, `cream-bootstrap.sh` (`cream bootstrap export`), по
  желанию — `.deb` cream для `--from`. Потомок в цепочке `FROM` снова копирует свой `cream/` — одноимённые файлы
  перезаписываются, файлы базы с другими именами остаются (поэтому имена — по образу).
- Секрет: `RUN_COMMAND --mount=type=secret,id=mirror_token sh … && cream apply …` и
  `bs image build --secret id=mirror_token,env=MIRROR_TOKEN`.

### 8.4 `EXTENSION` — удалить
Директива удаляется; парсер отвечает отказом с подсказкой (как сейчас для `UPDATE` → `UPGRADE`, словарь
переименований в `vmfile_parser.py`): «`EXTENSION` удалена в bisquite 3.0: расширения ставит cream в госте —
см. docs/guides/migration-3.0.md». Сахар, разворачиваемый в `COPY_IN` + `RUN`, отвергнут (§14). Утилиты
автоматической миграции нет: примеры (§11.2) переписываются руками в плане, внешних потребителей нет.

### 8.5 Кеш и инкрементальность
- Ключ шага `RUN_COMMAND … cream apply` = chain(ключ базы, ключ `COPY_IN cream` = отпечаток дерева `cream/`
  (файл состояния с пинами + `.env` + скрипт), строка команды, id секретов). То есть ровно «хеш cream.yaml + базы»:
  все пины внутри файла, поэтому ключ воспроизводим по смыслу без lock.
- **Что теряем:** при per-extension слоях смена параметра одного расширения пересобирала (логически) хвост от
  этого слоя; теперь — весь шаг `apply`. Реального выигрыша сейчас не было: материализованного кеша нет (§2.2),
  а все слои всё равно применялись заново.
- **Чем заменяем:** (1) несколько файлов состояния и шагов `apply` (§3, пример `jetson-nano-base`); (2) цепочки
  `FROM`, как сейчас; (3) внутри одного `apply` cream не переставляет уже стоящее (`noop`).
- Когда появится материализованный кеш шагов (отложенная спека), он ляжет на эти ключи без изменений в cream.

### 8.6 `bs extension sync/sources/ls/check`
Удаляются вместе с группой `bs extension`. Замены: `cream apply --validate` и `cream bootstrap export --check`
(CI автора), `cream apply --dry-run` в госте (что будет сделано). bisquite-подсказки «выполните
`bs extension sync`» исчезают.

### 8.7 Lineage и web UI
- Метки `bisquite.lineage.extensions`/`bisquite.lineage.cream` больше не пишутся; `bisquite.lineage.root` и
  цепочка шагов остаются. Состав образа — `/etc/cream/applied/*.json` (`virt-cat` по требованию, §4.5) и
  `cream list --json` в живой машине. Общая директива для выноса файла гостя в OCI-аннотацию — §17.
- Образы 2.x с метками `bisquite.lineage.cream` читаются (метки показываются как любые), но как база для 3.0
  ничего не дают: зависимости закрывает реестр гостя.
- **Web UI** (`bs web`): (1) self-check без компонента cream; (2) просмотр VMFILE (`/api/images/{ref}/vmfile`)
  подсвечивает `--mount=type=secret`; (3) файлы контекста шага `COPY_IN` (`cream/*.yaml`, `.env`) сохраняются вместе
  с VMFILE в артефакт сборки и доступны тем же эндпоинтом (`/api/images/{ref}/context/{path}`), иначе по образу не
  восстановить желаемое состояние без `virt-cat`. Секреты в UI — только ссылки (§6.2).

### 8.8 OpenWrt в bisquite
`OpenWrtBuilder` снимает запрет `EXTENSION` естественно (директивы нет); `RUN_COMMAND` и `COPY_IN` уже разрешены —
`cream apply` работает, как только cream поддерживает семейство `openwrt` (§9). Блокировки
`INSTALL/UNINSTALL/FIRSTBOOT*` не меняются.

## 9. Семейство OpenWrt 25.12
### 9.1 cream (минор после `apply`)
Берётся из отложенной спеки OpenWrt **только ядро** (её §4.1, срез 1): `platform.family: openwrt` (`ID=openwrt`,
`VERSION_ID >= 25.12`, только `apk`), `internal/systemd` → `internal/initsys` (systemd | procd, выбор по семейству),
`/etc/init.d/cream-configure` со стадиями `early` (S19) / `system` (S95) / `cloud-user` (S95 после system),
`graphical` — отказ валидации; состояние в `/opt/cream/var` (не tmpfs), `TMPDIR` хуков — `/opt/cream/var/tmp`;
поставка — `tar.gz` (§7); `policy-rc.d` не нужен. Teleport, glibc-рантайм, `keep.d` Teleport, скрипт Stvor —
**не входят**. `sysupgrade` стирает cream и реестр (решение Р4 спеки OpenWrt): после него — повторный `apply`.

### 9.2 Каталог: `extensions/openwrt/*`
Отдельный корень и снимок `cream-extensions-openwrt-<v>.tar.gz` (дубли имён в одном каталоге запрещены; решение
спеки OpenWrt §4.2). Первые расширения:
| Расширение | Что делает | Почему так |
|---|---|---|
| `amneziawg` | **только настройка**: интерфейс и пиры через `uci` (`network.<if>`, `proto amneziawg`), параметры обфускации (`Jc`, `Jmin`, `Jmax`, `S1`, `S2`, `H1`–`H4`), приватный ключ — `secret` (хранится на машине: задаётся на живой машине, не при сборке, §6.1); `check` — `awg show` | `kmod-amneziawg` привязан к `vermagic` ядра релиза и таргета — он уже в образе openwrt-builder v0.3.0 (пины по таргетам); ставить kmod из cream значит дублировать матрицу пинов. `platform.commands: [awg, uci]` — без пакета расширение не ставится (68) |
| `podkop` | настройка `podkop` через `uci` (режим, списки доменов/подсетей, исходящий — ссылка `vless://…` как `secret`), `restart` фоном; `requires: [sing-box]` способностью из `assumed` образа | пакет `podkop` и `luci-app-podkop` уже в образе v0.3.0; конфликт с `https-dns-proxy` известен (openwrt-builder `packages.txt`) — `conflicts.extensions` не нужен, это не расширение, проверка в `check` |
| `luci-apps` | `apk add` пакетов LuCI из **официальных подписанных фидов** по списку-параметру (`LUCI_APPS="luci-app-sqm luci-app-wol"`), `remove` — `apk del` поставленного им | пакеты фидов подписаны ключом релиза; список — параметр, а не по расширению на пакет; сторонние `.apk` (Argon) — остаются делом образа |
`platform.commands` у всех — `uci`, `apk` (и `sh`): хуки openwrt-расширений пишутся на POSIX `sh`, а не bash
(в чистой базе bash не гарантирован).

### 9.3 Связь с openwrt-builder
- `openwrt-builder` v0.3.0 — **база**: прошивка 25.12.6, `wrt.cloudinit`, онлайн-ресайз, пакеты, которые зависят
  от ядра или собраны вне фидов (AmneziaWG kmod/tools/luci-proto, podkop, Argon). Он не знает о cream.
- cream поверх базы делает то, что меняется от машины к машине: настройку (`amneziawg`, `podkop`) и добавочные
  пакеты фидов (`luci-apps`).
- Прошить cream в образ openwrt-builder не нужно: бутстрап ставит его шагом сборки bisquite или на живой машине.
- Сужение образа до «совсем голой базы» (вынести podkop/Argon в cream) — открытый вопрос §16 В4.

## 10. Влияние на Stvor и stvor-agent
- **Скрипт не меняется:** `backend/app/modules/teleport/install_script.py` делает `dpkg -i` cream по пину `.env`
  (`CREAM_VERSION`, `CREAM_SHA256_*`), `cream source add stvor --archive … --sync`, `cream install --source stvor
  teleport …`, `cream run teleport join` (секрет на stdin), `cream install stvor-agent`. Все эти команды в cream 1.x
  остаются; `apply` их не заменяет.
- **Инвариант «cream в образе ≤ пин Stvor».** Скрипт отказывает, если на машине cream **новее** пина портала
  (`dpkg --compare-versions "$CREAM_HAVE" gt "$CREAM_VERSION"` → «обновите пины портала»). Образ, собранный с
  `cream.version: 1.2.0`, при пине Stvor 1.1.x на регистрации робота упадёт. Отсюда:
  1. до первых образов роботов на bisquite 3.0 Stvor поднимает пин до cream 1.2.0 (§11.1, шаг 2);
  2. цепочка `cream-bump` **продолжает** открывать PR подъёма пина в Stvor (и в cream-extensions) на каждый выпуск
     cream; из цепочки выпадает только bisquite;
  3. guide bisquite и примеры роботов: `cream.version` в образе робота поднимают только после того, как пин Stvor
     догнал эту версию.
- `stvor-agent` зовёт `cream run teleport set-managed` — без изменений.
- **Опция, не требование:** скрипт может перейти на `cream apply -f robot.yaml` (`teleport` + `stvor-agent`), но
  `join` — действие, а не установка: его по-прежнему делает `cream run teleport join`. Выигрыш мал — переход только
  по отдельной задаче Stvor.
- Образы роботов, собранные bisquite 3.0 с `cream apply` (teleport в фазе build без `join`), ведут себя как сейчас
  образы с `EXTENSION teleport`: регистрация — командой портала на живой машине.

## 11. Миграция и выпуски
### 11.1 Порядок
| Шаг | Репозиторий | Версия | Что |
|---|---|---|---|
| 1 | cream | **1.2.0** (feat) | `apply` (`--phase`, `--dry-run`, `--validate`, `--secret`/`--secrets-dir`), `bootstrap export`/`pin`, `scope: build`, `cream-bootstrap.sh` в релизе, образ контейнера; `resolve`/`--from-dir` помечены устаревшими; ADR |
| 2 | **Stvor** | пин | поднять `CREAM_VERSION`/`CREAM_SHA256_*` до 1.2.0 (PR `cream-bump`), зелёный `robots-gate` — **до** первых образов на bisquite 3.0 |
| 3 | cream-extensions | минор | `scope: build` у ключей-токенов зеркал, если есть; CI: `cream apply --validate` на примерах |
| 4 | bisquite | **3.0.0** (`feat!`) | §8.1 удаление, §8.2 добавление, примеры (§11.2), guide; `Depends: cream` снят |
| 5 | cream | **1.3.0** (feat) | семейство `openwrt` (§9.1), ADR семейства; Stvor поднимает пин по шагу 2 |
| 6 | cream-extensions | минор | корень `extensions/openwrt`, снимок `-openwrt`, `amneziawg`, `podkop`, `luci-apps` |
| 7 | bisquite | 3.x | пример `examples/build/amd64/openwrt` с `cream apply` |
Шаги 1–4 не зависят от OpenWrt; 5–7 — после 1.

### 11.2 Примеры bisquite
`EXTENSION` встречается в 30 VMFILE (`examples/dst/*`, `examples/build/{amd64,jetson,raspberry-pi}/*`) и в README
этих каталогов. Каждый VMFILE переписывается **руками** в плане: каталог `cream/` с файлами по образу
(`<образ>.yaml`, общий для каталога `cream-bootstrap.env` и `cream-bootstrap.sh`), `COPY_IN cream:/etc/cream` и
шаг(и) `apply`. Подряд идущие `EXTENSION` — один файл; перемежённые с `RUN_COMMAND` — по файлу на группу
(`jetson-nano-base`, §3). Поднятие пинов в примерах — ручное (`cream bootstrap pin`); CI bisquite гоняет
`cream apply --validate` и `cream bootstrap export --check` в контейнере cream из релиза (cream не становится
зависимостью bisquite, только инструментом CI).

### 11.3 Цепочка выпусков
- `cream-bump.yml` больше не открывает PR в bisquite; Stvor и cream-extensions — как сейчас. `extensions-bump` в
  bisquite удаляется.
- bisquite CI больше не скачивает `.deb` cream для хоста; `make test-build` переписывается на общий шаг: пример с
  `cream apply` на настоящем `virt-customize`.

### 11.4 Guide миграции (`bisquite: docs/guides/migration-3.0.md`)
1. Подряд идущие `EXTENSION [src/]name[@v] K=V` → записи `extensions:` в `cream/<образ>.yaml`; источники — из
   старого `extensions.yaml` с пином (`sha256`/`commit`); блок `cream:` — `cream bootstrap pin <версия ≤ пин Stvor>`.
2. Секретные параметры — `{secret: id}`, в VMFILE — `--mount=type=secret`; секреты, которые расширение хранит на
   машине, — убрать из сборки и задавать на живой машине (§6.1).
3. `cream bootstrap export -f cream/<образ>.yaml`; `cream apply --validate`; закоммитить `cream/`.
4. Заменить группу `EXTENSION` на `COPY_IN cream:/etc/cream` + `RUN_COMMAND sh … && cream apply --phase build -f …`.
5. Собрать; сверить `virt-cat … /etc/cream/applied/<образ>.json` с прежней меткой `bisquite.lineage.extensions` базы.
6. Убрать `extensions.yaml` (`/etc/bisquite`, `$XDG_CONFIG_HOME/bisquite`) — bisquite 3.0 на него предупреждает;
   удалить пакет cream с машины сборки (если не нужен для `bootstrap export`).
Реальных потребителей вне владельца нет — переходной период (две ветки в bisquite) не нужен.

### 11.5 Грубая оценка объёма
| Часть | Объём |
|---|---|
| cream 1.2.0: `applyspec`, `apply` (план/исполнение/состояние), секреты и `scope: build`, `bootstrap export/pin` + скрипт, e2e на `virt-customize`, ADR | ~5–7 дней |
| bisquite 3.0.0: удаление (~2500 строк кода и тестов), ключи шагов по содержимому, `--secret`/`--mount`, тест «секрет не в raw», guide | ~4–6 дней |
| Примеры bisquite: 30 VMFILE + README вручную, CI-проверки | ~1,5–2 дня |
| Stvor: подъём пина 1.2.0 + `robots-gate` на образе из `apply` | ~0,5 дня |
| OpenWrt: cream 1.3.0 (`initsys`, procd, tar-поставка, e2e QEMU) + 3 расширения + пример bisquite | ~6–8 дней |

## 12. Безопасность
- Граница доверия прежняя: источник и его пин (sha256 архива / commit / digest дерева) — теперь обязательный и
  записан в `cream.yaml`. Пакет cream пинуется sha256 там же — подмена каталога или релиза ловится в госте.
- Хуки — root-код из источника, как раньше. `apply` не расширяет права.
- Секреты: §6; тест «маркер ключа `scope: build` не в raw-qcow2 / журнале (включая `--debug`) / манифесте» — в CI
  bisquite (`make test-build`) и в e2e cream. Значение секрета не попадает в текст команд `virt-customize`.
- `cream.yaml` и `cream-bootstrap.env` едут в образ (`/etc/cream/cream/`) и в артефакт сборки — значения секретов
  там запрещены проверкой `secret.in_file`.
- `cream-bootstrap.env` не исполняется (`read` + `case`, не `source`), поэтому подложенный файл не даёт выполнения
  кода; чужой `--mirror` не получает доверия — sha256 из файла.

## 13. Тестирование и приёмка
| Уровень | Что |
|---|---|
| cream unit | разбор `cream.yaml` (golden-ошибки: `source.unpinned`, `secret.in_file`, `cream.version_mismatch`), план `apply` (install/upgrade/set/noop/source update; секретный ключ — всегда `set`, `changed: true`), `bootstrap export` (байтовая стабильность `.env`, `--check`), `bootstrap.mismatch` |
| cream sh-тест | `cream-bootstrap.sh` под `busybox ash`: кривой `.env` (лишний ключ, `$(…)` в значении) — отказ без исполнения; новее/та же/старше версия; sha256 не сошёлся |
| cream e2e (`test/build`) | `virt-customize` на Debian 12: бутстрап с `--from`, `apply --phase build`, повтор — `changed:false`; база `FROM` с реестром закрывает `depends`; два файла и два шага `apply` подряд; ключ `scope: build` не попал в `secrets` домена |
| cream e2e OpenWrt | (после 1.3) QEMU 25.12 x86-64: бутстрап tar.gz, `apply` `amneziawg` с тестовым ключом, перезагрузка, `cream list` тот же |
| bisquite unit | ключи шагов по содержимому (`COPY_IN` каталога); `--mount=type=secret` разбор и отказы; `EXTENSION` → отказ с подсказкой |
| bisquite `test-build` | пример с `COPY_IN cream:/etc/cream` + `cream apply` + секрет-маркер на ключе `scope: build`: маркера нет в raw-qcow2, журнале (`--debug`), манифесте |
| Stvor | `scripts/robots-gate.sh` на роботе, поднятом из образа, собранного bisquite 3.0 через `cream apply` (cream 1.2.0 в образе, пин Stvor 1.2.0): регистрация, `cream run teleport join`, `stvor-agent` — зелёные; отдельно — отказ скрипта на образе с cream новее пина |

## 14. Отвергнутые варианты
| Вариант | Почему нет |
|---|---|
| **Оставить как есть** (`EXTENSION` через cream на хосте) | тройной пин одной версии и выпуск bisquite на каждый выпуск cream и каталога; кеш cream на хосте от того же euid; дайджест шага по строке; OpenWrt требует новой ветки в bisquite; `extension_router` + адаптер ~2000 строк, которые повторяют решения cream |
| **cream на хосте + chroot** в смонтированный образ (`guestmount`) | хуки исполняют бинари гостя — на чужой архитектуре нужен qemu-user (не-цель cream-core); FUSE и root на машине сборки; cream хоста снова связан версией с bisquite; `virt-customize` уже даёт изолированный appliance |
| **Ansible вместо cream** | Python в госте (на чистом OpenWrt нет); нет пинов коллекций/ролей с дайджестами; нет реестра установленного и действий с машинным выводом, на которых стоит `stvor-agent` (`cream run teleport set-managed`); второй формат расширений рядом с cream/v1. Ansible может вызывать `cream apply` — не запрещено |
| **`cream.lock` в первой версии** | граф всё равно разрешается только в госте (реестр базы `FROM` снаружи не виден), а пины источников и cream помещаются в сам `cream.yaml`; lock добавил бы формат, команду и проверку свежести без выигрыша в воспроизводимости. Отложен (§17) — понадобится для автоматического подъёма пинов |
| **Бутстрап разбирает `cream.yaml` (или JSON) в `sh`** | в `ash` busybox нет `yq`/`jq`, разбор `awk` хрупок к форматированию; отдельный `KEY=VALUE` читается `read` и сверяется `apply` с исходником |
| **Подписанный файл со списком (cosign / «защищённый файл»)** | Что даёт подпись: подлинность **автора** списка. В сборке bisquite автор VMFILE и `cream.yaml` — одно лицо, файлы в одном git; подлинность содержимого (каталог, cream) уже дают sha256 в `cream.yaml`. cosign keyless тянет Sigstore (Fulcio/Rekor, сеть, `sigstore-go`) — против ADR-0002 (одна зависимость); keyed-cosign/minisign — вопрос раздачи ключей в гостя. Подпись **не шифрует**: секреты в подписанном файле так же видны — задача секретов решается монтированием (§6). Где подпись нужна: если желаемое состояние приходит **извне** (портал Stvor раздаёт `robot.yaml` машинам) — тогда ed25519 detached-подпись на stdlib (`crypto/ed25519`), ключ в `/etc/cream/trusted.d/`; отложено (§16 В6). Подпись выпусков cream (`SHA256SUMS`) — отдельная задача CI |
| Зашифрованный файл секретов (sops/age) в образе | ключ расшифровки всё равно надо доставить в сборку — та же задача, что монтирование, плюс шифротекст навсегда в образе |
| Секрет через текст команды (here-doc в `--run-command`) как запасной путь | `virt-customize -x`/`-v` печатает аргументы команд в трассу, которую bisquite пишет в журнал |
| `EXTENSION` как сахар (разворачивается в `COPY_IN` + `RUN`) | bisquite снова знает пути cream, имя бутстрапа и флаги `apply`; смена любого — выпуск bisquite |
| `bs vmfile migrate` | одноразовая утилита ради ~30 файлов одного владельца; ручная правка в плане дешевле поддержки и удаления в 4.0 |
| `cream apply` на хосте, результат копируется (`build: files`) | требует раскладки без запуска гостя — зарезервировано в cream-core §19 В4, хуки большинства расширений исполняемые |
| Один шаг на расширение (сохранить per-extension слои через `apply --only name`) | автор может это сделать сам несколькими файлами (§8.5); навязывать — вернуть N запусков appliance и N бутстрапов |
| Бутстрап голым бинарём на всех семействах | ломает `.deb` расширений с `Depends: cream` (§7.3) |
| cream в образе openwrt-builder | связывает выпуск openwrt-builder с выпуском cream; бутстрап по пину проще |

## 15. Риски
1. **Секрет через `--upload` в tmpfs appliance** (вероятность ~30 %, что путь уйдёт на диск гостя): libguestfs
   выполняет `--run-command` в chroot `/sysroot`, а `--upload` — через свой API; tmpfs, смонтированный командой,
   должен быть виден загрузке, но это надо доказать тестом «маркер `scope: build` не в raw». Смягчение — тест первым
   днём плана bisquite; если не проходит — `--mount=type=secret` выпадает из 3.0.0 (секреты сборки только на живой
   машине) до проверенного способа. Обхода через текст команды нет (§14).
2. **Инвариант пина Stvor** (§10): образ с cream новее пина портала не регистрируется. Смягчение — шаг 2 §11.1 до
   первых образов, `cream-bump` в Stvor остаётся, приёмка `robots-gate` на образе из `apply`.
3. **Сеть в appliance для бутстрапа и `source sync`**: офлайн-машины сборки и корпоративные прокси. Смягчение —
   `--from` бутстрапа и источник `type: path` с `digest`, скопированные тем же `COPY_IN`; guide описывает
   офлайн-рецепт.
4. **Потеря тонкой инвалидации** (смена параметра одного расширения — весь `apply`): на тяжёлых образах (Jetson,
   `nvidia-jetpack`) — часы. Смягчение — несколько файлов состояния и `FROM`-цепочки в примерах Jetson; реальный
   выигрыш даст только материализованный кеш шагов (отложенная спека bisquite).
5. Каталог базы `FROM` старше файла потомка: `apply` приведёт источник к пину потомка и сделает `upgrade` тех
   расширений, что перечислены в файле; неперечисленные остаются на версии базы. Это ожидаемо, но неочевидно.
   Смягчение — `apply` печатает план (`--dry-run`-форму) перед исполнением в журнал шага.

## 16. Открытые вопросы (владелец — с рекомендацией)
| № | Вопрос | Рекомендация |
|---|---|---|
| В1 | Утвердить направление «bisquite строит, cream ставит» и breaking bisquite 3.0.0 без переходного периода? | **Да**: внешних потребителей нет, связка дорожает с каждым выпуском |
| В2 | `EXTENSION`: удалить (отказ-подсказка, ручная миграция примеров) или оставить сахаром? | **Удалить** (§8.4) |
| В3 | Секрет, который расширение хранит на машине, задан при сборке: запрещать (`apply --phase build` — отказ) или только предупреждать? | **Предупреждать** в 1.2.0 (`secret.persisted_in_image` в выводе), запрет — по опыту; цель §1 на такие ключи не распространяется |
| В4 | Сужать openwrt-builder до «голой базы» (podkop, Argon → cream)? | **Нет, пока**: kmod AmneziaWG привязан к ядру и должен остаться в образе; podkop/Argon переносить, когда понадобятся машины без них |
| В5 | Порядок: `apply` (1.2) раньше OpenWrt-семейства (1.3) или вместе? | **Раньше и отдельно**: `apply` разблокирует bisquite 3.0 на Debian/Ubuntu; OpenWrt-ядро — следом минором |
| В6 | Подпись желаемого состояния (cosign/«защищённый файл») | **Не сейчас**; при появлении раздачи `cream.yaml` порталом — ed25519 detached на stdlib, отдельная спека (§14) |
| В7 | Переводить скрипт Stvor на `cream apply`? | **Нет** в рамках этой спеки; кандидат бэклога Stvor после 1.2 (§10) |

## 17. Позже (отложено рулингом после skeptic)
| Что | Когда возвращаться |
|---|---|
| `cream.lock` (`cream lock`, `--update`, `--check`, `--locked`, канонический JSON) | когда ручной подъём пинов в `cream.yaml` станет заметной работой; минор cream после 1.2 |
| `--prune`, `--state-name`, `managed_by` источников и расширений | когда появится сценарий «держать состояние» на живой машине (удалять лишнее) |
| `--phase auto` | после опыта с явной фазой; детекция `virt-customize`-гостя должна быть громким отказом, а не тихим `live` |
| `ANNOTATE` в bisquite (файл гостя → OCI-аннотация) | если `virt-cat` по требованию окажется неудобен для web UI или CI |
| `bs vmfile migrate` | не планируется: внешних потребителей нет |
| Сборки cream под macOS для автора | не планируется: контейнер `ghcr.io/iamletenkov/cream` и Linux-бинарь |
