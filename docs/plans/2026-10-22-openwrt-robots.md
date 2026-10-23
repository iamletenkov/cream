# План: OpenWrt-роботы в Stvor (Teleport + stvor-agent + регистрация через портал)

> 2026-10-22. Спека — [docs/specs/2026-10-20-openwrt.md](../specs/2026-10-20-openwrt.md) (принята, возобновлена
> 2026-10-22; В1–В7, Р1–Р13). PoC — [research/2026-10-22-openwrt-teleport-glibc-poc.md](../research/2026-10-22-openwrt-teleport-glibc-poc.md).
> Классификация — **архитектурная**: четыре репозитория (cream-extensions, stvor-agent, Stvor, bisquite), новая
> платформа роботов. cream и openwrt-builder **не меняются** (спека §4.1, §4.6). После skeptic (2026-10-22) план
> упрощён рулингами Р11–Р13: OpenWrt-расширения называются `teleport-openwrt`/`stvor-agent-openwrt` и лежат в общем
> снимке каталога (второго корня, манифеста и снимка нет); задача «второй снимок» влилась в Task 2, задач — 15. 2026-10-23 владелец пересмотрел Р4 спеки (робот переживает
> `sysupgrade` сам, тем же узлом) — добавлена Task 13a, шаг 6 Task 13 переписан.
> Цикл на задачу: TDD → `make lint` + тесты по скоупу → reviewer → коммит в ветке задачи → слияние контроллером в
> ветку дорожки → `make ci` дорожки после волны.
>
> **Исполнителю:** после выполнения отметь свою задачу `- [x]` в Progress Tracker **в том же коммите**. План живёт в
> репозитории cream (ветка `docs/openwrt-spec` до слияния, потом `main`); исполнитель другого репозитория отметку не
> делает — её коммитит контроллер сразу после слияния задачи.

## Progress Tracker
<!-- Пункты разделены пустой строкой: параллельные отметки сливаются без конфликта. -->

- [x] Task 1: [контроллер] `tsh ssh` на PoC-узле OpenWrt (срез 0, день 1; провал — стоп, решение по варианту B)

- [x] Task 2: [cream-extensions] Ядро `lib/teleport-core.sh` (одна копия в git), снимок кладёт его в оба расширения, `debian/teleport` 4.2.0 без изменения поведения

- [x] Task 3: [stvor-agent] `initsys` (systemd/procd), пути и имя расширения по семейству, свой `machine-id`, телеметрия без PSI — поверх телеметрии

- [x] Task 4: [stvor-agent] Ассет `stvor-agent_<v>_linux_<arch>.tar.gz`, e2e на корневой ФС OpenWrt, `SECURITY.md`, выпуск

- [x] Task 5: [cream-extensions] Расширение `openwrt/teleport-openwrt`

- [x] Task 6: [cream-extensions] Расширение `openwrt/stvor-agent-openwrt`

- [x] Task 7: [cream-extensions] Регресс `debian/teleport` (живой прогон) и выпуск каталога

- [x] Task 8: [Stvor] Набор Teleport `openwrt-<arch>` на стенде: `build-teleport-openwrt.sh`, пины glibc и patchelf, `TELEPORT_OPENWRT_SHA256_<ARCH>`, FAIL в preflight/smoke

- [x] Task 9: [Stvor] Набор бутстрапа по семейству, ветка `openwrt` скрипта установки, пины в `settings.py`/`x-runtime-env`, `robot-pin-bump.sh` по группам

- [x] Task 10: [Stvor] `POST` на install-эндпоинте (функция + роутер Traefik), `command_openwrt`, строка OpenWrt на `/robots/new`

- [x] Task 11: [Stvor] `robots-gate`/`stand-seed` на OpenWrt-ВМ, runbook 10 и подъёма Teleport, ADR Stvor, подъём пинов

- [x] Task 12: [bisquite] Пример образа OpenWrt-робота и профиль деплоя 1 ГБ / 4 ГБ

- [ ] Task 13: [контроллер] Приёмка amd64: OpenWrt-ВМ на PVE — одна команда, «в сети», SSH через SSO, метки, reboot, `sysupgrade`

- [ ] Task 13a: [cream-extensions + Stvor] Восстановление после `sysupgrade` тем же узлом: `keep.d` расширений, служба `stvor-restore` (Р4, 2026-10-23)

