# План: bisquite с встроенным cream (cream 1.2.0 → каталог → пин Stvor → bisquite 3.0.0)

> 2026-10-21. Спека — [docs/specs/2026-10-21-bisquite-bundled-cream.md](../specs/2026-10-21-bisquite-bundled-cream.md)
> (редакция 4: Р1–Р3, упрощения, §19 закрыт). Классификация — **архитектурная**: четыре репозитория, breaking-выпуск
> bisquite, расширение контракта `cream/v1`. Цикл на задачу: TDD → `make lint` + тесты по скоупу → reviewer →
> коммит в ветке задачи → слияние контроллером в ветку дорожки → `make ci` дорожки после волны.
>
> **Исполнителю:** после выполнения отметь свою задачу `- [x]` в Progress Tracker **в том же коммите**. План живёт в
> репозитории cream; исполнитель другой дорожки отмечает чекбокс отдельным коммитом в ветку плана
> `docs/apply-spec` (cream) сразу после слияния своей задачи — это делает контроллер.

## Progress Tracker
<!-- Пункты разделены пустой строкой: параллельные отметки сливаются без конфликта. -->

- [ ] Task 1: [bisquite] Секрет-монтирование и тест «секрет не в qcow2» (риск №1, день 1)

- [x] Task 2: [cream] ADR-0004 и манифест 1.2.0: `every_boot`, `scope: build`, обобщённый `checkSince` (день 1, гейт контракта)

- [ ] Task 3: [cream-extensions] Аудит 14 хуков configure по А1–А6 и гейт `lint-hooks` в режиме warn (день 1)

- [x] Task 4: [cream] Автоопределение фазы

- [x] Task 5: [cream] `install`/`upgrade`/`remove` в фазе build из источника

- [ ] Task 6: [bisquite] Пакет `bisquite-cream-bundle`, `make bundle-deb`, self-check

- [x] Task 7: [cream] `cream check` (обобщение `resolve`)

- [x] Task 8: [cream] `/etc/image-labels.d/cream.json`

- [ ] Task 9: [bisquite] Удаление слоя `EXTENSION`, адаптера и источников на хосте; отказ парсера с подсказкой

- [ ] Task 10: [cream] `scope: build` в движке: значение только в окружении хука

- [ ] Task 11: [cream] Отметка выполнения configure, `secrets_rev`, `--force`, `--again`, `every_boot`

- [ ] Task 12: [bisquite] Скрытый шаг cream, `--no-cream`, `CREAM off`, метки `bisquite.cream.*` и `bisquite.os-release`

- [ ] Task 13: [cream] Код `20` хука configure — «пропущено»

- [ ] Task 14: [bisquite] Сбор `/etc/image-labels.d/*.json` в метки OCI

- [ ] Task 15: [cream] e2e (`make test-build`) и integration (две перезагрузки) для 1.2.0

- [ ] Task 16: [bisquite] Предсборочная проверка `cream_lint` и раздел `cream` в `bs image validate`

- [ ] Task 17: [cream] Документация cream 1.2.0 и выпуск 1.2.0

- [ ] Task 18: [bisquite] `bs image history`, `bs image diff` (метки + `--deep packages`), `bs extension ls`

- [ ] Task 19: [cream-extensions] Каталог под cream 1.2.0: `every_boot`, `cream: ">=1.2.0"`, гейт → error, `scope: build`, CI `cream check`; выпуск минора

- [ ] Task 20: [bisquite] Ключи шагов по содержимому; секреты + `scope: build` сквозь `cream install`

- [ ] Task 21: [Stvor] Пин cream 1.2.0 и каталога: PR, выкладка на каждый экземпляр, `robots-gate`

- [ ] Task 22: [bisquite] Подъём пина бандла до cream 1.2.0 и каталога нового минора

- [ ] Task 23: [cream-extensions] Документация: README и `docs/` в стиле популярных проектов под новую схему

- [ ] Task 24: [bisquite] Переписывание 32 VMFILE примеров и их README; `validate --strict` в CI с замером

- [ ] Task 25: [bisquite] Документация: README и `docs/` под новую схему, guide миграции 3.0, ADR bisquite

- [ ] Task 26: [bisquite] Предвыпускная проверка и выпуск 3.0.0 (условие — инвариант Stvor)

- [ ] Task 27: [cream] Фаза OpenWrt: семейство `openwrt`, `procd`, поставка `tar.gz` (по отдельной спеке)

- [ ] Task 28: [cream-extensions + bisquite] Фаза OpenWrt: корень `extensions/openwrt`, скрытый шаг в `OpenWrtBuilder`

- [ ] Task 29: Финальная интеграция: сборки примеров на PVE, `make ci` во всех репозиториях

