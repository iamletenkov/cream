# План: OpenWrt-роботы в Stvor (Teleport + stvor-agent + регистрация через портал)

> 2026-10-22. Спека — [docs/specs/2026-10-20-openwrt.md](../specs/2026-10-20-openwrt.md) (принята, возобновлена
> 2026-10-22; В1–В7, Р1–Р10). PoC — [research/2026-10-22-openwrt-teleport-glibc-poc.md](../research/2026-10-22-openwrt-teleport-glibc-poc.md).
> Классификация — **архитектурная**: четыре репозитория (cream-extensions, stvor-agent, Stvor, bisquite), новый ассет
> каталога, новая платформа роботов. cream и openwrt-builder **не меняются** (спека §4.1, §4.6).
> Цикл на задачу: TDD → `make lint` + тесты по скоупу → reviewer → коммит в ветке задачи → слияние контроллером в
> ветку дорожки → `make ci` дорожки после волны.
>
> **Исполнителю:** после выполнения отметь свою задачу `- [x]` в Progress Tracker **в том же коммите**. План живёт в
> репозитории cream (ветка `docs/openwrt-spec` до слияния, потом `main`); исполнитель другого репозитория отметку не
> делает — её коммитит контроллер сразу после слияния задачи.

## Progress Tracker
<!-- Пункты разделены пустой строкой: параллельные отметки сливаются без конфликта. -->

- [ ] Task 1: [контроллер] `tsh ssh` на PoC-узле OpenWrt (срез 0, день 1; провал — стоп, решение по варианту B)

- [ ] Task 2: [cream-extensions] Ядро `lib/teleport-core.sh`: вынос из `debian/teleport`, синхронизация копий, `debian/teleport` 4.2.0 без изменения поведения

- [ ] Task 3: [cream-extensions] Второй снимок: корень `extensions/openwrt-stvor`, `cream-catalog.openwrt.yaml`, ассет `cream-extensions-openwrt-<v>.tar.gz`

- [ ] Task 4: [stvor-agent] `initsys` (systemd/procd), пути по семейству, свой `machine-id`, телеметрия без PSI — поверх телеметрии (согласовать с сессией Stvor)

- [ ] Task 5: [stvor-agent] Ассет `stvor-agent_<v>_linux_<arch>.tar.gz`, e2e на корневой ФС OpenWrt, `SECURITY.md`, выпуск (согласовать с сессией Stvor)

- [ ] Task 6: [cream-extensions] Расширение `openwrt-stvor/teleport`

- [ ] Task 7: [cream-extensions] Расширение `openwrt-stvor/stvor-agent`

- [ ] Task 8: [cream-extensions] Регресс `debian/teleport` (живой прогон) и выпуск каталога с двумя снимками

- [ ] Task 9: [Stvor] Набор Teleport `openwrt-<arch>` на стенде: `build-teleport-openwrt.sh`, пины glibc, `TELEPORT_OPENWRT_SHA256_<ARCH>` (согласовать с сессией Stvor)

- [ ] Task 10: [Stvor] Набор бутстрапа по семейству и ветка `openwrt` скрипта установки (согласовать с сессией Stvor)

- [ ] Task 11: [Stvor] `POST` на install-эндпоинте, `command_openwrt`, строка OpenWrt на `/robots/new` (согласовать с сессией Stvor)

- [ ] Task 12: [Stvor] `robots-gate`/`stand-seed` на OpenWrt-ВМ, runbook 10, ADR Stvor, подъём пинов (согласовать с сессией Stvor)

- [ ] Task 13: [bisquite] Пример образа OpenWrt-робота и профиль деплоя 1 ГБ / 4 ГБ

- [ ] Task 14: [контроллер] Приёмка amd64: OpenWrt-ВМ на PVE — одна команда, «в сети», SSH через SSO, метки, reboot, `sysupgrade`

- [ ] Task 15: [контроллер] arm64: решение по AAVMF на PVE (Р10), приёмка на ВМ `armsr` или перенос на RPi владельца

- [ ] Task 16: Финальная интеграция + `make ci` во всех затронутых репозиториях

