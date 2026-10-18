# Опись системы расширений bisquite: исходные данные для спеки cream

Срез на 2026-10-18. Источники: `bisquite` (main, только чтение), `cream-extensions` (main `0d153c0`, release 3.3.0),
`bisquite-teleport-agent` (только для сверки). Пути bisquite даны от `src/bisquite/`, пути каталога — от корня `cream-extensions/`.

## 1. Формат `extension.yaml`

Модель — `domains/build/models/extension_manifest.py:120-250` (`ExtensionManifest`, pydantic, `frozen=True, extra="forbid"`).
Разбор файла — `infrastructure/extensions/catalog.py:117` (`load_extension_manifest`).
Вторая, более строгая проверка — `tools/validate-extensions.py` в каталоге (`check_fields`:171, `check_capabilities`:268,
`check_conflicts`:287, `check_cycles`:311, `check_knobs`:402, `check_script`:471).

| Поле | Тип и умолчание в bisquite | Валидация bisquite | Валидация каталога | Что с ним делает bisquite |
|---|---|---|---|---|
| `name` | str, обязательно | `[A-Za-z0-9_][A-Za-z0-9._-]{0,63}` (:59, совпадает с сегментом ссылки `extension_ref.py:54`) | равно имени каталога | резолвер ищет по нему (а не по пути) |
| `version` | str, умолчание `"0.0.0"` | никакой | semver `X.Y.Z` | **никуда не пишется** (см. ниже) |
| `layout` | int или None | только `2` (`SUPPORTED_EXTENSION_LAYOUT`, :65, :179-194) | строго целое `2` | `None` → отказ резолвера (`repository.py`, после выбора победителя) |
| `family` | str, обязательно | токен `[a-z0-9][a-z0-9._-]{0,63}`, перечня нет намеренно | `{deb, rpm, apk, openwrt}` (`KNOWN_FAMILIES`:105) | фильтр кандидатов по `<package_format>` из `virt-inspector` |
| `arch` | tuple, обязательно непустой | канонизация `aarch64→arm64` (`arch_tokens.canonical_arch`), поддерживаются только `amd64`, `arm64` | непустой | сверка с `LABEL arch` и архитектурой хоста |
| `phase` | tuple[`build`/`firstboot`], непустой | enum, дубли схлопываются | из набора; предупреждение, если `firstboot` без `configure.sh` и наоборот | `resolve_phases` (:271) |
| `provides` | tuple, `()` | токены | — | **только печать** (`bs extension ls/check`) |
| `requires` | tuple, `()` | токены | каждая способность кем-то предоставлена; нет циклов | только печать |
| `conflicts` | tuple, `()` | токены | **имена расширений** (не способности!), симметрия, существование | только печать |

Скаляр вместо списка принимается для `arch/phase/provides/requires/conflicts` (`_accept_scalar_sequences`, :153).
Набор полей закрыт с обеих сторон: новое поле (`description`, `requires_base`, `build_only`…) роняет старый bisquite и валидатор.

Что реально используется в каталоге (40 манифестов в `extensions/debian/*`; `extensions/openwrt/*` — не расширения, нет `install.sh`):
- все 40: `family: deb`, `layout: 2`, лишних полей нет;
- `arch`: 25 × `[amd64, arm64]`, 13 × только `arm64` (Jetson/L4T, can-usb, pixelflux-*, jetson-stats, cuda-toolkit, tensorrt),
  3 × только `amd64` (`nvidia`, `nvidia-container-toolkit`, `nvidia-jetpack`);
- `phase`: 21 × только `build`, 19 × `[build, firstboot]`; только-`firstboot` нет ни одного (и bisquite такое отвергает);
- `requires` у 12, `conflicts` у 7 (`gnome↔xfce4↔lxde`, `kiosk↔chromium-kiosk`, `pixelflux-tegra↔pixelflux-v4l2m2m`).

Расхождения между документами и кодом:
- `docs/extensions.md` (таблица «Читателей два») и README каталога утверждают, что `version` «кладётся в родословную образа». Это
  неверно: `provenance.applied_extensions` (`domains/build/provenance.py:126`) пишет только `qualified_name` из VMFILE, без версии,
  без коммита источника, без параметров.
- `conflicts` в модели bisquite названо «способности», а в каталоге и валидаторе — имена расширений. Грамматика Debian (`Conflicts`
  по пакетам/виртуальным пакетам) допускает оба толкования; для cream надо выбрать одно.
- Docstring `extension_manifest.py:11` показывает пример без `layout`, т. е. сам по себе отвергаемый.

**layout 1 vs 2** (спека `docs/specs/2026-09-14-guest-layout-bisquite.md`):
- 1 (поле отсутствует): каталог в `/opt/vmsetup/<имя>`, общий код вендорился копиями в каждый каталог (`get_cloud_user.sh` — 12 копий,
  `bisquite-desktop` — 4), настройки частично в `/opt/vmsetup/<имя>/config.yaml`.
- 2: код `/opt/bisquite/<имя>/`, общий код `/opt/bisquite/lib/<sha256>/` + ссылка `<имя>/lib`, настройки `/etc/bisquite/<домен>/`,
  состояние `/var/lib/bisquite/<имя>/`, CLI `/usr/local/sbin/bisquite-*`.