- [x] Task 14: [контроллер] arm64: решение по AAVMF на PVE (Р10), приёмка на ВМ `armsr` или перенос на RPi владельца
  — 2026-10-23 приёмка прошла на ВМ `armsr` на PVE (`owrt-arm64-t14`, VMID 990005, Teleport из тарбола форка): одна команда за 88 с под TCG, SSO SSH, метки, телеметрия, reboot тем же узлом (сценарий T13 п. 1–5); фазу 17 гейта на arm64 не гоняли (её уборка выключает Teleport; гейт проверен на amd64); `sysupgrade` на arm64 — в приёмке T13a

- [ ] Task 15: Финальная интеграция + `make ci` во всех затронутых репозиториях

## Рулинги исполнения
- **Т9/Т10 — набор Teleport:** скачивается плоско (как остальные файлы бутстрапа) и передаётся расширению через
  `TELEPORT_TARBALL=`; отдельный путь `<mirror>/binaries/teleport/<v>/…-openwrt-<arch>-bin.tar.gz` не нужен.
- **Команда OpenWrt:** `uclient-fetch -qO- --post-data='' --ca-certificate=/etc/ssl/certs/ca-certificates.crt '<url>' | sh`;
  без ca-bundle — понятный отказ до загрузки. Все загрузки на OpenWrt — только `uclient-fetch` с проверкой CA
  (busybox-`wget` сертификат не проверяет).
- **Тесты скрипта:** busybox в образ backend не добавляем — цель `make test-openwrt-shell` в разовом контейнере и
  задача CI `openwrt-shell`.
- **Код установки** выдаётся, если набор есть хотя бы у одного семейства (Debian или OpenWrt); Р5 сохраняется.
- **Порог ОЗУ** — 900 000 КиБ (`MemTotal`) = «номинально 1 ГБ».
- **Т3:** `machine-id` агента — `0640 root:stvor-telemetry`.
- **В7 закрыт владельцем 2026-10-22:** свой форк Teleport, только внутри организации, исходники доступны сотрудникам и
  уполномоченным лицам → AGPL выполнен; пересмотр — при внешних пользователях без доступа к исходникам. Пин arm64
  можно выкладывать.
- **Р10:** на PVE установлен `pve-edk2-firmware-aarch64` — arm64-ВМ `armsr` возможна.

## Правила плана
1. **Не более 3 исполнителей одновременно** (VM 12 CPU / 29 ГБ, история OOM): дорожка **E** (cream-extensions),
   дорожка **A** (stvor-agent), дорожка **S** (Stvor). Задача 12 (bisquite) — первому освободившемуся из E/A. Каждый —
   в своём worktree, `git add <пути>`, `git commit -F … -- <пути>`, без `git add -A`.
2. **Дорожка S — наш исполнитель** (не исполнитель сессии Stvor): worktree Stvor, ветка **`feature/openwrt-robots`**
   от `main`. Зона: `install_script.py`, `downloads.py`/`Bootstrap`, `install_code.py`, `settings.py` (только новые
   поля), `scripts/{fetch-teleport-tarball,build-teleport-openwrt,check-teleport-pin,robot-pin-bump,stand-seed,robots-gate}.sh`,
   `teleport/openwrt-glibc.env`, страница `/robots/new` (строка команды), две узкие правки `compose.yaml` (Р12).
   Условия сессии Stvor (ответ на Р12, 2026-10-22, вариант «а»):
   - **роутер `robot-install`** (`compose.yaml` ≈ :441): меняется **только** его `rule` (`` … && (Method(`GET`) || Method(`POST`)) ``)
     и добавляется **отдельный** middleware `buffering` с `maxRequestBodyBytes` ≈ 1 КБ в список `middlewares` роутера.
     Роутер `robot-api`, приоритеты и соседние роутеры сессии Stvor (`…-robot-telemetry-token` —
     `POST /robot-api/v1/telemetry-token`, `…-ingest` — `/robot-api/v1/{metrics,inventory,events}`) **не трогать**;
     тест: наш `POST` по пути `robot-install` не пересекается с остальными `/robot-api/v1/*`;
   - **`x-runtime-env`** (`compose.yaml` :29–67): каждая новая переменная — **своей строкой в конце блока**;
   - **`settings.py`**: новые поля — **в конце тела `RuntimeSettings`**, строку объявления класса не трогать (у сессии
     Stvor он наследует `VictoriaMetricsSettings`);
   - `robot_api.py`: новый `POST`-маршрут — **отдельной функцией в конце файла** (там же сессия Stvor добавила
     `POST /robot-api/v1/telemetry-token`);
   - `.env.example`: новые пины — **отдельными строками**; пин `STVOR_AGENT_*` до 4.1.0 поднимает сессия Stvor;
   - `robot-pin-bump.sh` и `check-teleport-pin.sh` — **наша** зона;
   - **кто готов первым — сливает в `main` первым, второй ребейзит**.