## Правила плана
1. **Не более 3 исполнителей одновременно** (VM 12 CPU / 29 ГБ, история OOM): дорожка **E** (cream-extensions),
   дорожка **A** (stvor-agent), дорожка **S** (Stvor — исполнитель сессии Stvor). Задача 13 (bisquite) — первому
   освободившемуся из E/A. Каждый — в своём worktree, `git add <пути>`, `git commit -F … -- <пути>`, без `git add -A`.
2. **Дорожка S — «согласовать с сессией Stvor».** Stvor-задачи (9–12) исполняет исполнитель сессии Stvor в worktree,
   ветка **`feature/openwrt-robots`** от `main`. Его зона: `install_script.py`, `downloads.py`/`Bootstrap`,
   `install_code.py`, `scripts/{fetch-teleport-tarball,build-teleport-openwrt,stand-seed,robots-gate}.sh`, страница
   `/robots/new` (строка команды). Ограничения сессии Stvor:
   - `robot_api.py`: новый `POST`-маршрут — **отдельной функцией в конце файла** (там же сессия Stvor добавила
     `POST /robot-api/v1/telemetry-token`);
   - `.env.example`: пины cream tar.gz / снимка openwrt / наборов Teleport openwrt / stvor-agent tar.gz — **отдельными
     строками**; пин `STVOR_AGENT_*` до 4.1.0 поднимает сессия Stvor;
   - **compose и Traefik не трогать** (у сессии Stvor — `ingest/vm-*` и роутеры `/robot-api/v1/{metrics,inventory,events}`);
   - кто сливает в `main` вторым — тот ребейзит.
3. **stvor-agent (задачи 4–5)** — ветвиться от `main` **после** squash-слияния телеметрии; выпуск 4.1.0 делает сессия
   Stvor, когда портал принимает телеметрию (она сообщит). OpenWrt-поддержка выходит в 4.1.0, если успевает до него,
   иначе — следующим минором (4.2.0); номер решается с сессией Stvor в момент выпуска.
4. **Общий стенд Stvor.** Перед своей выкладкой / `make up` / `robots-gate` сессия Stvor пишет сюда; получив сигнал,
   контроллер **приостанавливает все операции со стендом** (join, `tctl`, Keycloak, портал) и **убирает за собой**
   пользователей, токены и узлы `owrt-*` (`tctl users rm`, `tctl tokens rm`, `tctl rm node/<uuid>`, пользователь
   Keycloak `owrt-*`), затем подтверждает «стенд чист». Возобновление — по ответу сессии Stvor. Тяжёлые шаги (`make up`,
   сборка образов, `ci-full`) — только через `make heavy`, по одному.
5. **Пользователи стенда** — только префикс `owrt-*` (разрешение владельца на CRUD); живут не дольше проверки.
6. **PVE** (`root@192.168.202.206`): VMID 990001–990010, ВМ после себя удалять (`qm stop` + `qm destroy` своих VMID);
   пакеты на ноду не ставить без согласия владельца (Р10).
7. Отказ классификатора прав — стоп и доклад, без обходов.

## Порядок и зависимости
| Волна | E (cream-extensions) | A (stvor-agent) | S (Stvor) | Контроллер |
|---|---|---|---|---|
| 0 | T2 | T4 (после слияния телеметрии) | — | **T1** |
| 1 | T3 | T5 | T9 (после T1) | слияния, отметки |
| 2 | T6 (после T1–T3) | T13 | T10 (локальные пины из сборок E/A) | |
| 3 | T7 (после T5) | — | T11 | |
| 4 | T8 (выпуск каталога) | — | T12 (пины выпусков) | |
| 5 | — | — | — | T14, T15, T16 |

Выпуски: stvor-agent (T5) → `cream-extensions` (T8, пин агента в `openwrt-stvor/stvor-agent/files/pin.env`) → пины Stvor
(T12). Провал T1 останавливает T6, T9 и всё после них; T2–T5 не зависят от варианта Teleport.

## Task 1: [контроллер] `tsh ssh` на PoC-узле (срез 0)
Цель — закрыть единственный непроверенный пункт варианта A (спека §2.3, риск 1).
1. ВМ PVE (VMID из 990001–990010, 1 ГБ ОЗУ, диск 4 ГБ) из `openwrt-25.12.6-custom-x86-64-generic-ext4-combined-efi.qcow2`
   `openwrt-builder` v0.3.1 (sha256 сверить с `SHA256SUMS`); набор — как в PoC (debian:12, patchelf, 7 библиотек,
   `/opt/teleport/{bin,lib,data,tmp}`), init без `stdout/stderr`, `log.output: syslog`, `pam`/`enhanced_recording` off.