- Совместимость: новый bisquite отказывает layout≠2 в резолвере; старый bisquite отказывает на неизвестном поле. Обратной совместимости нет
  осознанно; для старых firstboot-команд с `/opt/vmsetup` есть только предупреждение (`domains/build/guest_layout.py:23-50`).

## 2. Резолвер, источники, синхронизация

**Ссылка в VMFILE** — `domains/build/models/extension_ref.py:116` (`parse_extension_ref`): `EXTENSION [источник/]имя [KEY=VALUE…]`,
разбор через `shlex.split`; версии в ссылке (`@1.2.0` из спеки) **нет**; имя ключа — POSIX-переменная; повтор ключа — отказ;
`EXTENSION FOO=bar` — отказ. Парсер VMFILE: `infrastructure/parsers/vmfile_parser.py:81,166-169,215-234` (ref кладётся в `layer.metadata`
и `vmfile.extension_refs`).

**Конфиг источников** `extensions.yaml` — модель `domains/build/models/extensions.py:98-390`, хранилище
`infrastructure/config/extensions.py`. Поиск файла (`locate`, :135): `BISQUITE_CONFIG_DIR` (единственный, если задан) →
`/etc/bisquite/` (системная установка) или `$XDG_CONFIG_HOME/bisquite/` → `/etc/bisquite/` → устаревший `<DATA_DIR>/data/` (с предупреждением,
недоверен при групповом корне) → умолчание пакета `bisquite/data/extensions.yaml` (один источник `core`, git, `ref: main`,
URL ещё `bisquite-extensions.git`). Свой файл **заменяет** умолчание целиком.
- Типы: `git` (url; ref, resolved), `path` (path — только абсолютный, `~` раскрывается), `oci` (url; ref, resolved — **синхронизация отказывает**,
  `sync.py:285-307`). `builtin` нет намеренно (решение 2026-09-03). Поля сверяются с типом в обе стороны (`_TYPE_FIELDS`).
- Имя источника — тот же алфавит, что сегмент (становится каталогом кеша).
- `is_pinned()` — только `resolved` у git/oci; `path` никогда не закреплён.

**Кеш**: `<DATA_DIR>/extensions/<источник>/` (`config/paths.py:975`). **sources.lock** — `<DATA_DIR>/data/sources.lock`
(`config/sources_lock.py`, модель `SourceLockEntry`: name, type, origin, resolved (коммит / отпечаток дерева для path), synced_at).
Lock — наблюдение, `resolved` в конфиге — объявление; версии формата у lock нет (регенерируемый).

**Синхронизация** — только явная команда `bs extension sync [имя]` (`infrastructure/extensions/sync.py:212`). Свойства:
- кеш меняется атомарно одним `rename` из частного каталога в `<DATA_DIR>/tmp`;
- git: полный клон (без `--depth`, чтобы `resolved` был достижим); обновление `fetch --prune --tags` + `checkout --detach --force`;
  порядок разрешения ref: `refs/remotes/origin/<ref>` → `refs/tags/<ref>` → как написано; `resolved` перевешивает `ref`;
  без ref — `origin/HEAD` (:694-740); чужой/групповой кеш не трогается git'ом, а переклонируется (защита от хуков);
  окружение git подменяется (`_git_env`, без интерактива); URL с кредами редактируется в выводе (`redact_credentials`);
- path: копия (не ссылка), отказ на ссылках наружу дерева, отпечаток `fingerprint_tree` (:137) по содержимому;
- статусы `synced` / `up-to-date` (по совпадению коммита и origin с lock).

**Офлайн-гарантия**: ни `bs image validate`, ни сборка в сеть за расширением не ходят — резолвер (`repository.py:52`) читает только кеш.
Валидация (`domains/build/services/image_build_service.py:385-432`) выдаёт **предупреждения** (несинхронизирован, не найден,
отказ фазы), гейт — `--strict`. Сборка — громкий `BuildError`.

**Поиск** (`ExtensionRepositoryAdapter.find`, `repository.py:52-120`): источники в порядке объявления (или только названный);
`rglob("extension.yaml")` по всему кешу источника, отбор по `manifest.name`; фильтр по `family`, если известна; два одноимённых
в одном источнике — отказ; первый источник побеждает, затенение логируется `extensions.shadowed`; затем проверка `layout`.
`library` = `<кеш>/lib`, если каталог есть. Различаются сообщения «не синхронизировано» и «нет в источниках».

**provides/requires/conflicts**: в bisquite **не исполняются** — ни топосортировки, ни отказа по конфликту, ни проверки, что `requires`
закрыт предыдущими слоями. Только `validate-extensions.py` проверяет статический граф каталога.

**Барьер архитектуры и фазы** — `resolve_phases` (`extension_manifest.py:271-341`):
1. `target_arch ∉ manifest.arch` → отказ;
2. нет `build` → фазы как объявлены (но билдер затем откажет, см. §3);
3. `host_arch is None` или `host == target` → выполняем;
4. иначе отказ «соберите на машине target», с пояснением, почему нельзя подменить `build` на `firstboot`.
Источник `target_arch`: `LABEL arch` VMFILE, иначе архитектура хоста. Семейство: однократный `virt-inspector` на сборку
(`linux.py:2216-2258`, regex `<package_format>`); неудача инспекции — семейство не участвует в отборе.
OpenWrt: `EXTENSION` заблокирован целиком в `OpenWrtBuilder.BLOCKED_LAYER_TYPES` (`infrastructure/build/builders/openwrt.py:27-50`).