## Дорожки и репозитории
| Дорожка | Репозиторий (основной checkout) | Ветка дорожки | Worktree исполнителя | Задачи |
|---|---|---|---|---|
| C | `/home/debian/projects/cream` | `feature/cream-1.2` | `cream-wt-t<N>` | 2, 4, 5, 7, 8, 10, 11, 13, 15, 17, 27 |
| E | `/home/debian/projects/cream-extensions` | `feature/configure-once` | `cream-extensions-wt-t<N>` | 3, 19, 23, 28 (часть) |
| B | `/home/debian/projects/bisquite` | `feature/bundled-cream` | `bisquite-wt-t<N>` | 1, 6, 9, 12, 14, 16, 18, 20, 22, 24, 25, 26, 28 (часть) |
| S | `/home/debian/projects/stvor` | `chore/cream-1.2-pin` | dev-worktree Stvor | 21 |

Ветки дорожек сливаются в `main` своих репозиториев **только** в порядке выпусков (ниже). Слияние дорожки C в `main`
cream = выпуск 1.2.0 через release-please (Task 17).

## Порядок выпусков (жёсткий)
1. **cream 1.2.0** (Task 17) — после Task 2, 4, 5, 7, 8, 10, 11, 13, 15.
2. **cream-extensions минор** (5.3.0; Task 19) — после выпуска cream 1.2.0: манифестам с `every_boot` нужен
   `cream: ">=1.2.0"` (спека §10.9), CI каталога валидирует пином cream.
3. **Пин Stvor** (Task 21) — PR `cream-bump` слит **и выложен на каждый экземпляр** по runbook 10 §10.2 Stvor,
   `scripts/robots-gate.sh` зелёный на каждом (спека §12 п. 2). «PR слит» условием не является.
4. **bisquite 3.0.0** (Task 26) — только после п. 3; Task 26 начинается с проверки этого условия.
5. OpenWrt (Task 27–28) — следующие миноры cream/каталога/bisquite, после 3.0.0.

## Волны и владение файлами
Не более **3 исполнителей одновременно** (по одному на дорожку или два в C при пустой дорожке). Тяжёлое
(`make ci`, `make test-build`, `make test-integration`, сборки на PVE) — только контроллер или задача, у которой это и
есть работа, под семафором; по одному на хост (память `project_vm_hard_stops`).

| Волна | Задачи (параллельно) | Пересечения и их владелец |
|---|---|---|
| 1 (день 1) | **T1** (B), **T2** (C), **T3** (E) | нет: три репозитория |
| 2 | T4 (C), T5 (C), T6 (B) | `internal/engine/engine.go` — **T4**; T5 в нём ничего не меняет (фаза — через `e.Phase()`) |
| 3 | T7 (C), T8 (C), T9 (B) | `internal/cli/commands.go` — **T7**; `internal/state/store.go` — **T8** |
| 4 | T10 (C), T11 (C), T12 (B) | `internal/state/record.go` — **T11**; `internal/engine/install.go` — **T10**; `internal/engine/configure.go` — **T11** |
| 5 | T13 (C), T14 (B), T15 (C) | T15 правит только `test/**` и `Makefile`; T13 — `internal/engine/{configure,hook}.go` |
| 6 | T16 (B), T17 (C) | — |
| 7 | T18 (B), T19 (E) | — |
| 8 | T20 (B), T21 (S), T23 (E) | — |
| 9 | T22 (B) | `src/bisquite/data/cream-pin.env` — **T22** |
| 10 | T24 (B), T25 (B) | T24 — `examples/**`; T25 — `README.md`, `docs/**`; чужое — стоп и доклад |
| 11 | T26 (B) | всё (контроллер) |
| 12 | T27 (C), T28 (E+B) | по отдельной спеке OpenWrt |
| 13 | T29 | всё (контроллер) |

Внутри дорожки B файлы `linux.py`, `vmfile_parser.py`, `cli/commands/image.py` трогают T9, T12, T14, T16, T18, T20 —
поэтому дорожка B **последовательна** (один исполнитель за раз), кроме T1 (новые файлы) и T6 (`packaging/bundle/`).

Правила исполнителю (вставлять в промпт): свои пути и список «не заходи»; `git add <пути>`, никогда `git add -A`;
коммит целыми файлами; Conventional Commits по-русски с `Co-Authored-By`; без `--no-verify`; тяжёлое не запускать,
если это не работа задачи; нужен чужой файл — остановиться и доложить.

---

## Task 1: [bisquite] Секрет-монтирование и тест «секрет не в qcow2» (риск №1, день 1)
**Зачем первым:** спека §9, §17 р. 1 — если путь `--upload` в tmpfs appliance оставляет след на диске гостя,
`--mount=type=secret` выпадает из 3.0.0 до проверенного способа, и Task 20 и примеры с секретами меняются.
- **Файлы:** `src/bisquite/infrastructure/build/secrets.py` (новый: временный файл хоста 0600 в каталоге 0700,
  `finally`-удаление, набор `--run-command`/`--upload` для tmpfs `/run/secrets`), `src/bisquite/infrastructure/parsers/vmfile_parser.py`
  (только разбор `RUN_COMMAND --mount=type=secret,id=…[,target=…]`), `tests/test_build_secrets.py`,
  `tests/build/VMFILE.secret-raw`, `tests/build/test_secret_not_in_qcow2.sh` (или сценарий в `tests/build/`).