3. **stvor-agent (задачи 3–4)** — ветвиться от `main` **после** squash-слияния телеметрии; выпуск 4.1.0 делает сессия
   Stvor, когда портал принимает телеметрию (она сообщит). OpenWrt-поддержка выходит в 4.1.0, если успевает до него,
   иначе — следующим минором (4.2.0); номер решается с сессией Stvor в момент выпуска.
4. **Общий стенд Stvor.** Перед своей выкладкой (`make deploy-branch`) / `make up` / `robots-gate` сессия Stvor пишет
   нам, мы — ей. Получив сигнал, контроллер **приостанавливает все операции со стендом** (join, `tctl`, Keycloak,
   портал) и **убирает за собой** пользователей, токены и узлы `owrt-*` (`tctl users rm`, `tctl tokens rm`,
   `tctl rm node/<uuid>`, пользователь Keycloak `owrt-*`), затем подтверждает «стенд чист». Возобновление — по ответу
   сессии Stvor. Тяжёлые шаги (`make up`, сборка образов, `ci-full`) — только через `make heavy`, по одному.
5. **Пользователи стенда** — только префикс `owrt-*` (разрешение владельца на CRUD); живут не дольше проверки.
6. **PVE** (`root@192.168.202.206`): VMID 990001–990010, ВМ после себя удалять (`qm stop` + `qm destroy` своих VMID);
   пакеты на ноду не ставить без согласия владельца (Р10).
7. Отказ классификатора прав — стоп и доклад, без обходов.

## Порядок и зависимости
| Волна | E (cream-extensions) | A (stvor-agent) | S (Stvor) | Контроллер |
|---|---|---|---|---|
| 0 | T2 | T3 (после слияния телеметрии) | — | **T1** |
| 1 | T5 (после T1, T2) | T4 | T8 (после T1) | слияния, отметки |
| 2 | T6 (после T4) | T12 | T9 (локальные пины из сборок E/A) | |
| 3 | T7 (выпуск каталога) | — | T10 | |
| 4 | — | — | T11 (пины выпусков) | |
| 5 | T13a (`keep.d`) | — | T13a (`stvor-restore`) | T13 (п. 6 — после T13a), T14, T15 |

Выпуски: stvor-agent (T4) → `cream-extensions` (T7, пин агента в `openwrt/stvor-agent-openwrt/files/pin.env`) → пины
Stvor (T11). Провал T1 останавливает T5, T8 и всё после них; T2–T4 не зависят от варианта Teleport.

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

## Task 2: [cream-extensions] Ядро `lib/teleport-core.sh` (Р11)
- Файлы: `lib/teleport-core.sh`, `lib/tests/test-teleport-core.sh`, `tools/catalog-snapshot.sh` (кладёт ядро в
  `files/teleport-core.sh` каждого расширения из списка потребителей), `tools/stage-catalog.sh` (то же дерево без
  архива — для `cream-validate.sh`, `hooktest.sh`, `live-test.sh` и path-источника), `tools/check.sh`,
  `tools/check-version-bump.sh`, `tools/test-snapshot.sh`, `extensions/debian/teleport/{files/teleport.sh,extension.yaml,README.md}`.
- **Одна копия ядра в git** — `lib/teleport-core.sh`. Потребители — `debian/teleport` и (с T5)
  `openwrt/teleport-openwrt`: список в одном месте (`lib/teleport-core.consumers`). `check.sh` сверяет: в git под
  `extensions/` копий `teleport-core.sh` нет; каждый потребитель объявляет `files/teleport-core.sh` в `files`;
  в снимке и в staged-дереве копии байтово равны `lib/` (`test-snapshot.sh`). `check-version-bump.sh`: правка
  `lib/teleport-core.sh` требует подъёма версий **всех** потребителей.