## 3. Как bisquite применяет расширение к образу

Диспетчер слоя: `linux.py:950` → `_apply_extension_layer` (`linux.py:2260-2324`):
1. нет резолвера или контекста — `BuildError`;
2. `parse_extension_ref(layer.content)`; `family = _detect_target_family(image)`; `find(ref, family=family)`;
3. `resolve_phases(...)`; отказ → `BuildError`; **если `build` не в фазах — `BuildError` «firstboot-only не реализован»**;
4. `_install_extension` (:2326): частный staging `<каталог образа>/.bisquite-ext-<uuid>` (0700), удаляется в `finally`.

`_apply_staged_extension` (`linux.py:2382-2486`):
- дерево расширения копируется в staging от дескриптора без следования ссылкам (`_stage_tree`, :2359; кеш открывается через `open_data_dir`);
- нет `install.sh` → `BuildError`; в каталоге расширения есть свой `lib` при наличии общего — `BuildError`;
- `lib/` источника копируется в staging, считается `_library_digest` (:2489: sha256 по отсортированным путям; `F путь\0`+содержимое,
  `L путь -> цель`, `D путь`), каталог переименовывается в `.libs/<sha256>`;
- **один** вызов `virt-customize -v --smp N --memsize M --format qcow2 -a <img>` (`_virt_customize_cmd`, :277-307; `-v` ради потокового вывода)
  со следующими операциями по порядку:
  1. `--mkdir /opt/bisquite`
  2. (если есть lib) `--mkdir /opt/bisquite/lib` `--copy-in <staging>/.libs/<sha256>:/opt/bisquite/lib` и `--chmod 0755:/opt/bisquite/lib/<sha256>/<файл>`
     для **каждого** обычного файла `lib/` (рекурсивно);
  3. `--copy-in <staging>/<имя каталога>:/opt/bisquite`
  4. `--chmod 0755:/opt/bisquite/<name>/<x>.sh` — только `*.sh` верхнего уровня каталога расширения;
  5. (если есть lib) `--link /opt/bisquite/lib/<sha256>:/opt/bisquite/<name>/lib`
  6. `--run-command 'if [ -w /dev/console ]; then { K=V … /opt/bisquite/<name>/install.sh ; } > /dev/console 2>&1; else …; fi'`
     (`_stream_to_console`, :318-358). Параметры — `KEY=shlex.quote(VAL)` префиксом к команде; порядок — порядок в VMFILE.
- код ≠ 0 → `RuntimeError` с **полной командой** в тексте (`linux.py:2478-2484`).

Мелочи, существенные для cream:
- **Скрытый дефект**: копируется каталог под именем **каталога** (`resolved.directory.name`), а `target_dir`, `--chmod`, `--link` и запуск
  используют `manifest.name`. В этом каталоге совпадение гарантирует валидатор; у стороннего источника с `name ≠ имя каталога` сборка упадёт.
- Параметры (в т. ч. пароли: `X11VNC_PASSWORD`, `CODE_SERVER_PASSWORD`) оказываются в argv `virt-customize` и в тексте `RuntimeError`;
  строка слоя с параметрами входит в VMFILE, который едет в образ.
- Дайджест слоя `EXTENSION` = sha256 строки VMFILE (`linux.py:729-734, 903-907`) — не зависит от содержимого расширения/коммита источника.
  Кеша слоёв нет, так что это влияет только на `chain_digest`/`final_layer_digest` в метках.
- В цепочке `FROM` одинаковый `lib` ложится одной копией, разные — рядом; старые каталоги `lib/<sha>` не чистятся никогда.
- `configure.sh` сборка не вызывает никогда.

**Метки lineage** (`domains/build/provenance.py:69-72`; запись `linux.py:3163-3178`; наследование `_inherit_lineage` :3102):
- `bisquite.lineage.extensions` — через запятую, сначала унаследованные от базы, потом свои, дедуп по первому вхождению; элемент — как в VMFILE
  (`имя` или `источник/имя`); вычисленное значение перекрывает одноимённый `LABEL`; читается только положительно;
- `bisquite.lineage.root` — `FROM`, на котором запись кончается (точка обрыва).
- Едут в конфиг OCI и аннотации манифеста (`infrastructure/oci/serialization.py:84`), в заметку Proxmox
  (`domains/deploy/services/deployment_service.py:317-336`).
- В метке нет: версии расширения, коммита/отпечатка источника, sha256 `lib`, параметров.

## 4. Раскладка в госте и firstboot-механика