2. Стенд Stvor: токен `node` с TTL 30 мин, имя узла `owrt-poc-<дата>`, метка `owrt=poc`; временная роль `owrt-ssh`
   (`logins: [root]`, `node_labels: {owrt: poc}`) и локальный пользователь `owrt-ssh-<дата>` с ней (Р9);
   identity `tctl auth sign --ttl=1h` во временный каталог вне репозитория.
3. Проверки: `tsh ssh root@owrt-poc-… 'id; uname -a; echo $SHELL'`; интерактивная сессия с PTY (`tsh ssh -t`, `stty size`,
   выход); `tsh scp` туда и обратно; `tsh ssh -L` к порту на узле; запись сессии в журнале (режим node);
   во время сессии — `cat /proc/<pid дочернего teleport>/maps | grep -c '/lib/libc.so'` = 0, `environ` без `LD_PRELOAD`;
   журнал `logread -e teleport` без ошибок utmp/PAM, кроме ожидаемых предупреждений `wtmp`.
4. Уборка: пользователь, роль, токен, узел `owrt-*`, identity-файлы, ВМ. Итог — раздел «tsh ssh» в
   `docs/research/2026-10-22-openwrt-teleport-glibc-poc.md` (коммит в ветку плана).
**Приёмка:** все проверки п. 3 зелёные — дальше по плану; любая красная, не чинящаяся настройкой `teleport.yaml`, —
стоп, доклад владельцу, решение по варианту B (спека §3).

## Task 2: [cream-extensions] Ядро `lib/teleport-core.sh`
- Файлы: `lib/teleport-core.sh`, `lib/tests/test-teleport-core.sh`, `tools/sync-teleport-core.sh`, `tools/check.sh`
  (шаг «копии ядра равны `lib/`»), `extensions/debian/teleport/{files/teleport.sh,files/teleport-core.sh,extension.yaml,README.md}`.
- Вынести init-независимую часть (`render`, join, метки, `apps.d`, `bound_keypair`, managed-метки, `managed-status`)
  в ядро с точками слоя (§4.2 спеки: init, restart, журнал join, arch, data_dir, TMPDIR, имя тарбола, доп. ключи
  `teleport.yaml`) — функции, которые слой определяет до `source`.
- TDD: сначала тест «копия ≠ `lib/`» в `check.sh` и тесты ядра на текущем поведении (перенос существующих H-тестов);
  тесты `debian/teleport` — **без правок ожиданий**.
- `debian/teleport` → `4.2.0` (комментарий версии: «вынос ядра, поведение без изменений»); `check-version-bump` зелёный.
**Приёмка:** `make ci` cream-extensions зелёный; диф рендера `teleport.yaml` и набора юнитов до/после на фикстурах — пуст.

## Task 3: [cream-extensions] Второй снимок
- Файлы: `cream-catalog.openwrt.yaml`, `tools/catalog-snapshot.sh` (`CATALOG=openwrt` → ассет
  `cream-extensions-openwrt-<v>.tar.gz`, манифест → `cream-catalog.yaml` снимка), проверка «каждое расширение под
  `roots` одного из манифестов», `tools/cream-validate.sh` (оба представления), `tools/test-snapshot.sh`
  (воспроизводимость обоих), `.github/release_please/*` (extra-files — второй манифест), workflow выпуска (оба ассета в
  `SHA256SUMS`), `docs/` каталога (как подключить снимок OpenWrt).
- Пустой корень `extensions/openwrt-stvor/` до T6 — с `.gitkeep`, проверка допускает пустой корень.
**Приёмка:** `make ci` зелёный; два прогона `catalog-snapshot.sh` дают одинаковые байты обоих ассетов; `cream source add
… --archive cream-extensions-openwrt-<v>.tar.gz --sync` на корневой ФС OpenWrt (`make test-openwrt`-подобный прогон)
проходит, основной снимок по составу не изменился.