- Вынести init-независимую часть (`render`, join, метки, `apps.d`, `bound_keypair`, managed-метки, `managed-status`)
  в ядро с точками слоя (§4.2 спеки: init, restart, журнал join, arch, data_dir, TMPDIR, имя тарбола, доп. ключи
  `teleport.yaml`, **имя расширения** `TELEPORT_EXT` для подсказок вида `cream run "$TELEPORT_EXT" join`) — функции и
  переменные, которые слой определяет до `source`.
- Страховка: ядро вызывает бинарь только как `env -u LD_PRELOAD "$TELEPORT_BIN" …` (`unset LD_PRELOAD` перед
  `$TELEPORT_BIN`); на Debian безвредно, на OpenWrt защищает от `libsetlbf.so` musl (спека §3 п. 1). Тест ядра.
- TDD: сначала тесты `check.sh` (копия в git — отказ; потребитель без `files`-строки — отказ) и тесты ядра на текущем
  поведении (перенос существующих H-тестов); тесты `debian/teleport` — **без правок ожиданий**, гоняются по
  staged-дереву.
- `debian/teleport` → `4.2.0` (комментарий версии: «вынос ядра, поведение без изменений»); `check-version-bump` зелёный.
**Приёмка:** `make ci` cream-extensions зелёный; диф рендера `teleport.yaml` и набора юнитов до/после на фикстурах —
пуст; два прогона `catalog-snapshot.sh` — одни байты; состав снимка отличается только файлом ядра в `debian/teleport`.

## Task 3: [stvor-agent] `initsys`, пути, имя расширения, `machine-id`, PSI
- Ветка от `main` после слияния телеметрии. `internal/systemd` → `internal/initsys` (`systemd`, `procd`), выбор по
  `/etc/os-release` под корнем (`ID`/`ID_LIKE`: `openwrt`, `lede`); procd — `/etc/init.d/<svc> enable|restart|stop|disable`.
- **Имя расширения по семейству** (Р11): `teleport` на Debian/Ubuntu, `teleport-openwrt` на OpenWrt — одно значение из
  того же определения семейства, три вызова `cream` в `internal/teleport/cream.go` (`managed-status` ≈ :97,
  `set-managed` ≈ :120, `actions … --json` ≈ :144). Имя самого себя (`stvor-agent` / `stvor-agent-openwrt`), если
  агент где-то его использует, — так же.
- Пути по семейству: состояние `/opt/stvor-agent/state.json`, телеметрия `/opt/stvor-agent/telemetry/`; токены и lock — как есть.
- `machine-id`: на `openwrt` — `/opt/stvor-agent/machine-id` (создаётся при первом старте, 0600).
- Сборщики телеметрии: нет `/proc/pressure/*` — поле не шлётся, без ошибки в журнале на каждом тике.
- TDD: table-driven тесты выбора init, путей и имени расширения по фикстурам os-release; фейк `cream` проверяет имя в
  argv; фейк procd; тест «без PSI».
**Приёмка:** `make ci` stvor-agent зелёный; на Debian поведение, пути и argv `cream` без изменений (существующие тесты
без правок).

## Task 4: [stvor-agent] tar.gz, e2e на OpenWrt, выпуск
- `make tarball` (бинарь, man, LICENSE, NOTICE; `SOURCE_DATE_EPOCH`, `gzip -n`), ассеты `stvor-agent_<v>_linux_{amd64,arm64}.tar.gz`
  в **том же** `SHA256SUMS`, что и `.deb`; `repro-check` для них; workflow выпуска.
- e2e: корневая ФС openwrt-builder v0.3.1 x86-64 (`docker import`, sha256 закреплён, как `make test-openwrt` cream) +
  `fakeportal` + фейк `cream`: `enable` пишет токен и зовёт procd-фейк/скрипт, цикл держит метки через
  `cream run teleport-openwrt …`, телеметрия уходит, состояние в `/opt`.
- `SECURITY.md`: на OpenWrt нет песочницы юнита; телеметрия — под `stvor-telemetry`.
- Выпуск — по правилу 3 плана.
**Приёмка:** e2e OpenWrt зелёный в CI; ассеты в релизе; `tools/agent-bump.sh` каталога читает их (контракт имён).