| Путь | Что | Кто пишет |
|---|---|---|
| `/opt/bisquite/<name>/` | копия каталога расширения целиком, включая `extension.yaml`, остаётся в образе как рабочий каталог служб | сборка |
| `/opt/bisquite/<name>/lib` → `/opt/bisquite/lib/<sha256>/` | общий код источника (`bisquite-conf`, `bisquite-desktop`, `get_cloud_user.sh`, `l4t`, `knobs/desktop{,.apply}`) | сборка |
| `/opt/bisquite/knobs/<домен>{,.apply,.secret}` | ссылки на схему и хуки; «последний установивший побеждает» | `conf_init` в install.sh |
| `/etc/bisquite/<домен>/config` | настройки `KEY=VALUE` (0600, если есть секрет) | `conf_init`/`conf_set` |
| `/etc/bisquite/teleport/apps.d/<имя>.conf` | объявления приложений для агента Teleport (`NAME`, `URI`, `ICON`) | `teleport-app.sh` у code-server, selkies |
| `/var/lib/bisquite/<имя>/` | состояние (`firewall/backend`, `rustdesk/managed`) | install.sh/configure.sh |
| `/usr/local/sbin/bisquite-{conf,desktop,firewall,sensing-camera-ctl,teleport}` | CLI (у conf/desktop — ссылка в `lib/<sha256>`) | install.sh / пакет агента |
| `/etc/systemd/system/*.service` | юниты расширений | install.sh (`install -m 0644`) |

Firstboot: **механики в bisquite нет**. `install.sh` сам кладёт `configure-<имя>.service` и включает его (`systemctl enable` — 14 расширений,
либо ссылкой в `*.wants` — selkies, pixelflux-*, sensing, l4t-boot-verify, network-manager, т. к. systemd в chroot не работает).
Юниты — `Type=oneshot`, `RemainAfterExit=yes`, `ExecStart=/bin/bash /opt/bisquite/<имя>/configure.sh` (exec-бит не нужен).
Фактически это **«на каждой загрузке»**, а не «один раз»: ни один `configure.sh` себя не выключает, маркеров «сделано» нет —
configure-скрипты пишутся идемпотентными. Точки привязки разные: `WantedBy=multi-user.target` (docker, chromium-kiosk, rustdesk),
`cloud-init.target` + `After=cloud-final` (code-server, jetson-stats, wallpaper), `graphical.target` (kiosk, x11vnc, selkies, vino-vnc,
pixelflux-*), `sysinit.target` (bisquite-firewall). Рабочий стол — `bisquite-desktop.service` (юнит внутри `lib/bisquite-desktop install`),
на каждой загрузке до менеджера входа. Кроме того, VMFILE-слои `FIRSTBOOT`/`FIRSTBOOT_COMMAND` (guestfs-firstboot) и `firstboot-commands`
манифеста записи (cloud-final) — пользовательские точки, где обычно зовут `bisquite-conf set`/`bisquite-desktop set`.
**Path-юнитов в каталоге нет.** Они есть только в пакете `bisquite-teleport-agent` (`src/systemd/bisquite-teleport-apps.path` —
`PathChanged=/etc/bisquite/teleport/apps.d`; `bisquite-teleport-token.path` — `PathExists=/var/lib/teleport/host_uuid`).

## 5. bisquite-conf и knobs

Файл: `lib/bisquite-conf` (956 строк bash) — одновременно библиотека (`source "$SCRIPT_DIR/lib/bisquite-conf"`) и CLI
(через ссылку `/usr/local/sbin/bisquite-conf`; `_CONF_LIB_PATH` не разрешается `readlink`, чтобы попадать в тот же `lib/<sha256>`).
Идентичная копия вендорена в `bisquite-teleport-agent/src/lib/bisquite-conf` (diff пуст); его `pkg-setup` регистрирует домен `teleport`
и намеренно не перехватывает CLI-ссылку образа. Спека — `bisquite/docs/specs/2026-09-15-extension-knobs-library.md`.

**Схема `knobs`** (в каталоге расширения либо `lib/knobs/<домен>`): строки `КЛЮЧ ТИП УМОЛЧАНИЕ ОПИСАНИЕ`, `—` = пусто, поле в кавычках
может содержать пробелы, `PREFIX_*` — шаблон. Типы (`_conf_base_check` :110-170): `bool`, `int`, `port`, `host`, `path`, `str`
(без управляющих, `$`, обратных кавычек), `enum:a,b`, `re:ERE`, `list:<тип>`, `secret`/`secret:<тип>` (маскируется, файл 0600),
`secret:hook` (в файл не пишется, значение уходит по stdin хуку `<домен>.secret KEY`, хук печатает производные пары), `ro:<тип>`
(`set` отказывает, пишет только владелец/хук; `--allow-ro` у API), суффикс `?` — пустое значение допустимо. Пустое значение принимается
только при пустом умолчании или `?`.

**Команды CLI** (`conf_main` :937): `list` (домены и наличие `+apply`/`+secret`), `show <домен>` (ключ/значение/откуда/тип, секреты
маскированы, `[НЕВЕРНО]` для битых значений в файле), `get <домен> KEY`, `set [--apply|--no-apply] <домен> KEY=VALUE…`.
`KEY=-` — значение со stdin (пустой stdin — отказ). Нужен root (или `BISQUITE_CONF_ROOT`).
Применение (`conf_cli_set` :838-884): после записи — хук `/opt/bisquite/knobs/<домен>.apply <изменённые ключи>`; без `--apply` хук
пропускается, если `systemctl is-system-running` = `starting|initializing` или systemd не запущен (сборка). Хук обязан быть неблокирующим
(`systemctl --no-block`) — это проверяет валидатор. Хук отказал → запись уже есть, код ≠ 0.

