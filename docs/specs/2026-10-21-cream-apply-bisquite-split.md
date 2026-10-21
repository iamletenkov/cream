# `cream apply` и разделение bisquite ↔ cream: «bisquite строит образы, cream ставит софт»

> **Черновик на утверждение владельца, 2026-10-21.** Плана реализации нет; план пишется после решений по §16.
> Вход: направление владельца «bisquite строит образы, cream ставит софт» (обсуждаемое). Код на момент написания:
> cream `1ea3017` (1.1.4), bisquite `2dcf890` (2.2.0, `origin/main`), cream-extensions `8e9d63f` (5.2.0),
> openwrt-builder `v0.3.0` (`206df8a`), Stvor `e1262de7`. Связанные документы: спека
> [cream-core](2026-10-18-cream-core.md) (§0 п. 3 и §12 эта спека **пересматривает**), спека bisquite
> `docs/specs/2026-10-19-extensions-via-cream.md` (её §4–§5 уходят целиком), отложенная спека OpenWrt
> (`docs/specs/2026-10-20-openwrt.md`, ветка `docs/openwrt-spec`, PR #16 — из неё берётся только ядро cream, без
> Teleport и Stvor).

## 0. Вход и что пересматривается
**Направление владельца (на утверждение):**
1. bisquite — только сборщик: `FROM`, копирование файлов, выполнение команд, кеш по шагам. Ни каталога, ни версий
   расширений, ни cream на хосте, ни зависимости `cream` в `.deb`.
2. cream — провижнер **внутри гостя**: декларативный `cream apply -f cream.yaml` (желаемое состояние) и
   `cream.lock` (точные версии и дайджесты, `cream lock`) для воспроизводимости.
3. Секреты сборки — монтированием на время одного шага (как `RUN --mount=type=secret` в BuildKit), не файлами в
   слоях.
4. cream попадает в гостя сам (бутстрап по URL+sha256 из lock), без участия хоста.
5. Семейство OpenWrt 25.12 — cream и первые расширения (без Teleport и Stvor).

**Что этим отменяется в действующих решениях:**
| Документ | Пункт | Было | Станет |
|---|---|---|---|
| cream-core §0 п. 3 | bisquite и `EXTENSION` | bisquite исполняет `EXTENSION` через cream, хост разрешает офлайн | bisquite про cream не знает; `EXTENSION` удаляется (§8.4) |
| cream-core §12 | фаза build | `cream resolve` на хосте + `install --phase build --from-dir --record` в госте | `cream apply --phase build` в госте; `resolve`/`--from-dir` остаются только для совместимости 1.x (§4.8) |
| cream-core §3 | цепочка выпусков | `cream-bump`, `extensions-bump` → bisquite | bisquite из цепочки выпадает (§11.3) |
| bisquite spec 2026-10-19 §4.1 | `Depends: cream (= пин)` | точная версия на хосте | зависимости нет |
| cream-core §0 п. 1 | поставка | `.deb` | `.deb` (deb-семейство) **и** `tar.gz` (OpenWrt и бутстрап) — tar уже выпускается |

Нужен ADR в cream: «формат `cream.yaml`/`cream.lock` и команда `apply` — часть контракта cream/v1» (номер — при
слиянии; ADR-0003 занят черновиком OpenWrt). В bisquite — ADR «bisquite не знает о cream» (breaking 3.0.0).

## 1. Цели и не-цели
**Что должно стать верным:**
- `bs image build` на хосте без cream собирает образ с расширениями: в VMFILE — копирование `cream.yaml` и
  `cream.lock` и одна команда `cream apply`.
- Одинаковые `cream.yaml` + `cream.lock` + база дают одинаковый набор расширений (версии, дайджесты деревьев,
  параметры) на любой машине сборки и на живой машине.
- Выпуск cream или каталога **не требует** выпуска bisquite. Выпуск bisquite не требует подъёма пинов cream.
- Секрет сборки не попадает ни в argv, ни в журнал сборки, ни в qcow2 (включая свободные блоки), ни в метки.
- Тот же `cream apply -f` работает на живой машине (Debian/Ubuntu, затем OpenWrt 25.12).
- Скрипт Stvor (`cream install … teleport`, `cream run teleport join`) работает без изменений.

**Не-цели:**
- Решатель версий: как и в cream-core, у источника одна версия расширения; lock фиксирует найденное.
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
  (`linux.py`, цикл слоёв): смена содержимого копируемого файла **не меняет** `chain_digest`. Только у cream-слоя
  дайджест содержательный (`token + params_digest`).
- **Один `virt-customize` на расширение.** Десять `EXTENSION` — десять запусков appliance и десять `dpkg -i` cream.
- **OpenWrt закрыт.** Резолвер на хосте, `dpkg` пакета в госте и `family: deb` делают `EXTENSION` на OpenWrt
  невозможным без новой ветки в bisquite.