## Task 5: [cream-extensions] `openwrt/teleport-openwrt`
- `extensions/openwrt/teleport-openwrt/`: `extension.yaml` (`name: teleport-openwrt`, `family: openwrt`,
  `cream: ">=1.3.0"`, arch `[amd64, arm64]`, `os: [{id: openwrt, version: ">=25.12"}]`,
  `platform.commands: [bash, curl, tar, sha256sum]`, **те же `config.keys` и действия**, что у debian),
  `files/teleport.sh` (слой procd, `TELEPORT_EXT=teleport-openwrt`), `files/teleport.init` → `/etc/init.d/teleport`,
  `files/keep.d` → `/lib/upgrade/keep.d/teleport`, хуки `install/apply/upgrade/remove`, README. `files/teleport-core.sh`
  объявлен в `files`, в git его нет — кладёт снимок/staged-дерево (T2); строка в `lib/teleport-core.consumers`.
- Решения PoC (спека §3) — тестами: в init нет `stdout`/`stderr`; `TMPDIR=/opt/teleport/tmp` в `procd_set_param env`;
  `teleport.yaml` с `log.output: /opt/teleport/log/teleport.log` (файл 0700/0600, `logcap` 1 МиБ, не syslog), `pam.enabled: false`, `enhanced_recording.enabled: false`, `data_dir: /opt/teleport/data`;
  маркер `/opt/teleport/.cream-owned`; `remove` снимает всё, кроме того, что снимать запрещает контракт; `apply` перезапускает
  фоном (`validate.apply_blocking` молчит); `validate.openwrt_tmpfs` молчит; подсказки называют `teleport-openwrt`.
- Тесты: H-тесты слоя + сценарий на корневой ФС OpenWrt в docker (install из локального набора → render → `managed-status`).
**Приёмка:** `make ci` зелёный; ручной прогон на ВМ PVE: `cream install teleport-openwrt …` + `cream run teleport-openwrt join`
→ узел в `tctl nodes ls`, reboot — онлайн без нового секрета; уборка `owrt-*`.

## Task 6: [cream-extensions] `openwrt/stvor-agent-openwrt`
- `extensions/openwrt/stvor-agent-openwrt/` по образцу `debian/stvor-agent`: `depends: [teleport-openwrt]`;
  `files/pin.env` (tar.gz по архитектурам; `tools/agent-bump.sh` пишет и `.deb`-пины debian, и tar.gz-пины openwrt из
  одного `SHA256SUMS`), `files/agent.sh`, init-скрипты `/etc/init.d/stvor-agent{,-telemetry}` (procd, `respawn`),
  хук `install` создаёт `stvor-telemetry` через `user_add`/`group_add_next` (`/lib/functions.sh`), `remove` — всё
  обратно; ключ `STVOR_AGENT_TARBALL` вместо `STVOR_AGENT_DEB`; пин агента — версия с T3 (имя расширения по семейству).
- Тесты: H-тесты + сценарий на корневой ФС OpenWrt.
**Приёмка:** `make ci` зелёный; на ВМ после T5 агент включается `stvor-agent enable`, метка из портала доезжает.

## Task 7: [cream-extensions] Регресс и выпуск каталога
- `make test-live` для `debian/teleport` 4.2.0 (контейнер с systemd) — зелёный.
- Выпуск минора каталога: **один** снимок `cream-extensions-<v>.tar.gz` с `teleport-openwrt`/`stvor-agent-openwrt`;
  цепочка подъёма пинов (`ci-release` → PR в Stvor) поднимает `CREAM_EXTENSIONS_*` как обычно. Бандл bisquite получает
  оба расширения тем же снимком.
**Приёмка:** релиз с `cream-extensions-<v>.tar.gz` и `SHA256SUMS`; в снимке оба расширения и две равные копии ядра.

## Task 8: [Stvor] Набор Teleport `openwrt-<arch>` (Р7, Р13)
- `scripts/build-teleport-openwrt.sh` (debian:12 по digest; `.deb` `libc6`/`libgcc-s1` и **`patchelf`** Debian 12 —
  по пинам sha256 из `teleport/openwrt-glibc.env`, `dpkg-deb -x`, без `apt`; детерминированный tar.gz), вызов из
  `fetch-teleport-tarball.sh` без `ARCH` при заданном `TELEPORT_OPENWRT_SHA256_<ARCH>`; сбой сборки — WARN в
  `fetch-teleport`, но **заданный пин без собранного набора (или с чужой суммой) — FAIL** в env-preflight/`smoke`;
  `check-teleport-pin.sh` — формат новых пинов; `.env.example` — отдельные строки с комментарием.