## Task 4: [stvor-agent] `initsys`, пути, `machine-id`, PSI (согласовать с сессией Stvor)
- Ветка от `main` после слияния телеметрии. `internal/systemd` → `internal/initsys` (`systemd`, `procd`), выбор по
  `/etc/os-release` под корнем (`ID`/`ID_LIKE`: `openwrt`, `lede`); procd — `/etc/init.d/<svc> enable|restart|stop|disable`.
- Пути по семейству: состояние `/opt/stvor-agent/state.json`, телеметрия `/opt/stvor-agent/telemetry/`; токены и lock — как есть.
- `machine-id`: на `openwrt` — `/opt/stvor-agent/machine-id` (создаётся при первом старте, 0600).
- Сборщики телеметрии: нет `/proc/pressure/*` — поле не шлётся, без ошибки в журнале на каждом тике.
- TDD: table-driven тесты выбора init и путей по фикстурам os-release; фейк procd; тест «без PSI».
**Приёмка:** `make ci` stvor-agent зелёный; на Debian поведение и пути без изменений (существующие тесты без правок).

## Task 5: [stvor-agent] tar.gz, e2e на OpenWrt, выпуск (согласовать с сессией Stvor)
- `make tarball` (бинарь, man, LICENSE, NOTICE; `SOURCE_DATE_EPOCH`, `gzip -n`), ассеты `stvor-agent_<v>_linux_{amd64,arm64}.tar.gz`
  в `SHA256SUMS`, `repro-check` для них; workflow выпуска.
- e2e: корневая ФС openwrt-builder v0.3.1 x86-64 (`docker import`, sha256 закреплён, как `make test-openwrt` cream) +
  `fakeportal`: `enable` пишет токен и зовёт procd-фейк/скрипт, цикл держит метки, телеметрия уходит, состояние в `/opt`.
- `SECURITY.md`: на OpenWrt нет песочницы юнита; телеметрия — под `stvor-telemetry`.
- Выпуск — по правилу 3 плана.
**Приёмка:** e2e OpenWrt зелёный в CI; ассеты в релизе; `tools/agent-bump.sh` каталога читает их (контракт имён).

## Task 6: [cream-extensions] `openwrt-stvor/teleport`
- `extension.yaml` (`family: openwrt`, `cream: ">=1.3.0"`, arch `[amd64, arm64]`, `os: [{id: openwrt, version: ">=25.12"}]`,
  `platform.commands: [bash, curl, tar, sha256sum]`, **те же `config.keys` и действия**, что у debian), `files/teleport.sh`
  (слой procd), `files/teleport-core.sh` (копия), `files/teleport.init` → `/etc/init.d/teleport`,
  `files/keep.d` → `/lib/upgrade/keep.d/teleport`, хуки `install/apply/upgrade/remove`, README.
- Решения PoC (спека §3) — тестами: в init нет `stdout`/`stderr`; `TMPDIR=/opt/teleport/tmp` в `procd_set_param env`;
  `teleport.yaml` с `log.output: syslog`, `pam.enabled: false`, `enhanced_recording.enabled: false`, `data_dir: /opt/teleport/data`;
  маркер `/opt/teleport/.cream-owned`; `remove` снимает всё, кроме того, что снимать запрещает контракт; `apply` перезапускает
  фоном (`validate.apply_blocking` молчит); `validate.openwrt_tmpfs` молчит.
- Тесты: H-тесты слоя + сценарий на корневой ФС OpenWrt в docker (install из локального набора → render → `managed-status`).
**Приёмка:** `make ci` зелёный; ручной прогон на ВМ PVE: `cream install teleport …` + `cream run teleport join` → узел в
`tctl nodes ls`, reboot — онлайн без нового секрета; уборка `owrt-*`.

## Task 7: [cream-extensions] `openwrt-stvor/stvor-agent`
- По образцу `debian/stvor-agent`: `files/pin.env` (tar.gz по архитектурам; `tools/agent-bump.sh` пишет оба пина),
  `files/agent.sh`, init-скрипты `/etc/init.d/stvor-agent{,-telemetry}` (procd, `respawn`), хук `install` создаёт
  `stvor-telemetry` через `user_add`/`group_add_next` (`/lib/functions.sh`), `remove` — всё обратно; ключ
  `STVOR_AGENT_TARBALL` вместо `STVOR_AGENT_DEB`; `depends: [teleport]` как у debian.