- **Тесты сначала:** unit — разбор `--mount`, текст команд `virt-customize` не содержит значения (только путь),
  временный файл удалён и при исключении; `test-build` — маркер (случайные 32 байта) передан `--secret`, шаг
  `cat /run/secrets/x >/dev/null`; затем `qemu-img convert -O raw` + `grep -c` маркера в raw, журнале сборки
  (`--debug`), OCI-манифесте и метках.
- **Приёмка:** четыре счётчика = 0 — вывод в отчёте задачи. ≠ 0 — задача закрывается **решением**: в план вписывается
  «`--mount` вне 3.0.0», Task 20 сужается до ключей шагов, примеры с секретами — без секретов (запасного пути через
  текст команды нет).

## Task 2: [cream] ADR-0004 и манифест 1.2.0: `every_boot`, `scope: build`, обобщённый `checkSince` (день 1)
- **Файлы:** `docs/adr/0004-bundled-cream-contract-1-2.md` (новый; фаза сама, `cream check`, `cream.json`, отметка
  configure, код `20` configure, `scope: build`, `every_boot` — минор `cream/v1`), `docs/adr/README.md`,
  `internal/manifest/{parse,validate}.go`, `internal/manifest/*_test.go`, `internal/keytype/spec.go` (`ScopeBuild`),
  `testdata/invalid/` (новые фикстуры), `tools/contract-fixtures*` при наличии.
- **Тесты сначала:** `every_boot: true` + `cream: ">=1.1.0"` → 65 `manifest.invalid`, `details.field =
  hooks.configure.every_boot`, `since = 1.2.0`; то же для `scope: build`; с `">=1.2.0"` — разбор успешен;
  `scope: bogus` → «допустимо config, install или build»; таблица `checkSince` «поле → версия» покрывает стадии 1.1.
  Контрактная фикстура: манифест с `every_boot` и `cream: ">=1.2.0"` на исполнителе 1.1.4 даёт
  `platform.cream_version` (68), а не «неизвестное поле» — проверяется прогоном релизного 1.1.4 в CI-скрипте
  (`checkCream` до `decodeStrict`, спека §10.9).
- **Приёмка:** `make lint`, `make test PKG=./internal/manifest/... ./internal/keytype/...` зелёные; ADR в индексе.

## Task 3: [cream-extensions] Аудит 14 хуков configure по А1–А6 и гейт `lint-hooks` в режиме warn (день 1)
- **Файлы:** `docs/research/2026-10-21-configure-audit.md` (таблица), `docs/README.md`, `tools/lint-hooks.sh`,
  `tools/test-lint-hooks.sh`, `tools/testdata/lint-hooks/{every-boot-missing,every-boot-excepted}/`.
- **Аудит:** все 14 хуков каталога 5.2.0: `chromium-kiosk`, `code-server`, `desktop`, `docker`, `firewall`,
  `jetson-stats`, `kiosk`, `pixelflux-tegra`, `pixelflux-v4l2m2m`, `rustdesk`, `selkies`, `vino-vnc`, `wallpaper`,
  `x11vnc`. Таблица: расширение → стадия → признаки А1–А6 (спека §10.7) с цитатой строки → `every_boot` да/нет →
  обоснование → нужен ли хуку `cream configure --again` вместо прямого `systemctl restart`. Предварительная выборка
  grep (подтвердить чтением): А2/А3 — `docker`, `jetson-stats`, `kiosk`, `rustdesk`, `code-server`,
  `pixelflux-tegra`, `wallpaper`; А4 — `desktop`, `firewall`, `kiosk`, `rustdesk`; А1 — `firewall`, `jetson-stats`,
  `kiosk`, `rustdesk`; А6 — `docker`, `jetson-stats`, `kiosk`, `pixelflux-*`, `rustdesk`.
- **Гейт:** `lint-hooks` ищет текстовые признаки А1–А4 (спека §10.7) в `hooks/configure`; нет `every_boot: true` и нет
  `# cream-lint: every-boot-not-needed <причина>` → находка. Режим `LINT_EVERY_BOOT=warn` (умолчание до Task 19):
  каталог 5.2.x с пином cream 1.1.4 не может объявить `every_boot`.
- **Тесты сначала:** фикстура «`getent passwd` без `every_boot`» → находка (warn: код 0 + строка; error: код ≠ 0);
  с комментарием-исключением → чисто; без признаков → чисто.
- **Приёмка:** `make ci` cream-extensions зелёный; таблица на 14 строк без пустых решений.

## Task 4: [cream] Автоопределение фазы
- **Файлы:** `internal/engine/phase.go` (новый), `internal/engine/phase_test.go`, `internal/engine/engine.go`
  (`Phase()` → `--phase` → `CREAM_PHASE` → автоопределение), `internal/paths` (путь `/proc/1/comm` под `Root`),
  `internal/cli/cmd_install.go` (флаг `--phase` для всех трёх команд), `internal/cli/envelope.go` (`phase`, `reason`).