**API для скриптов**: `conf_init <домен> <абс. путь схемы> [--env] [--migrate-yaml YAML=KEY,…] [KEY=VAL…]` (регистрация ссылок,
создание файла, дописывание недостающих ключей с непустым умолчанием, `--env` — экспортированные ключи схемы из окружения
install.sh, т. е. параметры VMFILE, `ro`-ключи из окружения пропускаются), `conf_load`, `conf_get`, `conf_set`, `conf_validate`,
`conf_check`, `conf_keys`, `conf_cli_set`. Разбор файла — порт `parse_env_file_internal()` systemd (без `source`), запись атомарная
с сохранением комментариев и порядка. Всё учитывает `BISQUITE_CONF_ROOT` (тесты без root).

**Домены** (13 + teleport): `desktop` (lib/knobs, ставят gnome/xfce4/lxde/cloud-user-desktop через `bisquite-desktop install`),
`x11vnc` (+secret), `selkies`, `vino` (+secret, владелец vino-vnc), `sensing-camera` (sensing-gmsl2-camera), `code-server`, `kiosk`,
`chromium-kiosk`, `can`, `firewall`, `locale`, `rustdesk`, `teleport` (пакет агента). `knobs` есть у 12 расширений каталога.

**Обёртки над bisquite-conf**: `bisquite-desktop set` (= `bisquite-conf set --apply desktop`; ещё `apply [--boot|--now]`, `defaults`,
`status`, `install <каталог>`), `bisquite-teleport set` (пакет агента, `conf_cli_set --apply teleport`; плюс `join`, `render`,
`labels-enable`). Прочие CLI каталога — не обёртки conf: `bisquite-firewall`, `bisquite-sensing-camera-ctl`.

## 6. Расширения каталога (40)

Ф: B = build, F = build+firstboot. Арх: A = amd64+arm64, a64 = arm64, x64 = amd64. K = есть `knobs`.
«Живая» — применимо ли `install.sh` на работающей машине. Общие оговорки для всех: install.sh **включает**, но не **запускает** службы
(configure/юниты поднимутся при следующей загрузке, нужен `daemon-reload` там, где включено ссылкой в `.wants`); многие ходят в сеть
(apt, curl) — на живой машине это норма, при сборке — сеть хоста.