- TDD: тест скрипта (bats/sh, как у соседних скриптов): воспроизводимость (две сборки — одни байты), состав набора
  (нет `tsh`), интерпретатор и `RUNPATH` у обоих бинарей, отказ на чужой пин glibc/patchelf; тест preflight: пин есть,
  набора нет — FAIL.
**Приёмка:** `make lint` Stvor зелёный; на стенде набор amd64 собран и совпал с пином; arm64 — собран на amd64 без
эмуляции; `smoke` падает, если удалить набор при заданном пине.

## Task 9: [Stvor] Набор бутстрапа, скрипт, пины (Р11–Р13)
- `downloads.py`/`Bootstrap`: измерение семейства; OpenWrt-робот для `<arch>` — при `CREAM_TARBALL_SHA256_<ARCH>`,
  общем снимке (`CREAM_EXTENSIONS_SHA256`, как у Debian), `TELEPORT_OPENWRT_SHA256_<ARCH>`, `STVOR_AGENT_TARBALL_SHA256_<ARCH>`;
  `fetch-teleport` кладёт эти файлы на зеркало.
- Поля `settings.py` для `CREAM_TARBALL_SHA256_{AMD64,ARM64}`, `STVOR_AGENT_TARBALL_SHA256_{AMD64,ARM64}`,
  `TELEPORT_OPENWRT_SHA256_{AMD64,ARM64}` — в конце тела `RuntimeSettings` (правило 2); строки `x-runtime-env` в
  `compose.yaml` — каждая своей строкой в конце блока; `.env.example` — отдельные строки; env-preflight: пин без пары —
  отказ, как сейчас.
- `robot-pin-bump.sh`: группа поднимается **целиком из одного `SHA256SUMS`** — cream: `.deb` + `cream_<v>_linux_<arch>.tar.gz`;
  stvor-agent: `.deb` + `stvor-agent_<v>_linux_<arch>.tar.gz`; нет любого ассета группы — отказ без правки файла;
  `--self-test` дополняется (полная группа, нет tar.gz — отказ). `check-teleport-pin.sh` сверяет согласованность новых
  строк с группой.
- `install_script.py`: ветка `openwrt` по §4.5 спеки — порядок проверок до изменений, `mktemp -d /opt/…`, `apk add`,
  cream tar.gz + `cream system init`, источник — общий снимок, `cream install teleport-openwrt`,
  `cream run teleport-openwrt join`, `cream install stvor-agent-openwrt`, маркер владения, отказ Р5 с перечнем
  недостающего.
- TDD: тесты рендера (pytest) + прогон скрипта под busybox `sh` на корневой ФС openwrt-builder в docker с фейками
  `cream`/`apk` (как существующие тесты скрипта): чистая машина, повтор (не отказ), Teleport без маркера (отказ),
  мало ОЗУ/места (отказ до изменений), нет набора для arch (отказ Р5), Debian-ветка без изменений; тест настроек
  (новые поля читаются из окружения контейнера).
**Приёмка:** `make test` по скоупу и `make lint` Stvor зелёные; `robot-pin-bump.sh --self-test` зелёный.

## Task 10: [Stvor] `POST`, Traefik, команда и портал (Р1, Р12)
- `robot_api.py`: `POST` install-эндпоинта — **отдельная функция в конце файла**, общая логика погашения с `GET`; аудит
  как у `GET` с заголовком; `HEAD`/`GET` без заголовка — по-прежнему без погашения.
- `compose.yaml`, роутер `robot-install`: `rule` → `` … && (Method(`GET`) || Method(`POST`)) ``; новый **отдельный**
  middleware `…-robot-install-body` (`buffering.maxRequestBodyBytes` ≈ 1024) добавлен в `middlewares` роутера;
  `robot-api`, приоритеты и роутеры сессии Stvor не трогаются (правило 2).
- `install_code.py`: `render_command_openwrt`, `InstallCodeOut.command_openwrt` (пусто, если на экземпляре нет
  OpenWrt-набора ни для одной архитектуры); OpenAPI и типы фронтенда.