- Материализованного кеша шагов **нет** (отложенная спека): падение на шаге N — сборка с нуля. Практика —
  цепочки `FROM` (`dst-base → dst-desktop → dst-robot-ws`).

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
 автор образа                        машина сборки (bisquite 3.0, cream НЕ нужен)            гость (virt-customize)
 ─────────────                       ───────────────────────────────────────────            ──────────────────────
 cream.yaml ──cream lock──► cream.lock                                                          
 (где угодно, где есть cream)        VMFILE:                                                    
                                       FROM base                                              
                                       UPLOAD cream/:/etc/cream/apply  ── ключ: sha256 файлов ─► /etc/cream/apply/{cream.yaml,cream.lock}
                                       RUN_COMMAND --mount=type=secret,id=… \                 
                                         sh /etc/cream/apply/cream-bootstrap.sh && \                ─► cream-bootstrap: .deb / tar.gz по lock, sha256
                                         cream apply --phase build --locked -f …              ─► cream source sync (сеть appliance) → граф по
                                                                                                 реестру гостя → install/upgrade → реестр
 Stvor (живой робот): curl … | sudo bash → dpkg -i cream → cream install teleport …   (без изменений)
 живая машина (опция):  cream apply -f cream.yaml [--secret id=…,src=…]
```
| Кто | Отвечает за | Не знает о |
|---|---|---|
| bisquite | база, копирование, команды, секрет-монтирование на шаг, ключи шагов, lineage шагов, хранилище, деплой | cream, каталоге, версиях расширений, `cream.yaml` |
| cream | желаемое состояние, lock, источники, граф, установка, реестр, бутстрап самого себя | bisquite, qcow2, OCI-метках |
| cream-extensions | расширения и снимки каталога | bisquite |
| автор образа | `cream.yaml`, `cream.lock` (командой `cream lock`), VMFILE | — |

## 4. `cream apply`
### 4.1 Файл желаемого состояния `cream.yaml`
```yaml
schema: cream-apply/v1             # отдельная схема в контракте cream/v1 (минорное расширение, ADR-0001)
cream: ">=1.2.0 <2.0.0"            # какой исполнитель годится; конкретную версию и sha256 фиксирует lock
sources:                           # как sources.yaml cream-core §9.1; порядок — приоритет
  - name: core
    type: archive
    url: https://github.com/iamletenkov/cream-extensions/releases/download/v5.2.0/cream-extensions-5.2.0.tar.gz
    sha256: 21121c16…              # обязательно для archive; git — commit или ref (lock закрепит commit)
extensions:                        # порядок — подсказка, не закон: устанавливает граф (§4.3)
  - name: cloud-user
  - name: desktop
    params: {DESKTOP_AUTOLOGIN: "1"}
  - ref: core/gnome@4.2.0          # короткая форма: [source/]name[@version] — @version утверждение (66 при расхождении)
  - name: some-ext
    params:
      API_TOKEN: {secret: api_token}   # значение — из секрета сборки/запуска (§6), не из файла
options:
  with_deps: false                 # true — добирать depends/requires как --with-deps (единственный поставщик)
  accept_risk: []                  # имена расширений с targets.live: confirm, разрешённые явно
