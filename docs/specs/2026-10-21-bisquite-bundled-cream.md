# bisquite с встроенным cream: vmfile в стиле Dockerfile

> **Редакция 4, 2026-10-21 — по схеме, утверждённой владельцем, с правками по skeptic и рулингами контроллера
> (Р1–Р3, упрощения, §19 закрыт).** План — [docs/plans/2026-10-21-bisquite-bundled-cream.md](../plans/2026-10-21-bisquite-bundled-cream.md).
> Редакции 1–2 предлагали `cream apply -f cream.yaml` с
> бутстрапом cream в госте и bisquite без cream — владелец этот вариант отклонил (§18, первая строка). Код на момент
> написания: cream `0e62232` (`origin/main`, последний выпуск 1.1.4, `upgrade` с планом — PR #23), bisquite
> `30938b7` (2.2.0, `origin/main`), cream-extensions 5.2.0, Stvor `23f8d773`. Связанные документы: спека
> [cream-core](2026-10-18-cream-core.md) (§0 п. 3, §6.2, §6.4 и §12 эта спека **пересматривает**), спека bisquite
> `docs/specs/2026-10-19-extensions-via-cream.md` (её §4–§5 — слои `EXTENSION`, staging, `resolve` на хосте —
> уходят; §4.1 `Depends: cream (= пин)` остаётся), отложенная спека OpenWrt (`docs/specs/2026-10-20-openwrt.md`,
> PR #16).

## 0. Вход и что пересматривается
**Схема владельца (утверждена 2026-10-21):**
1. bisquite — **дистрибутив**: пакет bisquite зависит от cream точной версии и везёт бандл — `.deb` и `tar.gz` cream
   по пину и снимок каталога cream-extensions по пину.
2. На каждой сборке сразу после `FROM` — **скрытый шаг**: cream из бандла ставится в гостя офлайн, встроенный снимок
   регистрируется источником `core`.
3. VMFILE как Dockerfile: расширения ставятся обычным `RUN_COMMAND cream install …`, по слою на строку; `EXTENSION`
   удаляется; внешний каталог — `RUN_COMMAND cream source add …`.
4. Валидация как hadolint: bisquite находит строки cream в VMFILE и до сборки проверяет их cream хоста по бандлу.
5. Состав образа — метками OCI: cream пишет `/etc/image-labels.d/cream.json`, bisquite собирает такие файлы в метки;
   `bs image diff` сравнивает образы по меткам.
6. (дополнение владельца) configure выполняется **один раз до успеха** — по отметке выполнения, а не на каждой
   загрузке; код хука `20` в configure — «пропущено», повтор на следующей загрузке (§10.7, §10.8).

**Что этим меняется в действующих решениях:**
| Документ | Пункт | Было | Станет |
|---|---|---|---|
| cream-core §0 п. 3 | `EXTENSION` | bisquite исполняет `EXTENSION` через cream: хост разрешает, в гостя кладутся бинарь и дерево расширения | `EXTENSION` удаляется (§6.3); cream ставится в гостя скрытым шагом, расширения — `RUN_COMMAND cream install` |
| cream-core §12 | фаза build | `cream resolve` на хосте + `install --phase build --from-dir --record` в госте | `cream install` в госте из источника `core` (снимок в образе); фаза определяется сама (§10.1); на хосте — только `cream check` (§7) |
| cream-core §0 п. 5 / Р5 | фаза по умолчанию | `live`, build — только явным флагом | автоопределение: в `virt-customize` PID 1 не systemd → `build`; `--phase` — переопределение |
| cream-core §6.4 | когда идёт configure | на каждой загрузке (юнит стадии) | на загрузке — только если нет отметки выполнения или она устарела; `configure.every_boot: true` — по-старому (§10.7) |
| cream-core §6.2 | код `20` хука configure | как у прочих хуков: без `--allow-skip` — 68 | «пропущено»: не ошибка, отметка не пишется, повтор на следующей загрузке (§10.8) |
| bisquite spec 2026-10-19 §4–§5 | слои cream | `extension_router`, staging `record.json`/`params.env`, `dpkg -i` cream на каждый слой, метки `bisquite.lineage.{extensions,cream}` | всё удаляется (§11.1) |
| bisquite spec 2026-10-19 §4.1 | `Depends: cream (= пин)` | точная версия на хосте | **остаётся**; добавляется бандл (§4) |
| bisquite `extensions.yaml` | источники каталога на хосте | `core` — git `main` или архив по пину, `bs extension sync` | каталог встроен в бандл; `extensions.yaml` и `sync` удаляются |

Нужны ADR: в cream — «фаза определяется сама; `cream check`; `/etc/image-labels.d/cream.json`; configure по
отметке выполнения — часть контракта cream/v1» (следующий свободный номер — 0004); в bisquite — «bisquite везёт
cream и каталог; `EXTENSION` удалена» (breaking 3.0.0).

## 1. Цели и не-цели
**Что должно стать верным:**
- `bs image build` собирает образ с расширениями **без сети для cream и каталога**: всё нужное — в бандле пакета
  bisquite; суммы сверены при сборке пакета.
- VMFILE читается как Dockerfile: что ставится в образ, видно строками `RUN_COMMAND cream install name@ver K=V`; у
  каждой строки свой шаг в журнале сборки и в `bs image history`.
- Ошибки, которые видны без гостя (нет расширения или версии в каталоге, чужая платформа, конфликт, пропущенный
  обязательный параметр, незакрытая зависимость), ловятся `bs image validate` и предсборочной проверкой за < 1 с,
  до загрузки базы.
- По образу без его загрузки видно: какой cream, какой снимок каталога, какие расширения каких версий (метки OCI);
  два образа сравниваются одной командой.
- Секрет сборки не попадает ни в argv, ни в журнал, ни в метки; ключи `scope: build` не попадают и в qcow2.
- configure расширения на живой машине выполняется до первого успеха и повторяется только при изменении версии,
  дерева или параметров (включая ротацию секрета), после сбоя или по `--force`; загрузка без изменений не гоняет хуки.
- Скрипт Stvor работает без изменений; инвариант «cream в образе ≤ пин Stvor на каждом экземпляре» соблюдается
  порядком выпусков: bisquite 3.x не выходит, пока пин не выложен на все экземпляры Stvor (§12).

**Не-цели:**
- Желаемое состояние отдельным файлом (`cream.yaml`, `cream apply`) — отвергнуто владельцем (§18).
- Решатель версий: у источника одна версия расширения (как в cream-core).
- Материализованный кеш шагов bisquite (отложенная спека `2026-09-04-layer-snapshots-deferred.md`) — здесь только
  ключи шагов.
- Сборка чужой архитектуры: барьер `_assert_arch_matches_host` остаётся.
- Семейство OpenWrt — отдельной следующей частью (§16, кратко).
- `install.sh` в выпуске cream, загрузка внешних каталогов на хост для проверки, `bs image diff --deep files` —
  «Позже / отдельно» (§21).
- Изменения в Stvor (кроме подъёма пина по цепочке).

## 2. Как сейчас (факты)
### 2.1 bisquite 2.2.0 — что знает о cream
| Место | Что делает |
|---|---|
| `packaging/deb/DEBIAN/control.in` | `Depends: cream (= @CREAM_VERSION@), …`; пакет per-arch (`Architecture: @ARCH@`), выпуск — amd64 (arm64 — по настройке CI) |
| `src/bisquite/data/cream-pin.env` (+ `tools/cream-pin.env`) | `CREAM_VERSION=1.1.4`, `CREAM_SHA256_{AMD64,ARM64}` `.deb`, `CREAM_URL` |
| `src/bisquite/data/extensions.yaml` | умолчание источника `core`; `bs extension sync` тянет его на хост |
| `domains/build/ports/cream.py`, `infrastructure/cream/{cli,errors,staging,guest_deb}.py` | порт и адаптер к процессу cream (`resolve`, `sync`, staging, `.deb` для гостя) |
| `domains/build/services/extension_router.py` | пробы `resolve`, проверка порядка, `--installed` из метки базы |
| `domains/build/provenance.py` | метки `bisquite.lineage.{extensions,cream,root}` |
| `infrastructure/build/builders/linux.py` | слой `EXTENSION`: `dpkg -i` cream, `system init`, `install --phase build --from-dir`, `virt-cat` `result.json` на отказе; дайджест прочих шагов — `sha256(строки VMFILE)` |
| `cli/commands/extension.py` | `bs extension sources|sync|ls|check` |
| `cli/commands/image.py` | `bs image validate` (разбор, `LABEL os/arch`, барьер архитектуры, маршруты `EXTENSION`); `bs image inspect` (`labels`, `layers_meta`), `bs image vmfile`; **`history` и `diff` нет** |
| `infrastructure/build/builders/openwrt.py` | `EXTENSION` запрещена |
| `.github/workflows/{cream-bump,extensions-bump}.yml` | PR подъёма пинов cream и каталога |

`EXTENSION` встречается в **32** VMFILE примеров (`examples/dst/*`, `examples/build/{amd64,jetson,raspberry-pi}/*`),
**166** строк; чаще всего `code-server`, `desktop`, `x11vnc`, `selkies`, `cloud-user`, `docker`, `apt-guard`.

### 2.2 cream 1.1.4 (+ PR #23)
- `install --phase build` работает **только** с `--from-dir`/`--record` (staging bisquite); без флага фаза —
  `live` (Р5, `engine.Phase()`: `CREAM_PHASE` родителя, иначе live).
- `resolve --phase build --target-arch --family --installed FILE` — офлайн-разрешение по кешу источников; основа
  будущего `cream check`.
- Источники: `archive` (`--archive URL --sha256 HEX`, в т. ч. `file:///`) и `path` (`--path DIR`); `--replace`,
  `--sync`.
- Реестр: `state.Record` (`name`, `version`, `source`, `ext_digest`, `params_digest`, `phase`, …), журнал
  `history.jsonl` без параметров — уже едет с образом по цепочке `FROM`.
- configure: юнит `cream-configure@<ext>` стадии запускает хук **на каждой загрузке** (`engine.ConfigureReport`,
  причина `boot` без `/run/cream/<ext>.reason`); сбой — 79 и запись `failed` в `history.jsonl`; код `20` — как у
  прочих хуков (68 без `--allow-skip`).
- Выпуск 1.1.4: `cream_1.1.4_{amd64,arm64}.deb` — 10,3 / 10,0 МБ; `cream_1.1.4_linux_{amd64,arm64}.tar.gz` —
  3,5 / 3,1 МБ; каталог `cream-extensions-5.2.0.tar.gz` — 1,3 МБ.

## 3. Целевая архитектура
```
 машина сборки                                            гость (virt-customize, шаг за шагом)
 ─────────────                                            ─────────────────────────────────────
 apt: bisquite 3.x ──Depends──► cream (= 1.2.0)  ◄── cream хоста: только `cream check` (офлайн)
          └─Depends──► bisquite-cream-bundle (= 1.2.0+cat5.2.0-1)
                       /usr/share/bisquite/cream/
                         cream_1.2.0_{amd64,arm64}.deb, cream_1.2.0_linux_{amd64,arm64}.tar.gz,
                         cream-extensions-5.2.0.tar.gz, SHA256SUMS, bundle.env
 bs image build -f VMFILE
   0. предсборочная проверка: строки cream → `cream check` по бандлу (§7)
   FROM base ─────────────────────────────────────────────► база
   #0 (скрыто) CREAM 1.2.0 core@5.2.0 ───── upload .deb + снимок ─► dpkg -i (или noop) → каталог в
                                                                     /usr/share/cream-catalogs/core-5.2.0 →
                                                                     cream source add core --path … --replace
   #1 RUN_COMMAND cream install desktop@2.1.0 DESKTOP_AUTOLOGIN=1 ─► фаза build определена сама →
                                                                     /etc/image-labels.d/cream.json
   #2 RUN_COMMAND cream install code-server@4.3.0 ─────────────────► …
   конец: virt-cat /etc/image-labels.d/*.json → OCI-метки; sysprep; sparsify
 первая загрузка машины: configure каждого расширения → отметка выполнения; дальше — только при изменениях
 Stvor (живой робот): curl … | sudo bash → dpkg -i cream → cream install teleport …   (без изменений)
```
| Кто | Отвечает за | Не знает о |
|---|---|---|
| bisquite | пин cream и каталога, бандл, скрытый шаг, разбор стандартных строк cream для проверки, сбор меток из `/etc/image-labels.d`, `history`, `diff`, секрет-монтирование | формате манифеста, графе, реестре cream (всё это — через `cream check` и `cream.json`) |
| cream | установка, граф, реестр, автоопределение фазы, `cream check`, запись `cream.json`, отметки configure | bisquite, qcow2, OCI |
| cream-extensions | расширения и снимки каталога | bisquite |
| автор образа | VMFILE | — |

**Окончательная проверка — в госте.** Предсборочная проверка видит только стандартные строки и метки базы; граф по
настоящему реестру гостя строит `cream install` на своём шаге.

## 4. Бандл
### 4.1 Отдельный пакет `bisquite-cream-bundle`
| Поле | Значение |
|---|---|
| Имя | `bisquite-cream-bundle` |
| `Architecture` | `all` — содержимое одинаково для любой машины сборки |
| Версия | `<cream>+cat<каталог>-<сборка>`, например `1.2.0+cat5.2.0-1` |
| Содержимое | `/usr/share/bisquite/cream/`: `cream_<v>_{amd64,arm64}.deb`, `cream_<v>_linux_{amd64,arm64}.tar.gz`, `cream-extensions-<c>.tar.gz`, `SHA256SUMS` (для self-check и скрытого шага; при установке сеть не нужна — суммы сверены при сборке пакета), `bundle.env` (`CREAM_VERSION`, `CATALOG_VERSION`, `CATALOG_SHA256`, строго `KEY=VALUE`) |
| Размер | ~28 МБ: 2×~10 МБ `.deb` + 2×~3,3 МБ tar + 1,3 МБ каталог |
| Связь | bisquite: `Depends: cream (= <cream>), bisquite-cream-bundle (= <версия бандла>)` |
| Удаление | `apt remove bisquite` оставляет бандл и cream как автоматически поставленные зависимости → `apt autoremove` убирает оба штатно |

**Почему отдельный пакет, а не файлы в `bisquite`** (решение, §19 В1): bisquite собирается per-arch — 28 МБ
повторились бы в каждом `.deb`; исправление кода bisquite без смены пинов не заставляет скачивать 28 МБ заново;
бандл строится один раз и проверяется отдельно. Оба пакета собираются и выпускаются из репозитория bisquite одним
релизом, поэтому «две версии одного бандла» не возникает.

**Почему обе архитектуры, хотя гость = хост** (барьер архитектуры): пакет `all` один на все машины; arm64-хост сборки
(Jetson/Orin) берёт тот же пакет. Цена — ~13 МБ на машину. Вариант «только своя архитектура» — §18.

### 4.2 Сборка пакета
- Пин — `src/bisquite/data/cream-pin.env` (как сейчас) **плюс** `CATALOG_VERSION`, `CATALOG_SHA256` (в одном файле;
  `extensions-bump` пишет туда же). Цепочки `cream-bump`/`extensions-bump` остаются: PR подъёма пина в bisquite.
- `make bundle-deb`: скачать ассеты релизов cream и cream-extensions, **сверить sha256 с пином** (не с
  `SHA256SUMS` релиза — пин зафиксирован в git), разложить, собрать `.deb` (тем же способом, что основной пакет).
  Расхождение — отказ сборки пакета.
- CI: `release-build-check` строит оба пакета и ставит их в контейнер Debian 12: `bs self-check` зелёный.

### 4.3 self-check
Компонент «Расширения cream» меняет смысл: `cream --version` хоста = пин; файлы бандла на месте и совпадают с
`SHA256SUMS` бандла; `bundle.env` = пин bisquite. Сетевых проверок нет.

## 5. Скрытый шаг cream
### 5.1 Алгоритм (один вызов `virt-customize` сразу после `FROM`)
1. Архитектура гостя — `LABEL arch` или архитектура хоста (барьер уже проверен). Семейство — по `virt-inspector`,
   который bisquite и так зовёт на базе; не deb-семейство (OpenWrt, Windows) — шаг пропускается с записью в журнал
   (`CREAM off` неявно, §16).
2. **Быстрый путь без appliance:** у базы метки `bisquite.cream.version` = пин и `bisquite.cream.catalog` = пин —
   шаг записывается как `noop (из базы)` и не запускает `virt-customize`.
3. Иначе в одном вызове: `--upload` пакета нужной архитектуры и снимка во временный каталог гостя
   `/var/cache/bisquite-cream/`; `--run-command` встроенного в bisquite POSIX-скрипта:
   - cream нет → `dpkg -i`; та же версия → ничего; **старше** → `dpkg -i` (обновление); **новее** → отказ с текстом
     «в базе cream X новее пина bisquite Y — обновите bisquite или соберите с `--no-cream`» (тот же
     `dpkg --compare-versions`, что в скрипте Stvor);
   - снимок распаковывается в `/usr/share/cream-catalogs/core-<c>/`, прежние `core-*` удаляются;
   - `cream source add core --path /usr/share/cream-catalogs/core-<c> --replace --sync` (офлайн);
   - временный каталог удаляется.
4. Шаг попадает в журнал как `#0 [<digest8>] CREAM 1.2.0 core@5.2.0 (скрытый шаг)` и в `bs image history` с пометкой
   `hidden`. Ключ шага — `sha256(версия бандла + архитектура)`.
5. Метки (всегда, когда шаг не отключён): `bisquite.cream.version=1.2.0`, `bisquite.cream.catalog=core@5.2.0`,
   `bisquite.cream.catalog.sha256=<hex>`.

Почему `type: path`, а не `archive file:///`: файл архива удаляется после шага, а запись источника с мёртвым
`file:///` ломала бы `cream source sync` на живой машине; каталог в образе (~5 МБ распакованным) остаётся рабочим
источником офлайн. На живой машине оператор может заменить `core` на архив по URL (`--replace`).

### 5.2 Отключение
- `bs image build --no-cream` или директива `CREAM off` в VMFILE (новая директива; другое значение — отказ разбора).
  Применение: образы без cream (чистый OpenWrt, «голые» базы для чужих потребителей).
- При отключении метки `bisquite.cream.*` не пишутся. Строки `cream …` в `RUN_COMMAND` при этом: если у базы есть
  `bisquite.cream.version` — разрешены (cream базы), проверка идёт по её меткам с заметкой «каталог — по метке
  базы»; если нет — ошибка проверки «cream отключён, а VMFILE его вызывает».

### 5.3 Почему не сеть в госте
Скрытый шаг не ходит в сеть: офлайн-машины сборки и прокси перестают быть проблемой, а суммы сверены один раз —
при сборке пакета, а не на каждой сборке образа. Внешние каталоги автор подключает сам (§6.2), и это видно в VMFILE.

## 6. VMFILE в стиле Dockerfile
### 6.1 Пример
```
FROM ubuntu2404:dst-base
LABEL os=ubuntu
LABEL version=24.04
LABEL arch=amd64
RESIZE size=16G device=/dev/sda idx=1
RUN_COMMAND cream install desktop@2.1.0 DESKTOP_AUTOLOGIN=1
RUN_COMMAND cream install gnome@4.2.0
RUN_COMMAND cream install x11vnc selkies code-server
RUN_COMMAND cream source add lab --archive https://git.example/lab-ext-1.4.0.tar.gz --sha256 9c1e… --sync
RUN_COMMAND cream install --source lab lab-tools@1.4.0
RUN_COMMAND --mount=type=secret,id=mirror cream install apt-mirror --params-file /run/secrets/mirror
```
- Одна строка — один шаг (один `virt-customize`), как `RUN` в Dockerfile. Несколько расширений в одной строке
  допустимы (`cream install a b c`) — один шаг; так автор сам выбирает между наглядностью и временем сборки (§17 р. 2).
- `@версия` — утверждение: другая версия в источнике — отказ 66 (уже так в cream). Без версии — та, что в снимке;
  проверка печатает, какая версия будет поставлена, а `--strict` требует явную.
- Фаза не пишется: cream в `virt-customize` сам определяет `build` (§10.1).
- Порядок строк задаёт автор: зависимость, которой нет ни в базе, ни в предыдущих строках, — отказ проверки (§7) и
  отказ 67 в госте; `--with-deps` в строке — добрать зависимости из того же источника.

### 6.2 Внешний каталог
Только явной строкой `RUN_COMMAND cream source add <имя> --archive <url> --sha256 <hex> --sync`. Сеть — у appliance
`virt-customize`, как у любого `apt-get` в `RUN_COMMAND`. Подмена архива ловится в госте (`source.mismatch`, 69).
`--path` в такой строке указывает на путь гостя (обычно скопированный `COPY_IN`) — предсборочная проверка его не
видит (§7.2).

### 6.3 `EXTENSION` удаляется
Парсер отвечает отказом с подсказкой (словарь переименований в `vmfile_parser.py`, как `UPDATE` → `UPGRADE`):
«`EXTENSION` удалена в bisquite 3.0: пишите `RUN_COMMAND cream install [--source S] name[@ver] K=V`». Перевод
механический: `EXTENSION [src/]name[@v] K=V` → `RUN_COMMAND cream install [--source src] name[@v] K=V`; секретные
параметры — через `--mount=type=secret` и `--params-file` (§9). 32 VMFILE и README примеров переписываются в плане
(§13.2); утилиты миграции нет.

## 7. Предсборочная проверка (как hadolint)
### 7.1 Где работает
- `bs image validate -f VMFILE` (существующая команда; маршруты `EXTENSION` в её отчёте заменяются разделом `cream`)
  — для CI автора и редактора.
- `bs image build` — тот же разбор **до** загрузки базы; ошибка останавливает сборку, предупреждения печатаются.
  `--strict` у `validate` по-прежнему делает предупреждения ошибками.

### 7.2 Что разбирается
**Стандартная форма** — `RUN_COMMAND [--mount=…] cream <подкоманда> <аргументы>`: одна команда без `;`, `&&`, `|`,
подстановок `$…` (разбор `shlex`), кроме допустимых `--params-file /run/secrets/<id>` и перенаправления
`< /run/secrets/<id>` для `KEY=-`. Подкоманды: `install` (ссылки, `K=V`, `--source`, `--with-deps`, `--accept-risk`,
`--params-file`), `source add` (`--archive URL --sha256 HEX`), `remove`, `upgrade`. Любая другая строка с `cream`
(в т. ч. `source add --path`) — **пропуск с заметкой** («нестандартная форма — проверит только гость»), не ошибка.

Состояние, которое ведёт проверка по строкам сверху вниз: установленное базой (метка `cream.extensions` базы, §8) ∪
поставленное предыдущими строками; источники `core` (бандл) ∪ добавленные `source add`.

### 7.3 Что проверяет — новая команда cream
bisquite зовёт **один** процесс на VMFILE:
```
cream --json check --catalog core=/usr/share/bisquite/cream/cream-extensions-5.2.0.tar.gz \
  --os ubuntu:24.04 --arch amd64 --installed <файл из метки базы> \
  --step 'install desktop@2.1.0 DESKTOP_AUTOLOGIN=1' --step 'install gnome@4.2.0' …
```
Ответ — по шагу: `ok` / `error` (код и `error.code` из таблицы cream-core §11.3) / `skipped`, итоговые версии и
порядок. Проверяется: платформа (`platform.os`, архитектура; `platform.commands` — только предупреждением, их
наличие видно лишь в госте), наличие расширения и версии, `depends`/`requires` закрыты базой или предыдущими шагами,
`conflicts`, обязательные параметры заданы (секретные — именем), типы значений, `targets.build` допускает фазу
build. Офлайн, архивы читаются без распаковки на диск; цель — < 1 с на VMFILE из примеров.

Откуда `--os`: метка базы `bisquite.os-release` (`ID:VERSION_ID`; новая, её пишет каждая сборка bisquite 3 по
`virt-inspector`) → иначе `LABEL os`+`LABEL version` для `debian`/`ubuntu` → иначе (`l4t`, импортированная база)
платформа не проверяется, заметка. Откуда `--installed`: метка `cream.extensions` базы из локального хранилища; базы
нет локально — без `--installed`, незакрытые зависимости становятся предупреждениями «возможно, есть в базе».

### 7.4 Внешние источники в проверке — решение: пропуск с заметкой (в 3.0)
**Решено контроллером 2026-10-21 (упрощение по skeptic; пересматривает рекомендацию редакции 3 «скачивать в кеш»).**
`source add --archive URL --sha256 H` в стандартной форме обязан нести sha256 (иначе — ошибка проверки), но архив на
хост **не скачивается**: шаги `install --source <внешний>` получают `skipped` с заметкой «внешний каталог `<имя>` —
проверит гость» (с `--strict` — тоже заметка, не ошибка: проверка 3.0 не может их проверить по построению).
Состояние после пропущенного шага считается неизвестным только для имён этого шага: зависимости на них у
последующих строк — предупреждение «возможно, поставлено строкой N», а не ошибка. Сеть у проверки не появляется,
кеш на хосте не заводится. Загрузка в кеш по sha256 — «Позже / отдельно» (§21).

## 8. Метки образа из гостя: `/etc/image-labels.d`
### 8.1 Общее правило (bisquite)
- В конце сборки (после последнего шага, до sysprep) bisquite читает `/etc/image-labels.d/*.json` одним
  `virt-cat`/`guestfish`-вызовом. Каждый файл — JSON-объект `{"ключ": "строка"}`.
- Ключи файла `<имя>.json` обязаны начинаться с `<имя>.` (`cream.json` → `cream.*`); префиксы `bisquite.` и
  `org.opencontainers.` запрещены. Нарушение — файл пропускается целиком, предупреждение в журнал и метка
  `bisquite.labels.skipped=<имя>[,<имя>…]`.
- Конфликт с `LABEL` VMFILE или `--label` CLI: явная метка автора побеждает, предупреждение.
- Лимиты: значение ≤ 8 КиБ, все метки из гостя ≤ 64 КиБ; превышение — файл пропускается так же (не обрезается:
  обрезанный JSON хуже отсутствующего).
- Каталог остаётся в образе: на живой машине тот же состав читается без bisquite.

### 8.2 `cream.json` (cream)
cream перезаписывает файл после каждой успешной `install`/`upgrade`/`remove` (атомарно: временный файл + rename) по
**всему** реестру, поэтому в образе-потомке он описывает и базу:
```json
{
  "cream.version": "1.2.0",
  "cream.extensions": "[{\"name\":\"desktop\",\"version\":\"2.1.0\",\"source\":\"core\",\"digest\":\"sha256:…\",\"provides\":[\"desktop-session\"]}]",
  "cream.sources": "[{\"name\":\"core\",\"type\":\"path\",\"catalog_version\":\"5.2.0\"}]"
}
```
- Только имя, версия, источник, `ext_digest`, `provides` — без параметров, `params_digest` и путей (секреты и
  настройки не утекают в метки). `provides` нужен проверке потомка (`--installed` уже принимает этот формат).
- Сбой записи — предупреждение `labels.write_failed`, не отказ операции.
- `cream.extensions` при ~40 расширениях — ~6 КиБ: укладывается в лимит 8 КиБ; при росте — части (§19 В5).

### 8.3 Что уходит
Метки `bisquite.lineage.extensions` и `bisquite.lineage.cream` больше не пишутся; `bisquite.lineage.root` и
`chain_digest` остаются. Образы 2.x читаются (метки показываются как любые), но для проверки потомка их метки не
используются — потомок 3.0 от базы 2.x проверяется без `--installed`.

## 9. Секреты сборки
Без изменений относительно редакции 2, универсально в bisquite:
- CLI: `bs image build --secret id=mirror,src=./token|env=MIRROR_TOKEN` (многократно). Web: только ссылки на файлы
  `/etc/bisquite/secrets.d/<id>`.
- VMFILE: `RUN_COMMAND --mount=type=secret,id=mirror[,target=/run/secrets/mirror] <команда>`; секрет, не переданный в
  CLI, — отказ до сборки; переданный и не использованный — предупреждение.
- Механика (один вызов на шаг): `--run-command 'mkdir -p /run/secrets && mount -t tmpfs -o mode=0700,size=1m
  bisquite-secrets /run/secrets'`, `--upload <host-tmp>:/run/secrets/<id>`, команда шага, `--run-command 'umount
  /run/secrets; rmdir /run/secrets 2>/dev/null || true'`. Временный файл хоста — 0600 в каталоге 0700, удаляется в
  `finally`. Значение никогда не входит в текст команды: трасса `virt-customize -x/-v` печатает аргументы.
- В cream: `--params-file /run/secrets/<id>` (файл `KEY=VALUE`, 0600, root — tmpfs это даёт) или `KEY=- <
  /run/secrets/<id>`. Новое значение `scope: build` у ключа `config.keys` (третье в перечислении
  `config|install|build`, минор контракта; точная семантика — §10.9): значение отдаётся хуку переменной окружения
  и нигде на машине не хранится.
- **Тест обязателен до принятия** (риск 1): маркер ключа `scope: build` не встречается в сыром qcow2
  (`qemu-img convert -O raw` + `grep -c`), журнале (включая `--debug`), OCI-манифесте и метках. Не прошёл —
  `--mount=type=secret` не входит в 3.0.0 до проверенного способа; запасного пути через текст команды нет.
- Ключ шага включает `id` секретов, не значения (как BuildKit).
- Секрет, который расширение хранит на машине (`secrets` домена), задан при сборке — остаётся в образе по контракту
  расширения; cream в фазе build предупреждает `secret.persisted_in_image` (§19 В4).

## 10. Изменения в cream (1.2.0)
### 10.1 Фаза определяется сама
- Порядок: `--phase build|live` (флаг) → `CREAM_PHASE` родителя (как сейчас) → автоопределение.
- Автоопределение (deb-семейство): `/run/systemd/system` существует и `/proc/1/comm` = `systemd` → `live`;
  `/proc/1/comm` прочитан и это не systemd (в `virt-customize` PID 1 — init appliance libguestfs, `/proc`
  привязан в chroot) → `build`; `/proc` не смонтирован или не читается → **отказ** 64 `phase.undetermined` («задайте
  `--phase`»), а не тихий выбор.
- Вывод `--json` и журнал называют фазу и причину (`phase: build, reason: pid1=init`).
- Контейнер без systemd (Docker) тоже даст `build` — это корректно: службы там не стартуют; документируется.
- OpenWrt (позже): `procd` вместо `systemd`.

### 10.2 `install` в фазе build из источника
Сейчас build требует `--from-dir`/`--record`. Становится: `cream install [--source S] name[@v] K=V` в фазе build
берёт дерево из синхронизированного источника, как в live, и применяет правила build (`targets.build`,
`policy-rc.d`, без restart, configure только `enable`). `upgrade` и `remove` — так же. `--from-dir`/`--record` и
`resolve` остаются в 1.x как устаревшие (контракт ADR-0001), удаление — cream 2.0.

**`--with-deps` в 1.2 (рулинг Task 5, 2026-10-21):** добирает только `depends` — по имени, транзитивно, из источника
того расширения, которому зависимость нужна, в той версии, что там лежит (диапазон проверяет граф: не та — 67
`deps.version`). `requires` не добираются — 67 `deps.requires`; поставщик способности — позже. `cream check`
(Task 7) повторяет ровно эту семантику. Пересматривает cream-core §9.4 в части `requires`.

### 10.3 `cream check`
Новая команда (обобщение `resolve`): `--catalog name=ARCHIVE|DIR` (многократно; без `sources.yaml` и кеша машины),
`--os ID:VERSION`, `--arch`, `--installed FILE`, `--step '<подкоманда> <аргументы>'` (многократно, состояние
переносится между шагами), `--json`. Без root, без сети, ничего не пишет. Коды — существующие (64/66/67/68/69),
формы `--json` — golden-тестами. `resolve` становится тонкой обёрткой над тем же кодом.

### 10.4 `/etc/image-labels.d/cream.json`
§8.2; пишет `internal/state` после фиксации реестра. Путь и формат — часть контракта cream/v1 (минор).

### 10.5 `install.sh` в релизе — вынесено
**Решено контроллером 2026-10-21:** не входит в эту спеку и в cream 1.2.0 (bisquite ставит cream из бандла, Stvor —
своим скриптом; ни одна часть схемы от `install.sh` не зависит). Владелец его хочет — отдельная задача, §21.

### 10.6 Уже сделано
`upgrade` с планом до действий, `hold`, частичным исходом — PR #23 (ADR-0003).

### 10.7 configure один раз до успеха: отметка выполнения
**Как сейчас:** юнит `cream-configure@<ext>` запускает хук configure на каждой загрузке. Образ с десятком расширений
на каждой загрузке гоняет десяток хуков — медленно и шумно в журнале, хотя менять нечего.

**Станет:**
- После **успешного** configure (код хука `0` или `10`) cream пишет отметку выполнения
  `/var/lib/cream/configured/<ext>.json` (атомарно, 0644, без значений):
  ```json
  {"version": "2.1.0", "ext_digest": "sha256:…", "params_digest": "sha256:…",
   "secrets_rev": 3, "machine_id": "<sha256 от /etc/machine-id>", "at": "2026-10-21T10:00:00Z"}
  ```
- На загрузке (причина `boot`) юнит стадии сравнивает отметку с реестром: совпали `version`, `ext_digest`,
  `params_digest`, `secrets_rev` и `machine_id` — хук **не запускается**, юнит завершается успехом, в журнал —
  строка `configure <ext>: пропуск, отметка совпала`. Не совпало что-либо или отметки нет — хук запускается.
- **Когда хук снова запускается:**
  | Событие | Почему отметка не совпадает |
  |---|---|
  | `install`, `upgrade` (новая версия или дерево) | `version`/`ext_digest`; причины `install`/`upgrade` запускают хук и без загрузки, как сейчас |
  | `set` несекретного ключа | `params_digest` |
  | **ротация секрета** (`set KEY=-`, `--params-file` с секретным ключом) | `params_digest` содержит только имя секрета, поэтому в реестр добавляется счётчик `secrets_rev`: он растёт при каждой записи секретного ключа (значение в отметку не попадает) |
  | ручной сброс | `cream configure --force <ext>`: запускает хук сейчас, минуя отметку, и переписывает её при успехе; `cream configure --force --all` — для всех |
  | сбой | при коде хука, отличном от `0`/`10`/`20`, отметка **удаляется** (если была): хук повторяется на **каждой** загрузке до успеха; каждая попытка — запись `failed` в `history.jsonl` |
  | клон машины | `machine_id` другой (образ сняли с живой машины, `/etc/machine-id` пересоздан) — configure проходит заново на новой машине |
  | `remove` | отметка удаляется вместе с записью реестра |
- **Фаза build:** configure в build не исполняется (только `enable` юнита), поэтому отметок в образе bisquite нет:
  первая загрузка машины из образа выполняет все configure. Если отметки всё же попали в образ (образ снят с живой
  машины), их обезвреживает `machine_id`.
- **Опция манифеста** `configure.every_boot: true` (необязательное поле, минор контракта cream/v1; по умолчанию
  `false`): отметка для расширения не проверяется, хук идёт на каждой загрузке — по-старому. Для хуков, которые
  поднимают состояние, живущее до перезагрузки: правила в ядре без сохранения, `ip link set can0 up`, файлы в
  `/run`. Отметка при этом всё равно пишется — для `cream show` и диагностики.
- `cream show <ext>` и `cream list --json` показывают `configured: {at, matches: true|false, every_boot}`.

- **Перезапуск юнита из самого хука идёт мимо отметки** (решено контроллером 2026-10-21, Р1). Хук, который
  ставит себе повтор (`systemctl restart cream-configure@<ext>` по таймеру, как `rustdesk`), перед перезапуском
  пишет причину в `/run/cream/<ext>.reason` (`followup`); cream запускает хук при любой причине, кроме `boot`, не
  сверяя отметку — как сейчас запускаются `install`/`upgrade`/`set`. Без этого второй прогон `rustdesk` увидел бы
  совпавшую отметку первого и не исправил бы файл, который приложение переписало после первого прогона.
  Для хука есть помощник `cream configure --again <ext>` (пишет причину и перезапускает юнит без блокировки).

**Миграция существующих расширений (решено контроллером 2026-10-21, Р1).** Хуки configure cream-extensions
идемпотентны (контракт cream/v1, cream-core §6.1), но идемпотентность не равна «достаточно одного раза»: хук может
зависеть от того, что меняется между загрузками помимо реестра cream. Признак аудита `every_boot: true` —
**любое** из:
| № | Признак | Почему одного прогона мало |
|---|---|---|
| А1 | восстанавливает состояние, не живущее до перезагрузки (правила ядра без сохранения, `ip link`, `/run`, модули) | после перезагрузки состояния нет |
| А2 | перебирает пользователей или домашние каталоги (UID 1000+, `getent passwd`, `/home/*`) | пользователь, появившийся после первого прогона, не настроен |
| А3 | ждёт `cloud-user` (стадия `cloud-user`, `get_cloud_user.sh`) | пользователь cloud-init может смениться при новом `cidata` |
| А4 | сам перезапускает `cream-configure@<ext>` (таймер повтора) | цикл рассчитан на «каждую загрузку до сходимости» |
| А5 | читает или правит файлы, которые переписывает само приложение (`RustDesk2.toml`, конфиги рабочего стола) | приложение откатывает правку после прогона |
| А6 | зависит от железа (GPU, камеры, `/dev/*`, `lspci`/`lsusb`, плата Jetson) | устройство появляется/меняется между загрузками |

Эталон — `extensions/debian/rustdesk/hooks/configure` (А2, А3, А4, А5). Аудит всех хуков configure каталога
(в 5.2.0 — **14** хуков, не ~28: остальные `hooks/configure` — фикстуры `tools/testdata`) — задача плана с таблицей
решений «расширение → признаки → `every_boot` да/нет → обоснование». **CI cream-extensions:** `tools/lint-hooks.sh`
получает проверку — хук configure с признаком А1/А2/А3/А4 (находится по тексту: `getent passwd`, `/home/`, UID
1000, `get_cloud_user`, `cream-configure@`, `ip link`, `iptables`/`nft`, запись в `/run/`) без `every_boot: true` —
**ошибка**; осознанное исключение — комментарий `# cream-lint: every-boot-not-needed <причина>` в хуке. А5 и А6 по
тексту не ловятся — только аудит. Минор каталога выходит вместе с cream 1.2.0. Переходный режим: на машине,
обновлённой с 1.1.x, отметок нет — первая загрузка после обновления выполняет все configure и пишет отметки;
отдельной миграции состояния не нужно.

### 10.8 Код `20` в configure — «пропущено»
Сейчас код `20` любого хука без `--allow-skip` — 68 `hook.not_applicable`. Для configure это неверно: «устройства
пока нет» (USB-камера, CAN-адаптер, GPU после драйвера) — нормальное состояние загрузки, а не сбой.

**Станет (только для хука configure):** код `20` — `skipped`: не ошибка, юнит завершается успехом, отметка **не
пишется**, поэтому хук повторится на следующей загрузке; в `history.jsonl` — запись `configure` со статусом
`skipped`; `cream configure <ext>` вручную отвечает кодом 0 и `"status": "skipped"`. Прежняя отметка (от прошлой
успешной загрузки) при `20` не удаляется, но и не обновляется: хук снова пойдёт на следующей загрузке, если отметка
не совпадает. Для `install`/`upgrade`/`remove` смысл `20` прежний.

Таблица кодов хуков (cream-core §6.2, `docs/reference/exit-codes.md`) после изменения:
| Код хука | `install`, `upgrade`, `remove`, `apply`, `secret` | `configure` |
|---|---|---|
| 0 | успех | успех, отметка выполнения пишется |
| 10 | успех, нужна перезагрузка (`reboot_required`, `/run/reboot-required`) | то же, отметка пишется |
| 20 | неприменимо здесь: с `--allow-skip` — статус `skipped`, иначе 68 `hook.not_applicable` | **пропущено**: не ошибка (cream — 0, `status: skipped`), отметка не пишется, повтор на следующей загрузке |
| другой | сбой: 79 `hook.failed`, статус `failed` | сбой: 79 `hook.failed`, отметка удаляется, повтор на каждой загрузке до успеха |

### 10.9 Новые поля манифеста требуют `cream: ">=1.2.0"` (решено контроллером 2026-10-21, Р3)
- Манифест с `configure.every_boot` (любое значение) или с ключом `scope: build` обязан объявлять
  `cream: ">=1.2.0"`. `checkSince` (`internal/manifest/parse.go`) обобщается со стадий configure на список
  «поле → версия»: стадии `early|cloud-user|graphical` → 1.1.0, `hooks.configure.every_boot` → 1.2.0,
  `config.keys.*.scope: build` → 1.2.0. Нарушение — 65 `manifest.invalid` с `details.field`/`details.since`.
- **Порядок проверок у старого cream.** 1.1.x разбирает манифест строго (`decodeStrict`) и на `every_boot` ответил
  бы «неизвестное поле», а на `scope: build` — «допустимо config или install» (`validate.go`), не сказав, что нужен
  cream новее. Поэтому в 1.2.0 `checkCream` (сравнение `cream:` манифеста с версией исполнителя) стоит **до**
  строгого разбора — так уже сейчас; требование «`cream: >=1.2.0` при новых полях» гарантирует, что 1.1.x
  откажет по `cream:` («нужен cream новее», 68 `platform.cream_version`) раньше, чем дойдёт до поля. Тест 1.2.0:
  фикстура с `every_boot` и `cream: ">=1.1.0"` → отказ `checkSince`; та же с `">=1.2.0"` на исполнителе 1.1.4
  (контрактная фикстура) → `platform.cream_version`, не «неизвестное поле».
- **`scope: build` — третье значение перечисления `config|install|build`** (сейчас `validate.go` допускает `config`
  и `install`, сообщение об ошибке — «допустимо config, install или build»). Отличие от `install`: ключ
  `scope: install` хранится в домене и меняется только `cream install KEY=…` с перезапуском хука `install`;
  ключ `scope: build` **не хранится нигде** (ни `config`, ни `secrets` домена, ни `params_digest`, ни `history.jsonl`)
  — значение живёт только в окружении хуков текущей операции `install`/`upgrade`. `cream set`/`get` на такой
  ключ — 64 `config.scope_build` («ключ только для сборки»), `cream show` его не печатает; `configure` его не видит. Допустим в обеих фазах.
  Обязательный (`empty: false` без `default`) ключ `scope: build` в live — тоже обязателен на `install`.

## 11. Изменения в bisquite (3.0.0)
### 11.1 Удаляется
| Что | Файлы |
|---|---|
| Порт и адаптер cream для слоёв | `domains/build/ports/cream.py`, `infrastructure/cream/{staging,guest_deb}.py`; `cli.py`/`errors.py` сужаются до вызова `cream check` и `--version` |
| Маршрутизация и модели | `domains/build/services/extension_router.py`, `domains/build/models/{cream,extensions,extension_ref}.py` |
| Источники на хосте | `infrastructure/config/extensions.py`, `src/bisquite/data/extensions.yaml`, `BISQUITE_CREAM_GUEST_DIR` |
| Слой `EXTENSION` | ветка в `linux.py` (Р30, `_CREAM_*`, `result.json`), `LayerType.EXTENSION` (остаётся только в словаре отказов парсера), запрет в `openwrt.py` |
| Lineage cream | `provenance.py`: `cream_layer_digest`, `merge/parse_cream_lineage`, метки `bisquite.lineage.{cream,extensions}` |
| CLI | `bs extension sources|sync` (и `check` — заменён `bs image validate`; судьба `ls` — §19 В3) |
| Сеть для cream | `fetch_guest_deb`, `bs extension sync`, загрузка каталога; `tests/fixtures/cream/<версия>/` контракта `resolve` |

### 11.2 Остаётся / меняется
- `cream-pin.env` (+ `CATALOG_VERSION`, `CATALOG_SHA256`), `Depends: cream (= пин)`, цепочки `cream-bump` и
  `extensions-bump` (PR в bisquite).
- self-check (§4.3), `bs image validate` (§7).

### 11.3 Добавляется
| Что | Где |
|---|---|
| пакет `bisquite-cream-bundle`, `make bundle-deb`, CI | `packaging/bundle/`, `Makefile`, `ci-release.yml` |
| скрытый шаг, `--no-cream`, `CREAM off`, метки `bisquite.cream.*`, `bisquite.os-release` | `linux.py`, `vmfile_parser.py`, `image.py` |
| разбор стандартных строк cream и вызов `cream check` | `domains/build/services/cream_lint.py` (новый, без знания формата манифеста) |
| сбор `/etc/image-labels.d/*.json` | `linux.py` (перед sysprep), правило §8.1 |
| `bs image history` | `image.py`: шаги из поля `history` конфигурации OCI (стандартное поле, как у `docker history`): директива, скрытый или нет, digest8, длительность; bisquite начинает его писать |
| `bs image diff` | §11.4 |
| секреты | §9 |
| ключи шагов по содержимому | `COPY_IN`/`UPLOAD` — плюс отпечаток дерева источника, `RUN_SCRIPT` — плюс содержимое скрипта, `RUN_COMMAND` — плюс `id` секретов (сейчас — только строка VMFILE) |

### 11.4 `bs image diff A B`
- По умолчанию — только метки из локального хранилища, без запуска appliance (мгновенно): база (`FROM`,
  `bisquite.lineage.root`, `bisquite.os-release`), cream и каталог (`bisquite.cream.*`), расширения из
  `cream.extensions` — **добавлено / удалено / версия изменилась / дайджест изменился при той же версии**, прочие
  метки — изменённые ключи.
- `--deep packages` — пакеты: `virt-cat /var/lib/dpkg/status` обоих образов, сравнение `Package`/`Version`
  (без chroot и без исполнения кода гостя). Других режимов `--deep` в 3.0 нет: `--deep files` (`virt-diff`) убран
  контроллером 2026-10-21 как упрощение — §21.
- Образа нет локально — отказ с подсказкой `bs image pull`.
- Вывод — таблица (как `bs image ls`) и `--format json` (общий `format_option`).

## 12. Stvor
Без изменений в коде. Скрипт регистрации отказывает, если cream на машине **новее** пина портала
(`install_script.py`, `dpkg --compare-versions "$CREAM_HAVE" gt "$CREAM_VERSION"`). Пин действует на **экземпляре**
портала (`.env` и набор бутстрапа на зеркале), а не в git: слитый PR без выкладки ничего не меняет для робота.
Поэтому (решено контроллером 2026-10-21, Р2):
1. `cream-bump` открывает PR подъёма пина в Stvor, cream-extensions и bisquite на каждый выпуск cream.
2. **Инвариант выпуска:** bisquite 3.x с пином cream X выходит, только когда пин cream ≥ X **выложен на каждый
   экземпляр Stvor** по runbook 10 §10.2 Stvor (`make init-env` с поднятым `CREAM_VERSION`, `make fetch-teleport`,
   `make up`, `make smoke`) и **`scripts/robots-gate.sh` зелёный на этом экземпляре**. «PR слит» условием не
   является. Проверка — пункт чек-листа runbook выпуска bisquite и PR `cream-bump` с перечнем экземпляров.
3. **Операторам чужих порталов** (экземпляры Stvor, которые выкладывает не владелец) — предупреждение в
   `docs/guides/migration-3.0.md` bisquite и в заметках выпуска: «образы bisquite 3.x несут cream X; до регистрации
   таких роботов поднимите пин cream портала до X, иначе скрипт регистрации откажет („cream новее пина“)».
4. Приёмка: `scripts/robots-gate.sh` на роботе из образа bisquite 3.0 (cream 1.2.0 из бандла, пин Stvor 1.2.0):
   регистрация, `cream run teleport join`, `stvor-agent` зелёные; отдельно — отказ скрипта на образе с cream новее
   пина. configure по отметке Stvor не затрагивает: `teleport join` — действие, а не configure; `set-managed` у
   `stvor-agent` меняет параметры → `params_digest` → configure на следующей загрузке, как и нужно.

## 13. Миграция и выпуски
### 13.1 Порядок
| Шаг | Репозиторий | Версия | Что |
|---|---|---|---|
| 1 | cream-extensions | минор | аудит хуков configure по признакам А1–А6, `configure.every_boot: true` и `cream: ">=1.2.0"` где нужно (§10.7, §10.9); проверка `lint-hooks` на признаки; `scope: build` у ключей-токенов зеркал (если есть); CI: `cream check` на примерах каталога — выходит **после** шага 2 (манифестам нужен cream 1.2.0) |
| 2 | cream | **1.2.0** (feat) | §10: фаза сама, `install`/`upgrade`/`remove` в build из источника, `check`, `cream.json`, `scope: build`, `checkSince` по новым полям, отметка configure, код `20` configure; ADR-0004 |
| 3 | Stvor | пин | PR `cream-bump` до 1.2.0 слит **и выложен на каждый экземпляр** (runbook 10 §10.2), `robots-gate` зелёный на каждом (§12 п. 2) |
| 4 | bisquite | **3.0.0** (`feat!`) | §11, бандл, примеры, guide миграции с предупреждением операторам порталов, ADR; только после шага 3 |
| 5 | cream / extensions / bisquite | следующая часть | OpenWrt (§16) |

### 13.2 Примеры bisquite
32 VMFILE и README: `EXTENSION` → `RUN_COMMAND cream install` построчно (подряд идущие простые строки можно
слить в одну, если это не меняет порядок относительно `RUN_COMMAND` автора); секреты — `--mount`; CI —
`bs image validate --strict` на всех примерах (офлайн, по бандлу) и `make test-build` на одном примере с настоящим
`virt-customize`.

### 13.3 Guide миграции (`bisquite: docs/guides/migration-3.0.md`)
1. `EXTENSION …` → `RUN_COMMAND cream install …` (§6.3); источники из старого `extensions.yaml`, кроме `core`, —
   строкой `cream source add … --sha256`.
2. Секретные параметры — `--mount=type=secret` + `--params-file`.
3. `bs image validate --strict`; собрать; `bs image diff <образ 2.x> <образ 3.0>` — расширения и версии совпадают.
4. Удалить `extensions.yaml` (`/etc/bisquite`, `$XDG_CONFIG_HOME/bisquite`) — 3.0 на него предупреждает.

## 14. Безопасность
- Граница доверия: пин cream и каталога в git bisquite; суммы сверяются при сборке пакета бандла; в госте бандл не
  перепроверяется по сети (её нет), но `cream source add --path` фиксирует отпечаток дерева. Подмена бандла на
  машине сборки = root на машине сборки — вне модели.
- Внешние каталоги — только с `--sha256` в VMFILE; на хост не скачиваются (§7.4), подмену ловит гость (`source.mismatch`).
- `cream check` на хосте не исполняет хуков и не пишет на диск; архивы читаются с защитой от `..`, ссылок и
  размера (существующий `internal/source/extract.go`).
- Метки из гостя: правило префиксов (§8.1) не даёт гостю подменить `bisquite.*`/`org.opencontainers.*`; в
  `cream.json` нет параметров.
- Отметка configure не содержит значений; `secrets_rev` — счётчик, а не хеш секрета (хеш короткого секрета
  подбирается). `machine_id` — sha256 от `/etc/machine-id`, не сам идентификатор.
- Секреты — §9; хуки — root-код из источника, как раньше.

## 15. Тестирование и приёмка
| Уровень | Что |
|---|---|
| cream unit | автоопределение фазы (подменённые `/proc/1/comm`, `/run/systemd/system`, нет `/proc` → 64); `check` (golden: нет версии, конфликт, незакрытая зависимость, обязательный параметр, платформа, перенос состояния между `--step`); `cream.json` (форма, нет параметров, атомарность, ошибка записи — предупреждение); отметка configure: каждая строка таблицы §10.7 (совпала → пропуск; версия, дерево, параметр, `secrets_rev`, `machine_id`, `--force`, сбой → удаление, `remove`), `every_boot`, код `20` (§10.8) |
| cream e2e (`make test-build`) | `virt-customize` Debian 12: `dpkg -i` пакета, `source add --path`, `cream install` **без** `--phase` → фаза build, повтор — noop; цепочка `FROM` — потомок закрывает `depends` реестром; `scope: build` не в `secrets`; в образе нет отметок configure |
| cream integration | systemd-контейнер (`make test-integration`): автоопределение → `live`; две перезагрузки (рестарт контейнера): configure выполнен один раз; `set` → выполнен снова; хук с `20` → повтор на следующей загрузке; падающий хук → повтор на каждой; `every_boot` → каждый раз |
| cream-extensions | аудит configure (§10.7): таблица 14 расширений × признаки А1–А6 с решением в PR; `lint-hooks` — фикстура «хук с `getent passwd` без `every_boot`» → ошибка, с комментарием-исключением → чисто |
| bisquite unit | разбор стандартной формы (golden: стандартные, нестандартные → пропуск, `--mount`), правило меток (префиксы, конфликт, лимит), `diff` по меткам, `EXTENSION` → отказ с подсказкой, `CREAM off` |
| bisquite пакет | `make bundle-deb`: суммы не сошлись → отказ; оба пакета в контейнере Debian 12 → `bs self-check` зелёный; `apt remove bisquite && apt autoremove` убирает бандл и cream |
| bisquite `test-build` | пример: скрытый шаг (журнал, `history`, метки `bisquite.cream.*`), 2–3 `cream install`, метки `cream.extensions`; потомок `FROM` — скрытый шаг `noop (из базы)`; база с cream новее пина → отказ; секрет-маркер на `scope: build` — нет в raw qcow2, журнале (`--debug`), манифесте, метках |
| bisquite проверка | `bs image validate --strict` по всем 32 примерам < 1 с каждый (замер в CI), офлайн |
| Stvor | §12 п. 3 |

## 16. OpenWrt — следующая часть (кратко)
cream: семейство `openwrt` (`ID=openwrt`, 25.12, `apk`), `procd` вместо systemd (`internal/initsys`), поставка
`tar.gz`, автоопределение фазы по `procd`, отметки configure — в `/opt/cream/var` (не tmpfs). Каталог: корень
`extensions/openwrt`, первые `amneziawg` (настройка через `uci`; kmod остаётся в образе openwrt-builder), `podkop`,
`luci-apps`. bisquite: `OpenWrtBuilder` получает скрытый шаг из `tar.gz` бандла (до этого — `CREAM off` неявно).
Teleport и Stvor на OpenWrt — не входят. Отдельная спека по `docs/specs/2026-10-20-openwrt.md`.

## 17. Риски
1. **Секрет через `--upload` в tmpfs appliance** (~30 %, что путь уйдёт на диск гостя). Смягчение — тест первым днём
   плана bisquite; не прошёл — `--mount` выпадает из 3.0.0.
2. **Время сборки: шаг на строку.** Каждый `RUN_COMMAND` — запуск appliance (~5–15 с); 166 строк в 32 примерах —
   ~5 шагов на образ, у Jetson больше. Смягчение — несколько расширений в одной строке по выбору автора; скрытый шаг
   пропускается без appliance, когда база уже с пином; кеш шагов — отложенная спека.
3. **configure по отметке ломает расширение, которому нужна каждая загрузка** (правила ядра, CAN, `/run`): после
   перезагрузки состояние не восстановлено, а хук не запущен. Смягчение — аудит каталога и `every_boot` до выпуска
   (§10.7, признаки А1–А6), ошибка `lint-hooks` на текстовые признаки, перезапуск из хука мимо отметки,
   integration-тест с двумя перезагрузками на расширениях из аудита; в документации расширений — правило
   «состояние до перезагрузки, пользователи, железо → `every_boot`».
4. **Диагностика отказа.** Раньше bisquite читал `result.json` через `virt-cat`; теперь отказ — это код
   `RUN_COMMAND`. Смягчение — cream в фазе build печатает отказ человекочитаемо в stderr одной строкой с `error.code`;
   тест, что без `--verbose` строка попадает в журнал bisquite.
5. **Автоопределение фазы ошиблось** (chroot без `/proc`, экзотический init). Смягчение — громкий отказ 64 вместо
   догадки, `--phase` как переопределение, тесты на трёх окружениях (§15).
6. **Предсборочная проверка расходится с гостем** (нестандартная строка, база без меток, l4t без `os-release`
   метки). Смягчение — проверка только советует, всегда называет пропущенное; окончательная проверка — в госте.
7. **Инвариант пина Stvor** — §12; смягчение — условие выпуска bisquite «пин выложен на каждый экземпляр и
   `robots-gate` зелёный на нём» (не «PR слит») и предупреждение операторам чужих порталов в guide миграции.
8. **Объём бандла и частота выпусков**: каждый выпуск каталога — новый пакет 28 МБ. Смягчение — отдельный пакет:
   код bisquite обновляется без него.

## 18. Отвергнутые варианты
| Вариант | Почему нет |
|---|---|
| **`cream apply -f cream.yaml` + `cream bootstrap` + `cream-bootstrap.env` в госте, bisquite без cream** (редакции 1–2 этой спеки) | **Владелец хочет слои в VMFILE в стиле Dockerfile, без отдельного yaml рядом.** Кроме того: второй формат желаемого состояния рядом с VMFILE и производный `.env`, который надо держать в синхроне; сеть в госте для бутстрапа и каталога (или ручное копирование пакета); один шаг на весь файл — ни журнал, ни история не показывают расширения по отдельности; без cream на хосте bisquite ничего не может проверить до сборки |
| Оставить как есть (`EXTENSION` + `resolve` на хосте + staging) | ~1800 строк адаптера и маршрутизатора повторяют решения cream; `dpkg -i` cream на каждом слое; кеш каталога на хосте от того же euid; отдельный язык (`EXTENSION`) рядом с `RUN_COMMAND`; OpenWrt закрыт |
| `EXTENSION` как сахар над `RUN_COMMAND cream install` | два способа сделать одно; сахар прячет, что это обычная команда гостя; проверка всё равно разбирает `RUN_COMMAND` |
| Сеть в госте для cream и каталога (скачать по URL+sha256 на каждой сборке) | офлайн-машины и прокси; сверка на каждой сборке вместо одной при сборке пакета |
| Бандл в самом пакете `bisquite` | 28 МБ в каждом per-arch `.deb` и при каждом обновлении кода (§4.1) |
| Бандл только архитектуры хоста | пакет перестаёт быть `all`, две сборки бандла; экономия ~13 МБ не стоит лишней матрицы |
| cream только в базовых образах, без скрытого шага | вендорские базы (cloud images, L4T) cream не несут; версия cream в образе перестаёт быть функцией пина bisquite |
| Проверка только в госте | ошибка версии или параметра видна через минуты-часы подготовки базы; hadolint-подобная проверка ловит её за секунду |
| Полный разбор shell (bashlex) в проверке | тяжело и всё равно неточно (переменные, условия); стандартная форма + пропуск с заметкой честнее |
| Скачивать внешние каталоги на хост в кеш по sha256 для проверки (рекомендация редакции 3) | сеть и кеш на машине сборки ради совета, который гость всё равно перепроверит; упрощение по skeptic — §7.4, §21 |
| bisquite читает реестр cream (`/var/lib/cream/…`) для меток | связывает bisquite с внутренним форматом состояния cream; `/etc/image-labels.d` — общий контракт для любого инструмента в госте |
| `bs image diff` сразу через `virt-diff`; `--deep files` в 3.0 | минуты на больших образах; метки и `--deep packages` отвечают на главный вопрос; файловое сравнение — §21 |
| configure: `ConditionPathExists=!отметка` в юните вместо проверки в cream | systemd не сравнит версию, дайджесты и `machine_id`; отметка устаревала бы молча |
| configure: хеш значения секрета в отметке вместо `secrets_rev` | хеш короткого секрета подбирается перебором; счётчик ничего не раскрывает |
| configure: `every_boot` по умолчанию, отметка — по желанию расширения | владелец выбрал «реже по умолчанию»; хуки уже идемпотентны, исключения находит аудит и `lint-hooks` |
| Перезапуск юнита из хука сверяет отметку, как загрузка | цикл повтора `rustdesk` остановился бы после первого прогона (§10.7) |
| `install.sh` в cream 1.2.0 | схеме не нужен (бандл и скрипт Stvor); отдельной задачей — §21 |
| cream на хосте + chroot в смонтированный образ (`guestmount`) | хуки исполняют бинари гостя — на чужой архитектуре нужен qemu-user; FUSE и root на машине сборки; `virt-customize` уже даёт изолированный appliance |
| Ansible вместо cream | Python в госте (на OpenWrt нет); нет реестра и действий с машинным выводом, на которых стоит `stvor-agent` |
| Секрет через текст команды; sops/age в образе | трасса `virt-customize -x/-v` печатает аргументы; ключ расшифровки всё равно доставлять, шифротекст навсегда в образе |
| Подписанный список расширений (cosign) | автор VMFILE и каталога — одно лицо в одном git; подлинность содержимого дают пины sha256; Sigstore против ADR-0002 |

## 19. Вопросы редакции 3 — закрыты
Все вопросы **решены контроллером 2026-10-21 по рекомендации спеки** (кроме В2 — решён упрощением по skeptic вопреки
рекомендации редакции 3). Открытых вопросов к владельцу в этой спеке нет: вопросов вне спеки/ADR и необратимых
внешних действий среди них не оказалось (выпуски идут штатным порядком §13.1).
| № | Вопрос | Решение |
|---|---|---|
| В1 | Бандл — отдельный пакет или файлы в `bisquite`? | **Отдельный пакет** `bisquite-cream-bundle` (`all`), выпускается из репозитория bisquite тем же релизом (§4.1). Решено контроллером 2026-10-21 по рекомендации |
| В2 | Внешние источники в проверке: скачивать или пропускать? | **Пропускать с заметкой** в 3.0 (§7.4); загрузка в кеш — §21. Решено контроллером 2026-10-21 упрощением по skeptic |
| В3 | `bs extension ls`/`check` | `check` удаляется (его делает `bs image validate`); `ls` остаётся тонкой обёрткой «что во встроенном каталоге» (cream хоста по снимку бандла); `sources`/`sync` удаляются. Решено контроллером 2026-10-21 по рекомендации |
| В4 | Секрет, хранимый расширением на машине, задан при сборке | **Предупреждать** `secret.persisted_in_image`, запрет — по опыту. Решено контроллером 2026-10-21 по рекомендации |
| В5 | `cream.extensions` перерастёт 8 КиБ | сейчас — одна метка; при > 60 расширений — части `cream.extensions.N` (правило — в §8.1 при необходимости, не в 1.2.0). Решено контроллером 2026-10-21 по рекомендации |
| В6 | `--os` для баз без `os-release`-метки | **метка `bisquite.os-release`** от `virt-inspector` на каждой сборке 3.0; таблицы «l4t → ubuntu» нет. Решено контроллером 2026-10-21 по рекомендации |
| В7 | Подпись выпусков cream | не в этой спеке; вместе с `install.sh` — §21. Решено контроллером 2026-10-21 по рекомендации |
| В8 | Скрытый шаг на базе с cream = пин | **пропускать без appliance** по меткам, `noop (из базы)` (§5.1 п. 2). Решено контроллером 2026-10-21 по рекомендации |
| В9 | `machine_id` в отметке configure | **Да**, клон перенастраивает себя на первой загрузке. Решено контроллером 2026-10-21 по рекомендации |

## 20. Грубая оценка объёма
| Часть | Объём |
|---|---|
| cream 1.2.0: фаза сама (0,5 д), `install/upgrade/remove` в build из источника (1–1,5 д), `check` (1,5–2 д), `cream.json` (0,5 д), `scope: build` + `checkSince` (0,5–1 д), выпуск (0,5 д), **отметка configure + `secrets_rev` + `--force`/`--again` + `every_boot` (1,5–2 д)**, **код `20` configure (0,5 д)**, e2e/integration и ADR (1–1,5 д) | ~8–10 дней |
| cream-extensions: **аудит 14 хуков configure по А1–А6 и `every_boot`** (0,5–1 д), проверка в `lint-hooks` (0,5 д), `scope: build`, CI `cream check` (0,5 д), документация (1 д) | ~2,5–3 дня |
| bisquite 3.0.0: бандл-пакет и CI (1–1,5 д), скрытый шаг + `CREAM off` + метки + `history` (1,5–2 д), проверка `cream_lint` (1,5–2 д), `/etc/image-labels.d` (0,5–1 д), `bs image diff` (1–1,5 д, `--deep packages` +0,3), секреты и тест raw (1,5–2 д), удаление (1–1,5 д), guide и документация (1,5 д) | ~10–13 дней |
| Примеры: 32 VMFILE/166 строк + README, `validate --strict` в CI, `test-build` | ~1–1,5 дня |
| Stvor: подъём пина 1.2.0 + `robots-gate` на образе 3.0 | ~0,5 дня |
| **Итого без OpenWrt** | ~22–28 дней |
| OpenWrt (следующая часть) | ~6–8 дней |

## 21. Позже / отдельно
| Что | Кратко | Почему не здесь |
|---|---|---|
| `install.sh` в выпуске cream | ассет каждого выпуска с вписанными версией и sha256 всех `.deb`/`tar.gz`; `curl -fsSL …/v<x>/install.sh \| sudo sh`; POSIX `sh` (проверка под `dash` и `busybox ash`): семейство и архитектура → `.deb` или `tar.gz`, sha256 до установки, та же версия — ничего, старше — обновление, новее — отказ. Для живых машин и сборок без bisquite (Packer, ручной `virt-customize`). Вместе с ним — подпись `SHA256SUMS` выпусков (В7) | владелец хочет, но схеме bisquite+Stvor не нужен; отдельная спека/задача cream после 1.2.0 |
| Внешние каталоги в предсборочной проверке | скачивание `source add --archive URL --sha256 H` в кеш хоста по sha256 и `cream check --catalog <имя>=<кеш>` | упрощение 3.0: гость проверяет сам (§7.4) |
| `bs image diff --deep files` | `virt-diff -a A -A B`, минуты на больших образах | метки и `--deep packages` закрывают главный вопрос |
| OpenWrt | §16 | следующая часть |