- **Тесты сначала:** подменённые `/proc/1/comm` + `/run/systemd/system` → `live`; `comm` ≠ systemd → `build`
  (`reason: pid1=<имя>`); нет `/proc` → 64 `phase.undetermined`; флаг побеждает всё; `CREAM_PHASE` родителя
  побеждает автоопределение.
- **Приёмка:** `make test PKG=./internal/engine/... ./internal/cli/...` зелёный, golden `--json` обновлены.
- **Рулинги исполнения:** systemd без `/run/systemd/system` (chroot с `/proc` хоста) — `build`, `reason:
  pid1=systemd,no-run` (ADR-0004 п.1 «иначе — build»). «Журнал» §10.1 — строка `cream: фаза <ф> (<причина>)` в
  stderr (кроме `-q`), её видит журнал сборки bisquite; набор полей `history.jsonl` — контракт, причина туда не
  пишется. Для Task 5: `Engine.buildPhase()` копирует решённую фазу — копии ставить `phase = build` явно.

## Task 5: [cream] `install`/`upgrade`/`remove` в фазе build из источника
- **Файлы:** `internal/engine/{build,install,upgrade,remove}.go`, их `_test.go`, `internal/cli/{cmd_install,cmd_upgrade,cmd_remove}.go`,
  `internal/cli/cmd_install_build_test.go`. `--from-dir`/`--record` и `resolve` остаются, помечены устаревшими.
- **Тесты сначала:** фаза build (через `CREAM_PHASE`) + источник `path` → дерево из кеша источника, `policy-rc.d`,
  configure только `enable`; `@версия` не та → 66; незакрытая зависимость → 67, `--with-deps` добирает; повтор —
  noop; `upgrade`/`remove` в build; stderr содержит одну человекочитаемую строку отказа с `error.code` без
  `--verbose` (спека §17 р. 4).
- **Приёмка:** тесты пакетов зелёные; старые тесты `--from-dir` не тронуты.

## Task 6: [bisquite] Пакет `bisquite-cream-bundle`, `make bundle-deb`, self-check
- **Файлы:** `packaging/bundle/` (новый: control, сборочный скрипт), `Makefile` (`bundle-deb`),
  `src/bisquite/data/cream-pin.env` (+ `CATALOG_VERSION`, `CATALOG_SHA256`, суммы `tar.gz` cream),
  `.github/workflows/ci-release.yml`, `.github/workflows/extensions-bump.yml` (пишет в тот же файл),
  `packaging/deb/DEBIAN/control.in` (`Depends: …, bisquite-cream-bundle (= …)`), self-check компонента «Расширения
  cream» (`application`/`infrastructure` — по месту нынешней проверки), `tests/test_bundle_pin.py`,
  `tests/test_self_check_cream.py`.
- **Пины на этом шаге** — текущие (cream 1.1.4, каталог 5.2.0): пакет и проверки готовы до выпуска 1.2.0; подъём —
  Task 22.
- **Тесты сначала:** сумма не сошлась → отказ сборки пакета; `bundle.env` строго `KEY=VALUE`; self-check: файл
  бандла изменён → красный; `cream --version` ≠ пин → красный; без сети.
- **Приёмка:** `make bundle-deb` строит `bisquite-cream-bundle_1.1.4+cat5.2.0-1_all.deb`; в контейнере Debian 12
  оба пакета ставятся, `bs self-check` зелёный; `apt remove bisquite && apt autoremove` убирает бандл и cream.

## Task 7: [cream] `cream check` (обобщение `resolve`)
- **Файлы:** `internal/resolve/check.go` (новый), `internal/resolve/check_test.go`, `internal/cli/cmd_check.go`
  (новый), `internal/cli/commands.go`, `internal/cli/testdata/golden/check-*.json`, `internal/cli/cmd_meta.go`
  (`resolve` → обёртка), `docs/reference/cli.md` (раздел `check`).
- **Тесты сначала (golden):** нет расширения/версии (66), конфликт и незакрытая зависимость (67), обязательный
  параметр не задан и секрет только по имени (64/65 по таблице cream-core §11.3), чужая платформа (68), перенос
  состояния между `--step`, `--installed` из формата `cream.extensions`, `targets.build` не допускает build,
  `platform.commands` — только предупреждение; архивы `--catalog` читаются без распаковки на диск; ничего не пишет
  (каталог `Root` пуст после прогона).