```
Правила:
- Разбор строгий (`KnownFields`), как у манифестов. Литерал значения ключа, который манифест объявил `secret`,
  — отказ 64 `secret.in_file` (зеркало `secret.in_argv`): файл едет в образ.
- Источники `apply` регистрируются в `/etc/cream/sources.yaml` под своими именами с пометкой `managed_by: apply`
  (поле реестра источников, не `cream.yaml`): потом работают `cream upgrade`, `cream source list`. Источник с тем же
  именем и другими полями, добавленный руками, — отказ 69 `source.conflict` без `--replace-sources`.
- Параметры — те же ключи `config.keys`, что у `cream install K=V`; запись в домен настроек — тем же
  `internal/config` (типы, readonly, хуки `apply`/`secret`).

### 4.2 Семантика
- **Аддитивно по умолчанию:** «эти расширения должны стоять с этими версиями и параметрами». Остальное
  установленное не трогается. Так `apply` складывается по цепочке `FROM` и по нескольким файлам (§8.6).
- `--prune` — удалить расширения с `managed_by: apply` из того же **имени состояния** (`--state-name`, по
  умолчанию имя файла без `.yaml`), которых больше нет в файле. Руками поставленное (`cream install`) `prune` не
  трогает. В фазе build `--prune` запрещён (64): образ строится только добавлением.
- Действие по каждому расширению: нет — `install`; другая версия в источнике — `upgrade` (понижение — только
  `--allow-downgrade`, как в cream-core); те же версия, `ext_digest` и `params_digest` — ничего (`changed: false`);
  другие параметры — `set` + хук `apply` расширения (как `cream set --apply`), без переустановки.
- Идемпотентность: повтор с тем же файлом — код 0, `changed: false` у всех.
- `--dry-run` — план действий (`install`/`upgrade`/`set`/`noop`/`remove`) без изменений, `--json` для машин.

### 4.3 Порядок и граф
Граф строится тем же `internal/resolve` на множестве «реестр машины ∪ файл». Порядок — топологический (поставщик
раньше потребителя), независимые — по порядку в файле (а не по имени, как сейчас в `resolve`: bisquite это мешало,
см. `extension_router.py`). Зависимости, стоящие в базе `FROM`, закрываются **реестром гостя** — флаг
`--installed` и метки `bisquite.lineage.cream` больше не нужны.

### 4.4 Фазы
`--phase build|live|auto` (умолчание `auto`): `auto` выбирает `build`, если PID 1 не systemd/procd или
`systemctl is-system-running` отвечает `offline` (так выглядит гость `virt-customize`). В `build` — `policy-rc.d`
(deb), никаких restart, `targets.build` проверяется как сейчас; хук, вернувший 20 в build, — `skipped` только с
`allow_skip` в файле. VMFILE в примерах пишет `--phase build` явно (§16 В5).

### 4.5 Вывод и коды
Коды — таблица cream-core §11.3 без новых номеров; новые строковые коды: `lock.stale`, `lock.missing`,
`lock.mismatch`, `secret.missing`, `secret.in_file`, `source.conflict`, `apply.prune_in_build`. `--json` —
`{"plan": [...], "results": [...], "lock": {"digest": "…"}, "state": "<имя>"}`; формы закрепляются golden-тестами,
как остальные команды. После успеха `apply` пишет `/etc/cream/applied/<имя состояния>.json`: digest файла, digest
lock, итоговые токены — это lineage для любого внешнего инструмента (§8.7), читаемый без cream (`cat`).

### 4.6 Место в коде cream
| Новое | Что |
|---|---|
| `internal/applyspec` | разбор и проверка `cream.yaml` |
| `internal/pinlock` | формат `cream.lock`, канонический JSON, проверка свежести (имя не `lock` — `internal/lock` уже `flock`) |
| `internal/apply` | план (diff реестр ↔ желаемое), исполнение через `engine.Install/Upgrade/Set/Remove` под одним `lock/global` |
| `internal/secretmount` | чтение секретов из каталога (§6) с проверкой прав |
| `internal/cli/cmd_apply.go` | `apply`, `lock` |
| `scripts/cream-bootstrap.sh` → ассет релиза | §7 |
Меняется: `internal/source` (источники с `managed_by`, синхронизация по пину lock без сети, если кеш совпал),
`internal/resolve` (порядок независимых — по входу), `packaging/nfpm.yaml` (бутстрап-скрипт в `/usr/share/cream/`
не нужен — он в релизе отдельным ассетом). Не меняется формат манифеста `cream/v1`.

### 4.7 Пример живой машины
`sudo cream apply -f /etc/cream/apply/robot.yaml --secret id=api_token,src=/root/token` — тот же файл, что в
сборке; секрет — файл или `env=`, не argv.

### 4.8 Что остаётся от интерфейса bisquite в cream 1.x
`cream resolve` и `install --phase build --from-dir --record` **не удаляются в 1.x** (контракт, ADR-0001), но
помечаются устаревшими в `docs/cli.md`; их потребитель (bisquite 2.x) остаётся на старом пине. Удаление — cream
2.0. Фикстуры контракта для bisquite (`tests/fixtures/cream/<версия>/` в bisquite) перестают обновляться.

## 5. `cream.lock`
### 5.1 Содержимое
Канонический JSON (ключи по алфавиту, отступ 2, каждое скалярное значение на своей строке — это контракт для
бутстрапа, §7.2):
```json
{
  "bootstrap": {
    "linux-amd64-deb":   {"sha256": "…", "url": "https://github.com/iamletenkov/cream/releases/download/v1.2.0/cream_1.2.0_amd64.deb"},
    "linux-amd64-tar":   {"sha256": "…", "url": "…/cream_1.2.0_linux_amd64.tar.gz"},
    "linux-arm64-deb":   {"sha256": "…", "url": "…"},
    "linux-arm64-tar":   {"sha256": "…", "url": "…"}
  },
  "cream": "1.2.0",
  "extensions": [
    {"deps": ["desktop@4.1.0"], "ext_digest": "sha256:…", "name": "gnome", "params_digest": "sha256:…",
     "source": "core", "token": "core/gnome@4.2.0#21121c164c92", "version": "4.2.0"}
  ],
  "input_digest": "sha256:<канонизированный cream.yaml>",
  "lock_version": 1,
  "sources": [{"catalog_version": "5.2.0", "name": "core", "resolved": "sha256:21121c16…", "type": "archive"}]
}
```
- `extensions` — **замыкание**: явно перечисленные плюс добранные `with_deps`, в порядке установки.
- `params_digest` — от несекретных параметров; секретные входят как `secret:<id>` (имя, не значение).
- `bootstrap` — из `SHA256SUMS` релиза cream версии `cream`; зеркало — `--mirror URL` у `cream lock`
  (Stvor-подобные офлайн-стенды), тогда `url` — на зеркало, sha256 те же.

### 5.2 Команды
- `cream lock [-f cream.yaml] [--out cream.lock] [--update [name…]] [--cream-version V] [--mirror URL]` — синхронизирует
  источники во **временный** кеш (не системный, root не нужен), разрешает граф для каждой пары
  `family × arch`, объявленной в файле (`platforms: [{family: deb, arch: amd64}]`, умолчание — `deb/amd64` и
  `deb/arm64`), пишет lock. Без `--update` существующий lock меняется только в части, которую требует изменение
  `cream.yaml` (как `npm install`/`cargo`): поднять каталог — `--update`.
- `cream lock --check` — свежесть (`input_digest`) и доступность всех URL (HEAD), для CI автора; без сети —
  только свежесть.
- `cream apply --locked` — lock обязателен и свеж; иначе 65 `lock.missing`/`lock.stale`. Без `--locked` и без
  lock — разрешение «как получится» с предупреждением `lock.missing` (для живых машин и экспериментов).
- `apply` с lock сверяет: sha256 архива источника = `resolved`, `ext_digest` дерева = lock, версия cream ≥ `cream`
  lock в мажоре; расхождение — 69 `lock.mismatch` (подмена каталога ловится).

### 5.3 Где запускать `cream lock`
cream — статический Linux-бинарь; `cream lock` не требует root и не трогает `/etc`. Автор запускает его там, где
есть cream: на машине сборки (по желанию, это не зависимость bisquite), в контейнере `ghcr.io/iamletenkov/cream`
(новый тривиальный образ из релиза), в CI. macOS-сборку не делаем (§16 В7).

## 6. Секреты сборки и запуска
### 6.1 В cream
- Ссылка в `cream.yaml`: `KEY: {secret: <id>}`; `id` — `[a-z0-9_]{1,64}`.
- Источник значения при `apply`: каталог `--secrets-dir` (умолчание `/run/secrets`, конвенция BuildKit/Compose)
  файл `<id>`; на живой машине ещё `--secret id=…,src=FILE|env=VAR`. Нет файла — 66 `secret.missing` до любых
  изменений.
- Значение идёт в домен через существующий путь секретов (`KEY=-`/хук `secret`, файл `secrets` 0600), в argv и
  журнал — нет (фильтр `internal/runner/filter.go` получает значения для вымарывания).
- Что в образе после сборки: файл `secrets` домена — **если расширение хранит секрет на машине**, это его
  контракт (как сейчас). Для секретов, нужных только на время сборки (токен приватного зеркала), манифест
  объявляет ключ `scope: build` (новое необязательное поле `config.keys`, минорное) — cream передаёт его хуку
  только переменной окружения и не пишет в домен.

### 6.2 В bisquite (универсально, без знания о cream)
- CLI: `bs image build --secret id=api_token,src=./token|env=API_TOKEN` (многократно). Web: только ссылки на
  файлы в `/etc/bisquite/secrets.d/<id>` (значения через браузер не принимаются).
- VMFILE: `RUN_COMMAND --mount=type=secret,id=api_token[,target=/run/secrets/api_token] <команда>`; без `target` —
  `/run/secrets/<id>`. Секрет, не переданный в CLI, — отказ до сборки; переданный и не использованный —
  предупреждение.
- Механика в `virt-customize` (один вызов на шаг): `--run-command 'mkdir -p /run/secrets && mount -t tmpfs -o
  mode=0700,size=1m bisquite-secrets /run/secrets'`, затем `--upload <host-tmp>:/run/secrets/<id>` (запись идёт в
  tmpfs ядра appliance, на диск гостя не попадает), затем команда шага, затем
  `--run-command 'umount /run/secrets; rmdir /run/secrets 2>/dev/null || true'`. Временный файл на хосте — 0600 в
  каталоге 0700, удаляется в `finally`.