- Тесты: H-тесты + сценарий на корневой ФС OpenWrt.
**Приёмка:** `make ci` зелёный; на ВМ после T6 агент включается `stvor-agent enable`, метка из портала доезжает.

## Task 8: [cream-extensions] Регресс и выпуск каталога
- `make test-live` для `debian/teleport` 4.2.0 (контейнер с systemd) — зелёный.
- Выпуск минора каталога (оба ассета); цепочка подъёма пинов (`ci-release` → PR в Stvor) открывает PR со
  `CREAM_EXTENSIONS_*`; пин `CREAM_EXTENSIONS_OPENWRT_SHA256` дописывает T12.
**Приёмка:** релиз с `cream-extensions-<v>.tar.gz`, `cream-extensions-openwrt-<v>.tar.gz`, `SHA256SUMS`.

## Task 9: [Stvor] Набор Teleport `openwrt-<arch>` (согласовать с сессией Stvor)
- `scripts/build-teleport-openwrt.sh` (debian:12 по digest; `.deb` `libc6`/`libgcc-s1` Debian 12 по пинам из
  `teleport/openwrt-glibc.env`; `dpkg-deb -x`; `patchelf`; детерминированный tar.gz), вызов из `fetch-teleport-tarball.sh`
  без `ARCH` при заданном `TELEPORT_OPENWRT_SHA256_<ARCH>`; сбой/чужая сумма — WARN без файла; `check-teleport-pin.sh` —
  формат новых пинов; `.env.example` — отдельные строки с комментарием.
- TDD: тест скрипта (bats/sh, как у соседних скриптов): воспроизводимость (две сборки — одни байты), состав набора
  (нет `tsh`), интерпретатор и `RUNPATH` у обоих бинарей, отказ на чужой пин.
**Приёмка:** `make lint` Stvor зелёный; на стенде набор amd64 собран и совпал с пином; arm64 — собран на amd64 без эмуляции.

## Task 10: [Stvor] Набор бутстрапа по семейству и скрипт (согласовать с сессией Stvor)
- `downloads.py`/`Bootstrap`: измерение семейства; пины `CREAM_TARBALL_SHA256_<ARCH>`, `CREAM_EXTENSIONS_OPENWRT_SHA256`,
  `STVOR_AGENT_TARBALL_SHA256_<ARCH>` (отдельные строки `.env.example`, env-preflight: пин без пары — отказ);
  `fetch-teleport` кладёт эти файлы на зеркало.
- `install_script.py`: ветка `openwrt` по §4.5 спеки — порядок проверок до изменений, `mktemp -d /opt/…`, `apk add`,
  cream tar.gz + `cream system init`, источник — снимок openwrt, маркер владения, отказ Р5 с перечнем недостающего.
- TDD: тесты рендера (pytest) + прогон скрипта под busybox `sh` на корневой ФС openwrt-builder в docker с фейками
  `cream`/`apk` (как существующие тесты скрипта): чистая машина, повтор (не отказ), Teleport без маркера (отказ),
  мало ОЗУ/места (отказ до изменений), нет набора для arch (отказ Р5), Debian-ветка без изменений.
**Приёмка:** `make test` по скоупу и `make lint` Stvor зелёные.

## Task 11: [Stvor] `POST`, команда и портал (согласовать с сессией Stvor)
- `robot_api.py`: `POST` install-эндпоинта — **отдельная функция в конце файла**, общая логика погашения с `GET`; аудит
  как у `GET` с заголовком; `HEAD`/`GET` без заголовка — по-прежнему без погашения.
- `install_code.py`: `render_command_openwrt`, `InstallCodeOut.command_openwrt` (пусто, если на экземпляре нет
  OpenWrt-набора ни для одной архитектуры); OpenAPI и типы фронтенда.
- `/robots/new`: вторая строка «OpenWrt» с копированием, по дизайн-системе.
- TDD: backend — POST гасит код один раз, повторный POST — отказ, предпросмотр GET не гасит, аудит; фронтенд — строка
  видна/скрыта.