- `/robots/new`: вторая строка «OpenWrt» с копированием, по дизайн-системе.
- TDD: backend — POST гасит код один раз, повторный POST — отказ, предпросмотр GET не гасит, аудит; фронтенд — строка
  видна/скрыта. **Через Traefik** (`smoke`/`robots-gate`, не только pytest): `POST` с пустым телом на
  `/robot-api/v1/install/<код>` доходит до backend; тело > лимита — 413; `POST` на `/robot-api/v1/{metrics,inventory,events,telemetry-token}`
  не попадает в роутер `robot-install` (пересечения нет).
**Приёмка:** `make test`, `make lint`, `make typecheck` по скоупу; проверка через Traefik на стенде (с паузой по правилу 4);
design-review строки (обе темы).

## Task 11: [Stvor] Гейт, документация, пины
- `robots-gate.sh`/`stand-seed.sh`: сценарий OpenWrt — ВМ PVE из примера bisquite (T12), команда с портала, ожидание
  «в сети», `set-managed`-метка, уборка ВМ и `owrt-*`. Гейт — опциональный по переменной (ВМ PVE не всегда есть).
- runbook 10 (набор бутстрапа OpenWrt, пины), runbook подъёма Teleport — шаг «пересобрать набор `openwrt-<arch>`,
  обновить `TELEPORT_OPENWRT_SHA256_*`», runbook 07 (CVE glibc → пины `openwrt-glibc.env`), ADR Stvor (следующий
  номер): «OpenWrt-роботы: семейство в наборе бутстрапа, `POST` install-эндпоинта» + строки индексов.
- Подъём пинов на выпуски T4 и T7 (`robot-pin-bump.sh`).
**Приёмка:** `make ci` Stvor зелёный; гейт OpenWrt зелёный на стенде (с паузой по правилу 4).

## Task 12: [bisquite] Пример OpenWrt-робота
- `examples/build/amd64/openwrt-robot/` (VMFILE: `FROM` openwrt-builder v0.3.1 x86-64; без Teleport внутри; README:
  регистрация командой портала после деплоя), `examples/deploy/openwrt-robot.yml` (2 vCPU, **1 ГБ ОЗУ**, диск 4 ГБ);
  arm64-вариант — `examples/build/arm64-vm/openwrt-robot/` по образцу соседнего примера.
**Приёмка:** `make ci` bisquite зелёный; `bs build` + деплой на PVE дают машину, проходящую проверки скрипта Stvor
(ОЗУ, место, `bash`/`curl`).

## Task 13: [контроллер] Приёмка amd64
На свежей ВМ PVE из примера T12 (VMID 990001–990010), стенд со всеми пинами (после T11):
1. Портал: «Добавить робота» → строка OpenWrt → вставить в консоль ВМ → робот **«в сети»** в Stvor.
2. **SSH через Teleport SSO**: пользователь Keycloak `owrt-*` с доступом к пространству робота → `tsh login` (SSO) →
   `tsh ssh root@<робот>`.
3. **stvor-agent**: метка, изменённая в портале, доезжает до узла ≤ интервала агента; телеметрия видна (если 4.1.0 в пинах).
4. **Перезагрузка** → «в сети» без действий.
5. Повтор команды с новым кодом — не отказ.
6. **`sysupgrade`** тем же образом с сохранением конфигурации (Р4, после T13a) → без действий на портале служба
   `stvor-restore` ставит cream и расширения, узел возвращается **тем же** (`host_uuid` и имя в `tctl nodes ls` прежние,
   второго узла нет), метки агента и телеметрия идут без новых токенов. Затем `sysupgrade -n` (идентичность потеряна) →
   «Новый секрет» → команда → узел с **новым** `host_uuid` онлайн, старый узел Stvor удалил.
7. Отказы: ВМ 512 МБ ОЗУ — отказ до изменений; экземпляр без набора arch — сообщение Р5.
8. Уборка ВМ и `owrt-*`; итог — раздел «Приёмка» в плане.