- **Проверка обязательна до принятия** (риск №1, §15): тест «маркер секрета не встречается в сыром qcow2
  (`qemu-img convert -O raw` + `grep -c`), в журнале сборки и в OCI-манифесте». Если `--upload` пишет мимо tmpfs
  (appliance монтирует гостя в `/sysroot`, и путь может разрешиться на диск), — запасной путь: секрет передаётся
  через `--run-command` с here-doc из файла 0600, загруженного в tmpfs самого appliance (`/tmp` appliance), с тем же
  тестом.
- Ключ шага включает `id` секретов, **не** значения (как BuildKit): смена секрета не инвалидирует шаг. Это
  документируется; `--no-cache-step N` пересобирает принудительно.

## 7. Доставка cream в гостя (бутстрап)
### 7.1 `cream-bootstrap.sh`
POSIX `sh` (годится для `ash` busybox), ассет релиза cream рядом с бинарями и в `SHA256SUMS`. Вызов:
`sh cream-bootstrap.sh --lock /etc/cream/apply/cream.lock [--from DIR] [--mirror URL]`.
1. Семейство и архитектура: `/etc/os-release` (`ID`/`ID_LIKE`), `uname -m` → `amd64|arm64`.
2. Формат: deb-семейство → `-deb` (пакет нужен в dpkg: `stvor-agent` и другие `.deb` расширений объявляют
   `Depends: cream`); openwrt → `-tar` (распаковка в `/usr/bin`, `cream system init`).
3. Пин — из `bootstrap.<linux-arch-fmt>` lock (разбор `awk` busybox по построчному JSON, §5.1).
4. Уже стоит cream: та же версия — ничего; старше — обновление; **новее** — отказ (код 3) «база собрана с cream
   новее lock — поднимите lock» (как сейчас `dpkg --compare-versions` в bisquite).
5. Загрузка: `--from DIR` (офлайн: файл заранее скопирован шагом `UPLOAD`) → `--mirror` → `url`; `curl`, иначе
   `wget`/`uclient-fetch`; sha256 сверяется **до** установки (`sha256sum -c`).