**Приёмка:** `make test`, `make lint`, `make typecheck` по скоупу; design-review строки (обе темы).

## Task 12: [Stvor] Гейт, документация, пины (согласовать с сессией Stvor)
- `robots-gate.sh`/`stand-seed.sh`: сценарий OpenWrt — ВМ PVE из примера bisquite (T13), команда с портала, ожидание
  «в сети», `set-managed`-метка, уборка ВМ и `owrt-*`. Гейт — опциональный по переменной (ВМ PVE не всегда есть).
- runbook 10 (набор бутстрапа OpenWrt, пины), runbook 07 (CVE glibc → пины `openwrt-glibc.env`), ADR Stvor (следующий
  номер): «OpenWrt-роботы: семейство в наборе бутстрапа, `POST` install-эндпоинта» + строки индексов.
- Подъём пинов на выпуски T5 и T8.
**Приёмка:** `make ci` Stvor зелёный; гейт OpenWrt зелёный на стенде (с паузой по правилу 4).

## Task 13: [bisquite] Пример OpenWrt-робота
- `examples/build/amd64/openwrt-robot/` (VMFILE: `FROM` openwrt-builder v0.3.1 x86-64; без Teleport внутри; README:
  регистрация командой портала после деплоя), `examples/deploy/openwrt-robot.yml` (2 vCPU, **1 ГБ ОЗУ**, диск 4 ГБ);
  arm64-вариант — `examples/build/arm64-vm/openwrt-robot/` по образцу соседнего примера.
**Приёмка:** `make ci` bisquite зелёный; `bs build` + деплой на PVE дают машину, проходящую проверки скрипта Stvor
(ОЗУ, место, `bash`/`curl`).

## Task 14: [контроллер] Приёмка amd64
На свежей ВМ PVE из примера T13 (VMID 990001–990010), стенд со всеми пинами (после T12):
1. Портал: «Добавить робота» → строка OpenWrt → вставить в консоль ВМ → робот **«в сети»** в Stvor.
2. **SSH через Teleport SSO**: пользователь Keycloak `owrt-*` с доступом к пространству робота → `tsh login` (SSO) →
   `tsh ssh root@<робот>`.
3. **stvor-agent**: метка, изменённая в портале, доезжает до узла ≤ интервала агента; телеметрия видна (если 4.1.0 в пинах).
4. **Перезагрузка** → «в сети» без действий.
5. Повтор команды с новым кодом — не отказ.
6. **`sysupgrade`** тем же образом (Р4) → узел офлайн → «Новый секрет» → команда → **тот же узел** (`host_uuid`) онлайн.
7. Отказы: ВМ 512 МБ ОЗУ — отказ до изменений; экземпляр без набора arch — сообщение Р5.
8. Уборка ВМ и `owrt-*`; итог — раздел «Приёмка» в плане.

## Task 15: [контроллер] arm64
- Спросить владельца о `pve-edk2-firmware-aarch64` на PVE-ноде (Р10). Да — ВМ `arch: aarch64` (TCG) из
  `openwrt-25.12.6-custom-armsr-armv8-generic-ext4-combined-efi.qcow2`, сценарий T14 п. 1–6 с набором `openwrt-arm64`.
  Нет / не грузится — перенос приёмки на RPi 4/5 владельца (образы `bcm2711`/`bcm2712` openwrt-builder v0.3.1), задача
  закрывается после неё. Выпуск arm64-набора на prod — после ответа на В7.

## Task 16: Финальная интеграция + `make ci`
- `make ci` в cream-extensions, stvor-agent, bisquite, Stvor на итоговых `main`; Stvor `main` с пинами выпусков;
  `docs/STATUS.md` Stvor (сессия Stvor), `docs/guides/openwrt.md` cream (раздел «OpenWrt-робот Stvor» — ссылкой),
  статус спеки «реализована»; все `owrt-*` на стенде и ВМ на PVE убраны.

## Приёмка плана
Задачи 1–16 закрыты; OpenWrt-ВМ amd64 на PVE регистрируется через портал одной командой, видна «в сети», SSH через
Teleport SSO, метки stvor-agent, перезагрузка и `sysupgrade`-сценарий проходят; arm64 — по T15.