- **Приёмка:** golden зелёные; замер на каталоге 5.2.0 и 10 шагах < 1 с (тест-бенчмарк с порогом).
- **Рулинги исполнения (2026-10-21):** `resolve` не переписан поверх `check` — у него иная семантика параметров
  (голый `KEY`, Р28) и выход record.json с дайджестами деревьев; общий код — граф `resolve.Resolve`, `resolve`
  помечен устаревшим (cream 2.0 уберёт), `cmd_meta.go` не тронут. В cream/v1 у каждого ключа валидный `default`,
  «обязательных» параметров нет: проверяются известность ключа, тип значения, секрет только `KEY=-` (64
  `secret.in_argv`), один stdin. `targets.build: none` — предупреждение `install.build_none`, как у `install` в
  фазе build. Без `--installed` база неизвестна (§7.3): незакрытое — `check.maybe_installed`, не ошибка; пустая
  база — `--installed` с `[]`. Нестандартная строка — `skipped` + `check.nonstandard`, хвост
  `< /run/secrets/<id>` — стандартный. Ошибка шага не каскадирует: имена упавшего шага «неизвестны».
  Замер на настоящем каталоге 5.2.0 и 10 шагах — 0,06 с.

## Task 8: [cream] `/etc/image-labels.d/cream.json`
- **Файлы:** `internal/state/labels.go` (новый), `internal/state/labels_test.go`, `internal/state/store.go` (вызов
  после фиксации реестра), `internal/paths` (`ImageLabels()`).
- **Тесты сначала:** форма §8.2 спеки; нет параметров, `params_digest`, путей; атомарность (временный + rename);
  ошибка записи → предупреждение `labels.write_failed`, операция успешна; после `remove` файл переписан.
- **Приёмка:** тесты `state` и `engine` зелёные.

## Task 9: [bisquite] Удаление слоя `EXTENSION`, адаптера и источников на хосте
- **Файлы (удаление по спеке §11.1):** `domains/build/ports/cream.py`, `infrastructure/cream/{staging,guest_deb}.py`,
  `domains/build/services/extension_router.py`, `domains/build/models/{cream,extensions,extension_ref}.py`,
  `infrastructure/config/extensions.py`, `src/bisquite/data/extensions.yaml`, ветка `EXTENSION` в
  `infrastructure/build/builders/linux.py`, запрет в `openwrt.py`, lineage cream в `domains/build/provenance.py`,
  `bs extension sources|sync|check` в `cli/commands/extension.py`; тесты этих модулей; `tests/fixtures/cream/`.
  `infrastructure/cream/{cli,errors}.py` сужаются до `--version` и `check`. `vmfile_parser.py`: `EXTENSION` →
  словарь отказов с подсказкой (спека §6.3).
- **Тесты сначала:** `EXTENSION desktop@2.1.0 K=V` → ошибка разбора с текстом подсказки и номером строки;
  `extensions.yaml` в `/etc/bisquite` → предупреждение «удалён в 3.0»; `bs extension sync` → «нет такой команды».
- **Приёмка:** `make lint`, `make typecheck`, `make test` bisquite зелёные; `grep -r extension_router src/` пуст.
  Примеры на этом шаге не валидны — CI-проверка примеров временно ограничена фикстурами (возвращается в Task 24).

## Task 10: [cream] `scope: build` в движке
- **Файлы:** `internal/config/domain.go`, `internal/engine/install.go`, `internal/engine/set.go`, тесты;
  `internal/runner/env.go` (окружение хука).
- **Тесты сначала:** ключ `scope: build` через `K=V`, `--params-file`, `K=- < файл` попадает в окружение хука
  `install`/`upgrade` и **не** попадает в `config`, `secrets`, `params_digest`, `history.jsonl`, `cream.json`; `cream
  set/get/show` → 64 `config.scope_build`; configure его не видит; обязательный ключ без значения на `install` → 64.
- **Приёмка:** тесты `config`, `engine`, `runner` зелёные.

## Task 11: [cream] Отметка выполнения configure, `secrets_rev`, `--force`, `--again`, `every_boot`
- **Файлы:** `internal/state/configured.go` (новый), `internal/state/record.go` (`secrets_rev`),
  `internal/engine/configure.go`, `internal/engine/set.go` (рост `secrets_rev`), `internal/engine/remove.go`
  (удаление отметки), `internal/cli/cmd_config.go` (`configure --force [--all]`, `--again`), `cream show`/`list --json`
  (`configured`), тесты.
- **Тесты сначала:** каждая строка таблицы спеки §10.7: совпало → хук не запущен, строка журнала «пропуск»; версия,
  `ext_digest`, `params_digest`, `secrets_rev`, `machine_id` → запуск; `--force` → запуск и перезапись; сбой → отметка
  удалена; `remove` → удалена; `every_boot` → запуск всегда, отметка пишется; причина в `/run/cream/<ext>.reason`
  ≠ `boot` → запуск мимо отметки; `--again` пишет `followup` и перезапускает юнит (фейковый systemctl); фаза build
  отметок не пишет; отметка без значений секретов, `machine_id` — sha256.
- **Приёмка:** тесты `state`, `engine`, `cli` зелёные; golden `show`/`list`.

## Task 12: [bisquite] Скрытый шаг cream, `--no-cream`, `CREAM off`, метки
- **Файлы:** `infrastructure/build/builders/linux.py`, `infrastructure/build/cream_step.py` (новый: встроенный POSIX-
  скрипт и набор аргументов `virt-customize`), `infrastructure/parsers/vmfile_parser.py` (`CREAM off`),
  `cli/commands/image.py` (`--no-cream`), `domains/build/provenance.py` (`bisquite.cream.*`, `bisquite.os-release`),
  тесты `tests/test_cream_step.py`, `tests/build/VMFILE.cream-*`.