6. Скрипт сам пинуется: VMFILE берёт его из релиза с проверкой sha256 (пример §11.2) или кладёт копию рядом с
   `cream.yaml`; `cream lock` пишет `cream-bootstrap.sh` рядом с lock (`--with-bootstrap`), чтобы шагу `UPLOAD`
   хватило одного каталога.
Сеть: шаг идёт в сети appliance `virt-customize`, как любые `apt-get`/хуки сейчас. Для офлайн-сборки — `--from`.

### 7.2 Почему не статический бинарь без пакета
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
2. **Секреты** — §6.2: флаг `--secret`, опция `--mount=type=secret` у `RUN_COMMAND`/`RUN_SCRIPT`, проверка
   «секрет не в образе».
3. **Объединение шагов** не нужно: `cream apply` — один шаг на все расширения.
4. **Аннотация из гостя (общая):** директива `ANNOTATE <ключ> @<путь в госте>` — после сборки bisquite читает файл
   (`virt-cat`, одна загрузка appliance на все `ANNOTATE`) и кладёт в OCI-аннотацию `<ключ>`; ≤ 64 КиБ. Для cream:
   `ANNOTATE io.cream.applied @/etc/cream/applied/image.json` — lineage без знания о cream (§8.7, §16 В4).

### 8.3 Пример VMFILE после
```
FROM ubuntu2404:dst-base
LABEL os=ubuntu
LABEL arch=amd64
RESIZE size=16G device=/dev/sda idx=1
UPLOAD cream/:/etc/cream/apply
RUN_COMMAND sh /etc/cream/apply/cream-bootstrap.sh --lock /etc/cream/apply/cream.lock && cream apply --phase build --locked --state-name image -f /etc/cream/apply/cream.yaml
ANNOTATE io.cream.applied @/etc/cream/applied/image.json
```
`cream/` рядом с VMFILE: `cream.yaml`, `cream.lock`, `cream-bootstrap.sh` (`cream lock --with-bootstrap`). Секрет:
`RUN_COMMAND --mount=type=secret,id=mirror_token sh … && cream apply …` и `bs image build --secret id=mirror_token,env=MIRROR_TOKEN`.

### 8.4 `EXTENSION` — удалить
Директива удаляется; парсер отвечает отказом с подсказкой (как сейчас для `UPDATE` → `UPGRADE`, словарь
переименований в `vmfile_parser.py`): «`EXTENSION` удалена в bisquite 3.0: расширения ставит cream в госте —
см. docs/guides/migration-3.0.md; `bs vmfile migrate` перепишет файл». Сахар, разворачиваемый в `UPLOAD` + `RUN`,
отвергнут (§14): он возвращает в bisquite знание о путях cream, имени бутстрапа и флагах `apply`, то есть связку
по версиям в мягкой форме.
**`bs vmfile migrate <VMFILE>`** — одноразовая офлайн-утилита только по синтаксису: собирает подряд идущие
`EXTENSION [src/]name[@v] K=V` в `cream/cream.yaml` (источники — из старого `extensions.yaml`, если он есть), ставит
`UPLOAD` + `RUN_COMMAND` на место первой директивы группы, печатает «запустите `cream lock`». Каталог не читает.
Удаляется в 4.0.

### 8.5 Кеш и инкрементальность
- Ключ шага `RUN_COMMAND … cream apply` = chain(ключ базы, ключ `UPLOAD cream/` = отпечаток `cream.yaml` +
  `cream.lock` + бутстрап, строка команды, id секретов). То есть ровно «хеш cream.yaml + cream.lock + базы».
  Без `--locked` ключ невоспроизводим по смыслу (каталог мог измениться); bisquite этого не знает и знать не должен
  — `--locked` в примерах и в guide обязателен, `cream apply` без lock в build-фазе печатает предупреждение.
- **Что теряем:** при per-extension слоях смена параметра одного расширения пересобирала (логически) хвост от
  этого слоя; теперь — весь шаг `apply`. Реального выигрыша сейчас не было: материализованного кеша нет (§2.2),
  а все слои всё равно применялись заново.
- **Чем заменяем:** (1) несколько файлов состояния — `cream/base.yaml`, `cream/desktop.yaml` с отдельными lock и
  отдельными шагами `RUN … cream apply -f` (аддитивная семантика §4.2 это разрешает; разбиение — решение автора, как
  разбиение `RUN` в Dockerfile); (2) цепочки `FROM`, как сейчас; (3) внутри одного `apply` cream не переставляет
  уже стоящее (`noop`), поэтому повторная сборка от образа-потомка дешёвая.
- Когда появится материализованный кеш шагов (отложенная спека), он ляжет на эти ключи без изменений в cream.

### 8.6 `bs extension sync/sources/ls/check`
Удаляются вместе с группой `bs extension`. Замены: `cream lock` (синхронизация и проверка на машине автора),
`cream lock --check` (CI), `cream apply --dry-run` в госте (что будет сделано). bisquite-подсказки «выполните
`bs extension sync`» исчезают.