| Имя | Вер. | Арх | Ф | provides / requires / conflicts | K | Живая | Причина |
|---|---|---|---|---|---|---|---|
| btop | 1.2.0 | A | B | system-monitor | | да | apt/бинарь |
| can | 1.1.0 | A | B | can-bus | K | да | ядра из `/lib/modules/*`, не `uname -r`; хук перезагружает modules-load |
| can-usb | 1.0.0 | a64 | B | can-usb-adapters / gmsl2-camera | | с оговоркой | собирает модули под ядро Sensing из `/boot/Image`; нужно ядро Sensing и `modprobe`/перезагрузка |
| chromium-kiosk | 3.0.0 | A | F | kiosk-browser / — / kiosk | K | с оговоркой | сторонний репозиторий; экран захватывается после старта configure-юнита |
| cloud-growroot | 1.1.0 | A | B | cloud-growroot | | нет по смыслу | правит модули cloud-init первой загрузки; на живой машине корень уже не вырастет |
| cloud-user-desktop | 4.1.0 | A | F | cloud-user-desktop | | с оговоркой | `bisquite-desktop install`; применение — следующая загрузка или `apply` |
| code-server | 3.0.1 | A | F | web-ide | K | да | configure-юнит `WantedBy=cloud-init.target` — на живой машине его надо запустить явно |
| cuda-toolkit | 1.1.0 | a64 | B | cuda | | да | apt из репозитория Jetson |
| docker | 3.0.0 | A | F | container-runtime | | с оговоркой | **удаляет** `docker.io`/`podman-docker`/`containerd`/`runc`, если стоят (на живой машине — разрушительно); правка `cmdline.txt` RPi — в configure, нужна перезагрузка |
| firefox | 1.1.1 | A | B | web-browser | | да | |
| firewall | 1.2.0 | A | F | host-firewall | K | с оговоркой | `policy drop` на входе: на живой машине по SSH легко отрезать себя; бэкенд выбирается по модулям ядра |
| gnome | 4.1.0 | A | F | x11-server, display-manager, desktop-session / — / xfce4, lxde | | с оговоркой | огромный набор пакетов, `set-default graphical.target`, смена DM — перезагрузка |
| jetson-stats | 2.0.0 | a64 | F | jetson-monitor | | да | pip + юнит |
| kiosk | 3.0.0 | A | F | kiosk-browser / x11-server, display-manager / chromium-kiosk | K | с оговоркой | захват экрана |
| l4t-board-fdt | 1.1.0 | a64 | B | l4t-board-selected / l4t-custom-kernel | | с оговоркой | правит `extlinux.conf`; перезагрузка; бэкап перезаписывается при каждом запуске |
| l4t-boot-verify | 1.1.0 | a64 | B | l4t-boot-verify | | да | маскирует вендорскую службу, включает ссылкой |
| l4t-gstreamer | 1.2.0 | a64 | B | gstreamer, hw-video-codec | | да | |
| l4t-kernel-kvm | 1.4.0 | a64 | B | kvm-host, l4t-custom-kernel | | нежелательно | ~3 ч сборки ядра и 11 ГБ, меняет `DEFAULT` загрузчика и `root=PARTUUID`; задумано для базового образа |
| l4t-opencv | 1.2.0 | a64 | B | opencv | | да | |
| l4t-pytorch | 1.2.0 | a64 | B | pytorch / cuda | | да | долгая сборка torchvision; на живой машине GPU есть (при сборке его нет) |
| l4t-tensorflow | 1.1.0 | a64 | B | tensorflow / cuda | | да | |
| locale | 1.0.0 | A | B | system-locale | K | да | |
| lxde | 4.1.0 | A | F | как gnome / — / gnome, xfce4 | | с оговоркой | как gnome |
| network-manager | 1.1.0 | A | B | network-manager | | с оговоркой | смена сетевого стека на живой машине может отрезать доступ |
| nocloud-cidata | 1.1.0 | A | B | nocloud-cidata-seed | | нет по смыслу | влияет только на источник данных cloud-init первой загрузки; по сути «только файл» (вопрос §9) |
| nvidia | 2.0.0 | x64 | B | gpu-driver-nvidia | | с оговоркой | модули под **все** ядра из `/lib/modules`; blacklist nouveau — перезагрузка; ветка очистки initrd appliance на живой машине не срабатывает |
| nvidia-container-toolkit | 1.0.0 | x64 | B | nvidia-container-runtime / container-runtime, gpu-driver-nvidia | | да | правит `daemon.json`, нужен перезапуск docker |
| nvidia-jetpack | 2.0.0 | x64 | B | jetson-flash-station | | с оговоркой | станция прошивки; binfmt через `/etc/binfmt.d` — нужен перезапуск `systemd-binfmt`; nfs-server только enable |
| pixelflux-tegra | 2.0.0 | a64 | F | — / web-remote-desktop / pixelflux-v4l2m2m | | с оговоркой | проверка кодера — только на загрузке (`verify-*.service`) |
| pixelflux-v4l2m2m | 1.0.0 | a64 | F | hardware-video-encoding / web-remote-desktop / pixelflux-tegra | | с оговоркой | то же |
| rustdesk | 1.1.0 | A | F | remote-support | K | да | `.deb` по sha256 |
| selkies | 4.0.3 | A | F | web-remote-desktop / x11-server, display-manager | K | с оговоркой | включение ссылкой, нужен daemon-reload/старт |
| sensing-gmsl2-camera | 4.1.0 | a64 | B | gmsl2-camera | K | нежелательно | подменяет `/boot/Image`, DTB-оверлеи, `extlinux.conf`, `apt-mark hold` ядра, `dpkg-divert` |
| teleport-agent | 3.1.0 | A | B | teleport-node | | да | **уже есть `update.sh`** — прецедент обновления на живой машине (только пакет агента) |
| tensorrt | 1.2.0 | a64 | B | tensorrt | | да | |
| vino-vnc | 4.0.0 | A | F | remote-desktop / x11-server, display-manager | K | с оговоркой | живёт в сессии пользователя |
| wallpaper | 2.0.1 | A | F | wallpaper / desktop-session | | да | |
| x11vnc | 4.0.0 | A | F | remote-desktop / x11-server, display-manager | K | с оговоркой | хук секрета вызывает `x11vnc -storepasswd` (пароль в argv на время вызова) |
| xfce4 | 4.1.0 | A | F | как gnome / — / gnome, lxde | | с оговоркой | как gnome |
| yq | 1.1.0 | A | B | yq | | да | статический бинарь |

Привязка к среде сборки (по комментариям и коду): `uname -r` в appliance ≠ ядро образа — `nvidia`, `can`, `can-usb`, `sensing`, `firewall`
берут ядра из `/lib/modules`, `/boot` (на живой машине это тоже верно); в virt-customize `/boot/firmware` пуст (RPi) — поэтому `docker`
перенёс правку `cmdline.txt` в configure; нет GPU/устройств — l4t-*, pixelflux-* проверяют только на загрузке; systemd не работает —
включение ссылками и `systemctl enable` без `start`; память appliance `--memsize` (по умолчанию 2048) — `nvidia`, `l4t-pytorch`,
`l4t-kernel-kvm` её учитывают. Идемпотентность заявлена конвенцией (`docs/extensions.md`, «Как добавить расширение», п. 1),
проверка «уже» есть у большинства; явно неидемпотентного поведения не найдено, кроме перезаписи бэкапов (`l4t-board-fdt`,
`docker/configure.sh` `cmdline.txt.before-docker` защищён проверкой). Сплошной сверки манифест ↔ скрипт не делалось
(открытый вопрос `bisquite/docs/specs/2026-09-11-extensions-defects.md`).
Ни у одного расширения нет `remove`/`uninstall`; `upgrade` — только `teleport-agent/update.sh`. Состояния «что установлено» нет нигде,
кроме самого каталога `/opt/bisquite/<имя>/extension.yaml` и метки lineage образа.

## 7. CI и тесты