- **Тесты сначала:** unit — быстрый путь по меткам базы → `noop (из базы)` без appliance; не deb-семейство → пропуск
  с записью; `CREAM on`/другое значение → ошибка разбора; ключ шага = sha256(версия бандла + арх); POSIX-скрипт под
  `dash`: нет cream → `dpkg -i`; та же → ничего; старше → обновление; новее → отказ с текстом спеки §5.1;
  `test-build` (контроллер) — журнал `#0 … (скрытый шаг)`, метки, повторная сборка потомка — `noop`.
- **Приёмка:** unit зелёные; `make test-build` на одном VMFILE зелёный (вывод в отчёте).

## Task 13: [cream] Код `20` хука configure — «пропущено»
- **Файлы:** `internal/engine/configure.go`, `internal/engine/hook.go`, тесты, `docs/reference/exit-codes.md`.
- **Тесты сначала:** таблица спеки §10.8: `20` в configure → cream 0, `status: skipped`, отметка не пишется, прежняя
  не удаляется, запись `skipped` в `history.jsonl`; `20` в `install` без `--allow-skip` → 68 по-прежнему.
- **Приёмка:** тесты `engine` зелёные.

## Task 14: [bisquite] Сбор `/etc/image-labels.d/*.json` в метки OCI
- **Файлы:** `infrastructure/build/guest_labels.py` (новый), `infrastructure/build/builders/linux.py` (вызов перед
  sysprep), тесты `tests/test_guest_labels.py`.
- **Тесты сначала:** правило спеки §8.1: префикс `<имя>.`; `bisquite.`/`org.opencontainers.` → файл пропущен,
  `bisquite.labels.skipped`; конфликт с `LABEL`/`--label` → автор побеждает; значение > 8 КиБ или сумма > 64 КиБ →
  файл пропущен целиком; невалидный JSON → пропущен.
- **Приёмка:** unit зелёные; один `virt-cat`/`guestfish`-вызов на сборку (проверка мока).

## Task 15: [cream] e2e и integration для 1.2.0
- **Файлы:** `test/build/{scenario.sh,Dockerfile}`, `test/integration/scenario/*`, `Makefile` (при необходимости).
- **Сценарии:** `virt-customize` Debian 12 — `dpkg -i`, `source add --path`, `cream install` **без** `--phase` → build,
  повтор — noop, потомок `FROM` закрывает `depends`, `scope: build` не на диске, отметок configure нет, `cream.json`
  есть; systemd-контейнер — автоопределение `live`, две перезагрузки → configure один раз, `set` → снова, `20` →
  повтор, падающий → каждый раз, `every_boot` → каждый раз, `--again` из хука → второй прогон в той же загрузке.
- **Приёмка:** `make test-build` и `make test-integration` зелёные (контроллер, по одному; вывод в отчёте).

## Task 16: [bisquite] Предсборочная проверка `cream_lint`
- **Файлы:** `domains/build/services/cream_lint.py` (новый, без знания формата манифеста), `cli/commands/image.py`
  (раздел `cream` в `validate`, вызов до загрузки базы в `build`), `infrastructure/cream/cli.py` (`check`),
  тесты `tests/test_cream_lint.py` (golden по ответам `cream check` из Task 7).
- **Тесты сначала:** стандартная форма (спека §7.2) → шаги `--step`; `;`/`&&`/`|`/`$…` и `source add --path` →
  пропуск с заметкой; `source add --archive` без `--sha256` → ошибка; шаги внешнего источника → `skipped` с заметкой,
  зависимости на их имена — предупреждение (§7.4); `--os` из `bisquite.os-release` → `LABEL os/version` → нет;
  `--installed` из метки базы, база не локально — предупреждения; `CREAM off` без cream в базе → ошибка; `--strict`.
- **Приёмка:** unit зелёные; `bs image validate --strict` на фикстурах < 1 с.

## Task 17: [cream] Документация cream 1.2.0 и выпуск 1.2.0
- **Файлы:** `docs/reference/{cli,manifest,exit-codes,json-output,files-and-environment}.md`,
  `docs/guides/{build-phase,write-extension,diagnostics}.md`, `docs/architecture.md`, `docs/README.md`, `README.md`,
  `docs/STATUS.md` (контроллер), man-страницы (`make man`).
- **Содержание:** фаза сама, `check`, `cream.json`, отметка configure и правило «состояние до перезагрузки,
  пользователи, железо → `every_boot`», `--again`, код `20`, `scope: build`; build-phase переписан под
  `RUN_COMMAND cream install`.
- **Выпуск:** слияние `feature/cream-1.2` в `main` с `make ci` зелёным; release-please → **1.2.0**; ассеты и
  `SHA256SUMS` проверены по runbook `docs/runbooks/01_release.md`; `cream-bump` открыл PR в Stvor, cream-extensions,
  bisquite.