### 8.7 Lineage и web UI
- Метки `bisquite.lineage.extensions`/`bisquite.lineage.cream` больше не пишутся; `bisquite.lineage.root` и
  цепочка шагов остаются. Список установленного — аннотация `io.cream.applied` (через `ANNOTATE`) и
  `cream list --json` в живом образе.
- Образы 2.x с метками `bisquite.lineage.cream` читаются (метки показываются как любые), но как база для 3.0
  ничего не дают: зависимости закрывает реестр гостя.
- **Web UI** (`bs web`): страницы сборки в UI нет, поэтому затронуто немногое — (1) self-check без компонента
  cream; (2) детали образа показывают аннотацию `io.cream.applied` как JSON (общий показ аннотаций);
  (3) просмотр VMFILE (`/api/images/{ref}/vmfile`) подсвечивает `--mount=type=secret` и `ANNOTATE`; (4) файлы
  контекста шага `UPLOAD` (`cream.yaml`, `cream.lock`) сохраняются вместе с VMFILE в артефакт сборки и доступны
  тем же эндпоинтом (`/api/images/{ref}/context/{path}`), иначе по образу не восстановить состояние. Секреты в UI —
  только ссылки (§6.2).

### 8.8 OpenWrt в bisquite
`OpenWrtBuilder` снимает запрет `EXTENSION` естественно (директивы нет); `RUN_COMMAND` уже разрешён — `cream apply`
работает, как только cream поддерживает семейство `openwrt` (§9). Блокировки `INSTALL/UNINSTALL/FIRSTBOOT*` не
меняются.

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
| `amneziawg` | **только настройка**: интерфейс и пиры через `uci` (`network.<if>`, `proto amneziawg`), параметры обфускации (`Jc`, `Jmin`, `Jmax`, `S1`, `S2`, `H1`–`H4`), приватный ключ — `secret`; `check` — `awg show` | `kmod-amneziawg` привязан к `vermagic` ядра релиза и таргета — он уже в образе openwrt-builder v0.3.0 (пины по таргетам); ставить kmod из cream значит дублировать матрицу пинов. `platform.commands: [awg, uci]` — без пакета расширение не ставится (68) |
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
- Сужение образа до «совсем голой базы» (вынести podkop/Argon в cream) — открытый вопрос §16 В6.

## 10. Влияние на Stvor и stvor-agent
- **Ничего не ломается:** скрипт установки (`backend/app/modules/teleport/install_script.py`) делает `dpkg -i`
  cream по пину `.env` (`CREAM_VERSION`, `CREAM_SHA256_*`), `cream source add stvor --archive … --sync`,
  `cream install --source stvor teleport …`, `cream run teleport join` (секрет на stdin), `cream install
  stvor-agent`. Все эти команды в cream 1.x остаются; `apply` их не заменяет.
- `stvor-agent` зовёт `cream run teleport set-managed` — без изменений.
- **Опция, не требование:** скрипт может перейти на `cream apply -f robot.yaml` (`teleport` + `stvor-agent` с
  параметрами пина), секрет регистрации — `--secret id=registration,src=-`… Но `join` — действие, а не установка:
  его по-прежнему делает `cream run teleport join`. Выигрыш мал — переход только по отдельной задаче Stvor.
- Образы роботов, собранные bisquite 3.0 с `cream apply` (teleport в фазе build без `join`), ведут себя как
  сейчас образы с `EXTENSION teleport`: регистрация — командой портала на живой машине.
- Цепочка пинов Stvor (`CREAM_*`, `CREAM_EXTENSIONS_*`) не меняется; `cream-bump.yml` по-прежнему открывает PR в
  Stvor и cream-extensions.

## 11. Миграция и выпуски
### 11.1 Порядок
| Шаг | Репозиторий | Версия | Что |
|---|---|---|---|
| 1 | cream | **1.2.0** (feat) | `apply`, `lock`, `--secret`/`--secrets-dir`, `scope: build`, `cream-bootstrap.sh` в релизе, образ контейнера; `resolve`/`--from-dir` помечены устаревшими; ADR |
| 2 | cream-extensions | минор | CI: `cream lock --check` на примерах; `tools/cream-validate.sh` знает `cream.yaml` |
| 3 | bisquite | **3.0.0** (`feat!`) | §8.1 удаление, §8.2 добавление, `bs vmfile migrate`, примеры, guide; `Depends: cream` снят |
| 4 | cream | **1.3.0** (feat) | семейство `openwrt` (§9.1), ADR семейства |
| 5 | cream-extensions | минор | корень `extensions/openwrt`, снимок `-openwrt`, `amneziawg`, `podkop`, `luci-apps` |
| 6 | bisquite | 3.x | пример `examples/build/amd64/openwrt` с `cream apply` |
Шаги 1–3 не зависят от OpenWrt; 4–6 — после 1.

### 11.2 Примеры bisquite
Каждый `EXTENSION`-пример (`examples/dst/*`, `examples/build/jetson/nano/*`, `amd64/ubuntu2204/gnome`,
`debian12/lxde`, …) получает каталог `cream/` с `cream.yaml`, `cream.lock`, `cream-bootstrap.sh`. Lock примеров
поднимает ручной `make examples-lock` (контейнер cream из релиза; cream не становится зависимостью bisquite,
только инструментом CI/разработки), CI проверяет `cream lock --check` в том же контейнере.