**cream-extensions**:
- `tools/check.sh` — точка входа, доводит все проверки до конца: `validate-extensions.py` (Python 3 + PyYAML; манифесты, граф, knobs через
  саму библиотеку, запреты `source`/записи конфига мимо библиотеки, блокирующий `systemctl`), `test-conf.sh` (bash на подменённом корне,
  сверка разбора с `systemd-run --user` или эталоном systemd 257), `test-teleport-agent.sh`, `test-nvidia-jetpack.sh`,
  `test-sensing-gmsl2-camera.sh`, `test-l4t-kernel-kvm.sh`, `test-firewall.sh`, `test-can-usb.sh` — все на хосте, с подставными
  `curl/dpkg/apt-get/make/modinfo/iptables`, без root, без ВМ, без контейнеров. Живые проверки (systemd-контейнер для firewall, плата
  для l4t/sensing/can-usb) — вручную по README расширений. Сборку образов с расширениями каталог не гоняет.
- `.github/workflows/ci-release.yml`: на PR и push в main — `check` (`ubuntu-latest`, Python 3.12, явный `pip install pyyaml`);
  на push в main — `release-please` (`release-type: simple`, один тег `vX.Y.Z` на весь каталог, `package-name: bisquite-extensions`,
  разделы CHANGELOG по-русски, манифест `.github/release_please/.release-please-manifest.json`); при созданном релизе —
  `dispatch-bisquite`: `gh workflow run extensions-bump.yml -R iamletenkov/bisquite -f tag=<тег>` токеном секрета `BISQUITE_DISPATCH_TOKEN`
  (fine-grained PAT, до 16.10.2027); `HOME_REPO: iamletenkov/bisquite-extensions` — в домашнем репозитории отсутствие токена — ошибка,
  в форке — предупреждение.
- `.github/workflows/agent-bump.yml` (`workflow_dispatch`, вход — тег `bisquite-teleport-agent`): берёт `SHA256SUMS` релиза агента,
  `tools/agent-bump.sh` поднимает пин и версию расширения `teleport-agent`, прогоняет `check.sh`, пушит ветку `chore/agent-<тег>`,
  открывает PR и вручную запускает `ci-release.yml` на ветке (PR от `GITHUB_TOKEN` проверки не триггерит).
- Версия каждого расширения в `extension.yaml` поднимается руками и ни с чем не связана, кроме CHANGELOG-конвенции.

**bisquite**: unit-тесты `tests/test_extension_instruction.py` (172), `test_extension_resolver.py` (559), `test_extensions_config.py` (512),
`test_extensions_config_lookup.py` (267), `test_extensions_packaged_defaults.py` (93), `test_extensions_sync.py` (932),
`test_cli_extension_commands.py` (678). `.github/workflows/extensions-bump.yml` поднимает подмодуль `bisquite-extensions` (он всё ещё в
репозитории — нужен для `UPLOAD` файлов OpenWrt) и `EXT_VERSION`/`ref` в примерах; `job-hygiene.yml` проверяет подмодуль.
В `examples/` 32 VMFILE используют `EXTENSION` (лидеры: code-server 14, x11vnc 11, selkies 10, docker 10, yq 8, gnome 8);
сборки примеров с расширениями — ручные (VM/PVE/платы), не в CI.

## 8. Имя «bisquite» внутри каталога и lib (для замены на cream)

Всего 1268 строк во всём репозитории `cream-extensions`; в `extensions/` и `lib/` — 525 строк в коде + 341 в README.
Файлов с упоминанием: `extensions/debian` 159, `extensions/openwrt` 11 (в т. ч. фикстуры `bisquite-device-write*`), `lib` 4, `tools` 10, `.github` 3.

| Группа | Шаблон | Строк / файлов | Замечание |
|---|---|---|---|
| Пути гостя (контракт layout 2) | `/opt/bisquite` | 124 / 66 | юниты `ExecStart`, скрипты, документация; смена = новый `layout` и мажоры всех расширений с юнитами |
| | `/etc/bisquite` | 166 / 77 | настройки доменов, `teleport/apps.d` (общий контракт с пакетом агента) |
| | `/var/lib/bisquite` | 12 / 7 | firewall, rustdesk |
| CLI и библиотека | `bisquite-conf` | 239 / 75 | имя файла `lib/bisquite-conf`, CLI, `source …/lib/bisquite-conf` во всех install.sh с knobs, копия в пакете агента |
| | `bisquite-desktop` | 133 / 26 | `lib/bisquite-desktop`, юнит `bisquite-desktop.service`, `/etc/default/bisquite-desktop` (legacy) |
| | `bisquite-teleport` | 60 / 13 | внешний пакет `bisquite-teleport-agent`; переименование — вне каталога |
| | `bisquite-firewall`, `bisquite-sensing*`, `bisquite-l4t-boot-verify` | 38/10, 34/11, 5/2 | имена юнитов и CLI |
| Переменные | `BISQUITE_CONF_ROOT` 27, `BISQUITE_SELKIES_ALLOW_NO_AUTH` 16 (+`BISQUITE_SELKIES_*` 4), `BISQUITE_TELEPORT_ROOT` 6, `BISQUITE_FIREWALL_{BACKEND,RUN,MODULES}` 9, `BISQUITE_RUSTDESK_FOLLOWUP` 2, `BISQUITE_DISPATCH_TOKEN` 4 (CI) | 65 / 24 | `BISQUITE_SELKIES_ALLOW_NO_AUTH` — ключ схемы, уже пишется в манифесты записи (`firstboot-commands`) |
| Сетевые объекты | `inet bisquite` (nft) 23/7, `BISQUITE-INPUT` (iptables) 34/11 | | имена таблиц/цепочек на живых машинах — смена требует миграции правил |
| Файлы в госте | `zz-bisquite-nocloud.cfg` 6/2, `10-bisquite-iptables-raw.conf` 2/1, `99-bisquite-sensing-camera.rules` 1/1, `/etc/default/bisquite-x11vnc` 6/3 (legacy) | | |
| Команды хост-инструмента | `` `bs ` `` | 40 / 25 | в README/комментариях: `bs extension sync`, `bs image build --smp` и т. п. |
| Репозиторий | `bisquite-extensions` | 5 / 4 (+ release-please `package-name`, CI `HOME_REPO`) | URL в `bisquite/src/bisquite/data/extensions.yaml` тоже старый |