- **Приёмка:** `make ci` зелёный; выпуск v1.2.0 с 4 ассетами и `SHA256SUMS`.

## Task 18: [bisquite] `bs image history`, `bs image diff`, `bs extension ls`
- **Файлы:** `cli/commands/image.py`, `domains/build/queries/{image_history,image_diff}.py` (новые),
  OCI-конфигурация (поле `history` — где пишутся метки сейчас), `cli/commands/extension.py` (`ls` по снимку бандла
  через cream хоста), тесты.
- **Тесты сначала:** `history` — шаги с директивой, `hidden`, digest8, длительностью; `diff` по меткам — добавлено /
  удалено / версия / дайджест при той же версии, база и cream; `--deep packages` — разбор `dpkg/status` (фикстуры);
  `--deep files` → «нет такого режима»; образа нет локально → отказ с подсказкой `bs image pull`; `--format json`.
- **Приёмка:** unit зелёные.

## Task 19: [cream-extensions] Каталог под cream 1.2.0
- **Файлы:** `extensions/debian/*/extension.yaml` (по таблице Task 3: `every_boot`, `cream: ">=1.2.0"`), хуки с А4 —
  на `cream configure --again`, `tools/cream-pin.env` (1.2.0), `tools/lint-hooks.sh` (умолчание `error`),
  ключи-токены зеркал → `scope: build` (если есть по аудиту), `tools/check.sh` (CI: `cream check` по примерам
  каталога), `CHANGELOG`/release-please.
- **Тесты сначала:** гейт `error` красный до правки манифестов; `cream validate --catalog .` с пином 1.2.0 зелёный;
  `make test-live EXT=rustdesk` и `firewall` — две перезагрузки (L-раннер) — хук отработал на второй.
- **Приёмка:** `make ci` зелёный; выпуск минора (5.3.0) с `SHA256SUMS`; `extensions-bump` открыл PR в bisquite и
  Stvor.

## Task 20: [bisquite] Ключи шагов по содержимому; секреты сквозь `cream install`
- **Файлы:** `infrastructure/build/builders/linux.py` (ключи шагов), `domains/build/cache_digests.py` (или текущее
  место дайджеста), `infrastructure/build/secrets.py`, тесты `tests/test_cache_and_digests.py`,
  `tests/build/VMFILE.secret-cream`.
- **Тесты сначала:** `COPY_IN`/`UPLOAD` — отпечаток дерева источника; `RUN_SCRIPT` — содержимое; `RUN_COMMAND` —
  `id` секретов, не значения; секрет не передан → отказ до сборки; передан и не использован → предупреждение;
  `test-build`: `cream install … --params-file /run/secrets/x` с ключом `scope: build` → маркер не в raw qcow2,
  журнале, манифесте, метках (повтор теста Task 1 на cream 1.2.0).
- **Приёмка:** unit зелёные, `make test-build` зелёный (контроллер). Если Task 1 закрыта отрицательно — только ключи.

## Task 21: [Stvor] Пин cream 1.2.0 и каталога
- **Файлы (Stvor):** пины в `.env.example` и где их держит набор бутстрапа (runbook 10 §10.1), PR от `cream-bump`.
- **Шаги:** PR слит после `make ci`; **выкладка на каждый экземпляр** по runbook 10 §10.2 (`make init-env` + ручной
  подъём `CREAM_VERSION`, `make fetch-teleport`, `make up`, `make smoke`), проверка §10.3; `scripts/robots-gate.sh`
  на каждом экземпляре зелёный.
- **Приёмка:** в отчёте — список экземпляров с отметкой «пин выложен, robots-gate зелёный» и выводом гейта. Это
  условие Task 26.

## Task 22: [bisquite] Подъём пина бандла
- **Файлы:** `src/bisquite/data/cream-pin.env` (cream 1.2.0, каталог 5.3.0), PR `cream-bump`/`extensions-bump`.
- **Приёмка:** `make bundle-deb` → `bisquite-cream-bundle_1.2.0+cat5.3.0-1_all.deb`; self-check зелёный; unit и
  `make test-build` на одном VMFILE зелёные.

## Task 23: [cream-extensions] Документация
- **Файлы:** `README.md`, `docs/README.md`, `docs/extensions.md` → раскладка в стиле популярных проектов
  (`docs/getting-started.md`, `docs/guides/{use-in-bisquite,write-extension,configure-lifecycle,live-runs}.md`,
  `docs/reference/catalog.md` — таблица расширений с параметрами, генерируется скриптом из манифестов и проверяется в
  CI), `docs/runbooks/*` под новую схему.
- **Содержание:** установка через `RUN_COMMAND cream install` в VMFILE и `cream install` на живой машине; правило
  `every_boot` (А1–А6) и `--again`; `scope: build` для токенов. README: что это, быстрый старт в 5 строк, таблица
  расширений, ссылки — как README крупных проектов (badges выпуска/CI, разделы «Быстрый старт», «Документация»,
  «Участие»).