### 11.3 Цепочка выпусков
- `cream-bump.yml` больше не открывает PR в bisquite; `extensions-bump` в bisquite удаляется.
- bisquite CI больше не скачивает `.deb` cream; `make test-build` переписывается на общий шаг: пример с
  `cream apply` на настоящем `virt-customize` (контейнер cream — только для `cream lock`).

### 11.4 Guide миграции (`bisquite: docs/guides/migration-3.0.md`)
1. Поставить cream где угодно (или контейнер), `bs vmfile migrate VMFILE` → `cream/cream.yaml`.
2. Проверить параметры; секретные — заменить на `{secret: id}`, в VMFILE добавить `--mount=type=secret`.
3. `cream lock -f cream/cream.yaml --with-bootstrap`; закоммитить `cream/`.
4. Собрать; сверить `cream list` в госте с прежней меткой `bisquite.lineage.extensions` базы.
5. Убрать `extensions.yaml` (`/etc/bisquite`, `$XDG_CONFIG_HOME/bisquite`) — bisquite 3.0 на него предупреждает.
6. Удалить пакет cream с машины сборки (если не нужен для `cream lock`).
Реальных потребителей вне владельца нет — переходной период (две ветки в bisquite) не нужен.

## 12. Безопасность
- Граница доверия прежняя: источник и его пин (sha256 архива/коммит). lock добавляет пин **дерева каждого
  расширения** и **бинаря cream** — подмена каталога или релиза ловится в госте.
- Хуки — root-код из источника, как раньше. `apply` не расширяет права.
- Секреты: §6; тест «маркер не в qcow2/журнале/манифесте» — в CI bisquite (`make test-build`) и в e2e cream.
- `cream.yaml` и `cream.lock` едут в образ (`/etc/cream/apply`) и в аннотации — значения секретов там запрещены
  проверкой `secret.in_file`.
- Бутстрап на чужом `--mirror`: sha256 — из lock, зеркало доверия не получает.

## 13. Тестирование и приёмка
| Уровень | Что |
|---|---|
| cream unit | разбор `cream.yaml` (golden-ошибки), канонический lock (байтовая стабильность), план `apply` (install/upgrade/set/noop/prune), свежесть lock, `secret.in_file`, `auto`-фаза |
| cream e2e (`test/build`) | `virt-customize` на Debian 12: бутстрап по lock из `file://`, `apply --phase build --locked`, повтор — `changed:false`; база `FROM` с реестром закрывает `depends` |
| cream e2e OpenWrt | (после 1.3) QEMU 25.12 x86-64: бутстрап tar.gz, `apply` `amneziawg` с тестовым ключом, перезагрузка, `cream list` тот же |
| bisquite unit | ключи шагов по содержимому; `--mount=type=secret` разбор и отказы; `EXTENSION` → отказ с подсказкой; `bs vmfile migrate` |
| bisquite `test-build` | пример с `cream apply` + секрет-маркер: маркера нет в raw-qcow2, журнале, манифесте |
| Stvor | `scripts/robots-gate.sh` на роботе с cream 1.2 — без изменений |

## 14. Отвергнутые варианты
| Вариант | Почему нет |
|---|---|
| **Оставить как есть** (`EXTENSION` через cream на хосте) | тройной пин одной версии и выпуск bisquite на каждый выпуск cream и каталога; кеш cream на хосте от того же euid; дайджест шага по строке; OpenWrt требует новой ветки в bisquite; `extension_router` + адаптер ~2000 строк, которые повторяют решения cream |
| **cream на хосте + chroot** в смонтированный образ (`guestmount`) | хуки исполняют бинари гостя — на чужой архитектуре нужен qemu-user (не-цель cream-core); FUSE и root на машине сборки; cream хоста снова связан версией с bisquite; `virt-customize` уже даёт изолированный appliance |
| **Ansible вместо cream** | Python в госте (на чистом OpenWrt нет); нет lock на коллекции/роли с дайджестами; нет реестра установленного и действий с машинным выводом, на которых стоит `stvor-agent` (`cream run teleport set-managed`); второй формат расширений рядом с cream/v1. Ansible может вызывать `cream apply` — не запрещено |
| **Подписанный файл со списком (cosign / «защищённый файл»)** | Что даёт подпись: подлинность **автора** списка. В сборке bisquite автор VMFILE и `cream.yaml` — одно лицо, файлы в одном git; подлинность содержимого (каталог, cream) уже даёт sha256 в lock. cosign keyless тянет Sigstore (Fulcio/Rekor, сеть, `sigstore-go`) — против ADR-0002 (одна зависимость); keyed-cosign/minisign — вопрос раздачи ключей в гостя. Подпись **не шифрует**: секреты в подписанном файле так же видны — задача секретов решается монтированием (§6). Где подпись нужна: если желаемое состояние приходит **извне** (портал Stvor раздаёт `robot.yaml` машинам) — тогда ed25519 detached-подпись на stdlib (`crypto/ed25519`), ключ в `/etc/cream/trusted.d/`; это отложено до такого сценария (§16 В8). Подпись выпусков cream (`SHA256SUMS`) — отдельная задача CI, полезна независимо |
| Зашифрованный файл секретов (sops/age) в образе | ключ расшифровки всё равно надо доставить в сборку — та же задача, что монтирование, плюс шифротекст навсегда в образе и в аннотациях |
| `EXTENSION` как сахар (разворачивается в `UPLOAD` + `RUN`) | bisquite снова знает пути cream, имя бутстрапа и флаги `apply`; смена любого — выпуск bisquite. Миграцию закрывает одноразовый `bs vmfile migrate` |
| `cream apply` на хосте, результат копируется (`build: files`) | требует раскладки без запуска гостя — зарезервировано в cream-core §19 В4, хуки большинства расширений исполняемые |
| Один шаг на расширение (сохранить per-extension слои через `apply --only name`) | автор может это сделать сам несколькими файлами (§8.5); навязывать — вернуть N запусков appliance и N бутстрапов |
| lock в YAML | разбор в `ash` busybox без `yq` ненадёжен; канонический построчный JSON читается `awk` (§7.1) и детерминирован `encoding/json` |
| Бутстрап голым бинарём на всех семействах | ломает `.deb` расширений с `Depends: cream` (§7.2) |
| cream в образе openwrt-builder | связывает выпуск openwrt-builder с выпуском cream; бутстрап по lock проще и пинуется |