## 9. Открытые вопросы и известные проблемы

Из спек bisquite:
1. **Ось `phase` смешивает «когда» и «нужен ли гость»** (`2026-09-02-extensions-system.md`, конец). Расширение «только файлы»
   (`nocloud-cidata`, по сути и `cloud-growroot`) объявляет `build` и поэтому попадает под барьер архитектуры, хотя `--copy-in` работает
   при любой паре; перевод `raspios-normalized.vmfile` на `EXTENSION` сломал бы сборку arm64 на amd64. Нужна третья возможность
   («файлы»/«гость не нужен») — для cream это ещё и вопрос «что делать на живой машине».
2. **firstboot-only не реализован** — билдер отказывает; объявление в манифесте шире реализации.
3. **Граф зависимостей не исполняется**: ни топосортировки, ни отказа по конфликту, ни сверки `requires` с уже установленным
   (в т. ч. унаследованным через `FROM`); порядок держится вниманием автора VMFILE. Способности, поставленные голым `INSTALL`,
   в граф не попадают в принципе.
4. **Требования к базе не выражаются** (cloud-init, yq для `get_cloud_user.sh`, cloud-guest-utils для growroot).
5. `oci`-источник не реализован (не выбран `mediaType`/раскладка артефакта).
6. Нет версии в ссылке (`@1.2.0` из спеки не сделан) и нет решателя версий — намеренно.
7. `ref: main` в поставляемом умолчании — сборка не воспроизводима, пока оператор не закрепит `resolved`.
8. Нужна ли родословной форма SBOM (SPDX/CycloneDX) отдельным OCI-артефактом.
9. Цель `l4t` (Ubuntu с BSP) не выражается осью — только ветвлением по `/etc/nv_tegra_release` в госте.
10. `policy-rc.d` в virt-customize — открытый вопрос, не воспроизведено.
11. Не пора ли сверять `extension.yaml` со скриптами (дефект 8.1 из `2026-09-11-extensions-defects.md`).

Из `docs/extensions.md` каталога («Что осталось незакрытым»): `arch` у `chromium-kiosk` для arm64 не проверен; разбор `bisquite-conf`
сверен только с systemd 257 (на Ubuntu 22.04 — 249, расхождение с комментарием на `\`); пароль x11vnc проходит через argv
`x11vnc -storepasswd`.

Найдено при описи (стоит учесть в спеке cream):
- имя каталога vs `manifest.name` при копировании (§3) — латентный дефект для сторонних источников;
- параметры `EXTENSION` (включая секреты) попадают в argv `virt-customize`, текст ошибки и VMFILE образа; канала «секрет по stdin»
  для сборки нет (у `bisquite-conf` он есть: `KEY=-`);
- lineage не несёт версии/коммита/отпечатка `lib`, хотя документация каталога утверждает обратное про `version`;
- `--chmod` ставится только `*.sh` верхнего уровня; прочие исполняемые файлы полагаются на сохранение режима `--copy-in`
  или на `install -m` внутри install.sh;
- каталоги `/opt/bisquite/lib/<sha256>` копятся в цепочке `FROM` и не чистятся; «последний установивший побеждает» у ссылок
  `/opt/bisquite/knobs/*` и `/usr/local/sbin/bisquite-{conf,desktop}` — неявное правило владения, которое cream при install/remove
  на живой машине должен сделать явным (кто владелец домена, что делать при remove последнего владельца);
- состояния «установлено» нет: cream придётся ввести реестр (что, какая версия, из какого источника/коммита, с какими параметрами
  без секретов, какой `lib`), и он же может стать источником lineage;
- `conflicts` — по именам, `provides/requires` — по способностям; смешение стоит развести в формате cream;
- копия `bisquite-conf` в пакете `bisquite-teleport-agent` — третий потребитель; переименование CLI/путей затронет и его;
- подмодуль `bisquite-extensions` в bisquite и `UPLOAD` файлов OpenWrt из чекаута — отдельный канал доставки, не покрытый резолвером
  (`extensions/openwrt/` — не расширения).