- **Приёмка:** `make ci` (вкл. проверку ссылок и сгенерированной таблицы) зелёный.

## Task 24: [bisquite] Переписывание 32 VMFILE примеров
- **Файлы:** 32 VMFILE с `EXTENSION` в `examples/dst/*`, `examples/build/{amd64,jetson,raspberry-pi}/*` и README
  примеров; `.github/workflows/ci.yml` (шаг `bs image validate --strict` по всем примерам с замером времени),
  `tests/test_examples_extensions.py`.
- **Правило:** `EXTENSION [src/]name[@v] K=V` → `RUN_COMMAND cream install [--source src] name[@v] K=V`; подряд
  идущие простые строки сливаются в одну, если порядок относительно `RUN_COMMAND` автора не меняется; секреты —
  `--mount`. OpenWrt-примеры не трогаются.
- **Тесты сначала:** тест «ни в одном примере нет `EXTENSION`»; validate каждого примера < 1 с.
- **Приёмка:** CI-шаг зелёный, замеры в журнале CI.

## Task 25: [bisquite] Документация, guide миграции, ADR
- **Файлы:** `README.md`, `docs/README.md` (новый индекс), `docs/getting-started.md` (новый),
  `docs/guides/{vmfile,compose,device,progress}.md`, `docs/guides/{extensions,secrets,image-labels}.md` (новые),
  `docs/guides/migration-3.0.md` (новый, спека §13.3 + **предупреждение операторам порталов Stvor**, §12 п. 3),
  `docs/reference/{cli,vmfile}.md` (новые), `docs/dev/*`, `docs/runbooks/{01_robot-image,03_bump-pins,04_release}.md`
  (чек-лист выпуска: инвариант Stvor), `docs/adr/NNNN-bundled-cream.md` (bisquite везёт cream и каталог; `EXTENSION`
  удалена).
- **Стиль:** как у популярных проектов (Docker/hadolint/Packer): README — что это, установка, пример VMFILE в
  10 строк, ссылки; документация по задачам: первые шаги → руководства → справочник → разработка.
- **Приёмка:** `make ci` bisquite зелёный (проверка ссылок, `test_device_docs_sync` и пр.).

## Task 26: [bisquite] Предвыпускная проверка и выпуск 3.0.0
- **Условие входа:** Task 21 закрыта — пин cream 1.2.0 выложен на каждый экземпляр Stvor, `robots-gate` зелёный.
  Нет — задача не начинается.
- **Шаги:** слияние `feature/bundled-cream` в `main` с `make ci`; сборка на PVE `root@192.168.202.206` (VMID
  990001–990003) трёх образов: `dst-base`, `dst-robot-ws` (amd64), один с секретом; робот из `dst-robot-ws` —
  регистрация в Stvor, `cream run teleport join`, `stvor-agent` (`robots-gate`); отдельно образ с cream новее пина →
  отказ скрипта Stvor; `bs image diff <образ 2.x> <образ 3.0>` — расширения и версии совпадают; ВМ удалены.
  Выпуск `feat!` → **3.0.0**, заметки выпуска с предупреждением операторам порталов.
- **Приёмка:** выпуск v3.0.0 с обоими пакетами и `SHA256SUMS`; отчёт с выводом гейтов.

## Task 27: [cream] Фаза OpenWrt (кратко)
По отдельной спеке (на основе `docs/specs/2026-10-20-openwrt.md`, PR #16; пишется перед фазой): семейство `openwrt`
(`ID=openwrt`, 25.12, `apk`), `internal/initsys` с `procd`, автоопределение фазы по `procd`, отметки configure в
`/opt/cream/var`, поставка `tar.gz`; минор cream.

## Task 28: [cream-extensions + bisquite] Фаза OpenWrt (кратко)
Каталог: корень `extensions/openwrt`, `amneziawg` (настройка `uci`), `podkop`, `luci-apps`. bisquite:
`OpenWrtBuilder` получает скрытый шаг из `tar.gz` бандла; OpenWrt-примеры — `RUN_COMMAND cream install`. Teleport
и Stvor на OpenWrt — вне фазы.

## Task 29: Финальная интеграция
- Сквозной `make ci` во всех четырёх репозиториях на `main` (cream, cream-extensions, bisquite, Stvor) — по одному,
  под семафором.
- Сборки примеров на PVE `root@192.168.202.206`, VMID 990001–990010: все 32 переписанных примера amd64 (Jetson и
  Raspberry Pi — `validate --strict` и сборка там, где есть железо/база), по 3–4 параллельно в пределах VMID;
  загрузка каждого образа, `cream list --json` показывает ожидаемый состав, вторая загрузка — configure пропущены
  (кроме `every_boot`); ВМ после себя удалены.
- `make test-build`/`make test-integration` cream, `make test-build` bisquite, `robots-gate` Stvor.
- STATUS всех репозиториев обновлён; спека получает отметку «реализована», план закрыт.