## 15. Риски
1. **Секрет через `--upload` в tmpfs appliance** (вероятность ~30 %, что путь уйдёт на диск гостя): libguestfs
   выполняет `--run-command` в chroot `/sysroot`, а `--upload` — через свой API; tmpfs, смонтированный командой,
   должен быть виден загрузке, но это надо доказать тестом «маркер не в raw». Смягчение — тест первым днём плана,
   запасной путь §6.2.
2. **Сеть в appliance для бутстрапа и `source sync`**: офлайн-машины сборки и корпоративные прокси. Смягчение —
   `--from DIR` бутстрапа и `type: path`/`file://` источников, уже скопированных шагом `UPLOAD`; guide описывает
   офлайн-рецепт.
3. **Потеря тонкой инвалидации** (смена параметра одного расширения — весь `apply`): на тяжёлых образах (Jetson,
   `nvidia-jetpack`) — часы. Смягчение — несколько файлов состояния и `FROM`-цепочки в примерах Jetson; реальный
   выигрыш даст только материализованный кеш шагов (отложенная спека bisquite).
4. Расхождение lock и реестра базы `FROM` (база собрана с другим каталогом): `apply` увидит другую версию
   установленного и сделает `upgrade` — это ожидаемо, но неочевидно. Смягчение — `--dry-run` в выводе сборки
   печатает план до исполнения.

## 16. Открытые вопросы (владелец — с рекомендацией)
| № | Вопрос | Рекомендация |
|---|---|---|
| В1 | Утвердить направление «bisquite строит, cream ставит» и breaking bisquite 3.0.0 без переходного периода? | **Да**: внешних потребителей нет, связка дорожает с каждым выпуском |
| В2 | `EXTENSION`: удалить или оставить сахаром? | **Удалить** с отказом-подсказкой и `bs vmfile migrate` (§8.4) |
| В3 | `--locked` в фазе build — обязателен или предупреждение? | **Предупреждение в cream, обязателен в примерах и guide**; bisquite не проверяет (не знает о cream) |
| В4 | Нужна ли общая директива `ANNOTATE` в bisquite ради lineage? | **Да**: универсальна (подходит любому инструменту в госте), заменяет `bisquite.lineage.cream`; без неё состав образа виден только загрузкой образа |
| В5 | `--phase auto` по умолчанию или только явная фаза? | **auto по умолчанию**, явный `--phase build` в примерах; ошибка детекции — громкий отказ, а не тихий `live` |
| В6 | Сужать openwrt-builder до «голой базы» (podkop, Argon → cream)? | **Нет, пока**: kmod AmneziaWG привязан к ядру и должен остаться в образе; podkop/Argon переносить, когда понадобятся машины без них |
| В7 | Сборки cream под macOS для `cream lock` у автора? | **Нет**: контейнер `ghcr.io/iamletenkov/cream` и Linux-бинарь закрывают сценарий без новой матрицы сборки |
| В8 | Подпись желаемого состояния (cosign/«защищённый файл») | **Не сейчас**; при появлении раздачи `cream.yaml` порталом — ed25519 detached на stdlib, отдельная спека (§14) |
| В9 | Порядок: `apply` (1.2) раньше OpenWrt-семейства (1.3) или вместе? | **Раньше и отдельно**: `apply` разблокирует bisquite 3.0 на Debian/Ubuntu; OpenWrt-ядро — следом минором |
| В10 | Секреты уровня установки в каталоге (`scope: build`) — добавлять поле в `config.keys` сейчас? | **Да, вместе с `apply`**: иначе токен приватного зеркала останется в домене настроек образа |
| В11 | Переводить скрипт Stvor на `cream apply`? | **Нет** в рамках этой спеки; кандидат бэклога Stvor после 1.2 (§10) |