## Task 13a: [cream-extensions + Stvor] Восстановление после `sysupgrade` тем же узлом (Р4, 2026-10-23)
Спека §4.3, §4.7, Р4. cream не меняется (ADR 0005 п. 8). Debian не затронут.
- **cream-extensions, `teleport-openwrt`:** `files/keep.d` дополняется доменом `/etc/cream/teleport-openwrt/` и флагом
  `/opt/cream/var/ext/teleport-openwrt/agent-enabled`; хук `install` возвращает `0700` каталогу состояния (tar
  восстанавливает `0755`) — проверить на busybox `tar`. Повторный `install` при маркере и данных не трогает данные и не
  зовёт `join`.
- **cream-extensions, `stvor-agent-openwrt`:** свой `keep.d` — `/opt/stvor-agent/{machine-id,state.json,telemetry-token.src}`,
  `/etc/stvor-agent/token`, `/etc/stvor-agent-telemetry/token`, `/etc/cream/stvor-agent-openwrt/`; права после
  распаковки — существующий configure `every_boot` (без рекурсивного `chown`). README обоих расширений: архив
  `sysupgrade` — секрет уровня root, восстанавливать только на ту же машину.
- **Stvor, `install_script.py` (только OpenWrt-ветка):** после успешной установки — `/etc/stvor/restore.sh` (зеркало и
  пины sha256 cream tar.gz, снимка, набора Teleport, stvor-agent tar.gz; без кода и секрета), `/etc/init.d/stvor-restore`
  (procd, `START=99`, повтор раз в минуту до успеха), `/lib/upgrade/keep.d/stvor`. Логика службы — спека §4.7. Повтор
  команды портала на роботе с маркером, данными и токеном `robot-<id>-*` — без `TELEPORT_FORCE`. Исправить
  комментарий о `host_uuid` при `join` в `install_script.py:349` и docstring
  `backend/tests/teleport/test_install_script_openwrt.py:535`: в Teleport 18.10 `join` без живой идентичности даёт
  новый host ID.
- **Stvor, «Новый секрет»:** предупреждение на портале, если узел робота в сети; после привязки нового поколения
  (`bound_host_id`) — удаление `node/<старый bound_host_id>`; выяснить, где «дубль» — в Teleport (истекает ~10 мин)
  или в инвентаре Stvor. runbook 10 Stvor: раздел о `sysupgrade` и рисках архива.
- TDD: H-тесты `keep.d` обоих расширений; сценарий на корневой ФС OpenWrt: снять всё, кроме путей `keep.d` → служба
  восстанавливает без `join` (фейк `cream`/`apk` для Stvor-теста скрипта), повтор безопасен; тест рендера рецепта
  (нет секрета и кода в `restore.sh`).
**Приёмка:** `make ci` cream-extensions и Stvor зелёные; выпуск каталога и подъём пина в Stvor; сценарий Task 13 п. 6 (`sysupgrade` тем же узлом,
затем `sysupgrade -n`) на ВМ PVE **amd64 и arm64** (`armsr`).

## Task 14: [контроллер] arm64
- Спросить владельца о `pve-edk2-firmware-aarch64` на PVE-ноде (Р10). Да — ВМ `arch: aarch64` (TCG) из
  `openwrt-25.12.6-custom-armsr-armv8-generic-ext4-combined-efi.qcow2`, сценарий T13 п. 1–5 с набором `openwrt-arm64`
  (п. 6, `sysupgrade`, — в приёмке T13a).
  Нет / не грузится — перенос приёмки на RPi 4/5 владельца (образы `bcm2711`/`bcm2712` openwrt-builder v0.3.1), задача
  закрывается после неё. Выпуск arm64-набора на prod разрешён (В7 закрыт 2026-10-22).

## Task 15: Финальная интеграция + `make ci`
- `make ci` в cream-extensions, stvor-agent, bisquite, Stvor на итоговых `main`; Stvor `main` с пинами выпусков;
  `docs/STATUS.md` Stvor (по согласованию с сессией Stvor), `docs/guides/openwrt.md` cream (раздел «OpenWrt-робот
  Stvor» — ссылкой), статус спеки «реализована»; все `owrt-*` на стенде и ВМ на PVE убраны.

## Приёмка плана
Задачи 1–15 и 13a закрыты; OpenWrt-ВМ amd64 на PVE регистрируется через портал одной командой, видна «в сети», SSH через
Teleport SSO, метки stvor-agent, перезагрузка и `sysupgrade` (тот же узел без портала) проходят; arm64 — по T14.
