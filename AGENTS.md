# cream — регламент для агентов и людей

**cream** — исполнитель расширений: один статический Go-бинарь `cream` (linux/amd64, linux/arm64), ставится `.deb`. Читает `extension.yaml`, ведёт жизненный цикл install / upgrade / configure / remove на живой машине и фазу build внутри `virt-customize`, хранит состояние установленного, даёт `cream set` / `cream show` (бывший `bisquite-conf`) и работает с источниками каталогов. Лицензия Apache-2.0.

**Выпущена 1.1.4.** MVP 1.0 и 1.1 сделаны по [спеке](docs/specs/2026-10-18-cream-core.md) и [плану](docs/plans/2026-10-18-cream-core.md): формат `cream/v1`, источники, резолвер, install / upgrade / remove / set / run, фаза build, `.deb` и тарболы. Контракт — [ADR 0001](docs/adr/0001-contract-cream-v1.md), реализация CLI и сборки — [ADR 0002](docs/adr/0002-single-dependency-stdlib-cli.md), справочник CLI — [docs/reference/cli.md](docs/reference/cli.md), пользовательская документация — [docs/README.md](docs/README.md). Состояние — [docs/STATUS.md](docs/STATUS.md).

## Место в экосистеме
- **bisquite** — собирает образы ВМ; в слоях сборки вызывает `cream` (фаза build).
- **cream-extensions** — каталог расширений, которые исполняет `cream`.
- **stvor-agent** — агент портала Stvor на машине; ставится расширением.
- **Stvor** — приватный портал (identity & access hub); с ним говорит stvor-agent, не cream.

Контракты между ними (формат `extension.yaml`, CLI, пути на машине) меняются только через спеку/ADR и с планом совместимости.

## Язык
Общение, документация, комментарии в планах — **по-русски**. Код, идентификаторы, имена файлов и веток — **по-английски**. Коммиты и заголовки PR — Conventional Commits: тип и скоуп латиницей, заголовок и тело **по-русски** (`feat(cli): подкоманда show печатает настройки`).

## Карта репозитория
```
cmd/cream/            — точка входа: run(args, stdout, stderr) int → internal/cli
internal/cli/         — диспетчер команд: flag с перестановкой, глобальные флаги, конверт --json, коды выхода; golden в testdata/golden
internal/engine/      — исполнитель: реестр + домен настроек + блокировки + хуки + systemd = install/upgrade/remove/set/run/build
internal/resolve/     — резолвер: платформа и применимость, граф depends/requires/conflicts, порядок, lineage (§9.4, §12.4)
internal/catalog/     — валидация каталога (§4.1) и поиск расширения по ссылке в кеше источников (§9.3)
internal/source/      — sources.yaml, единственный сетевой доступ (sync), кеш и sources.lock, catalog pack
internal/manifest/    — строгий разбор extension.yaml cream/v1 и cream-catalog.yaml
internal/config/      — домен настроек расширения: /etc/cream/<ext>/{config,secrets}, присваивания, params-file
internal/state/       — реестр установленного (cream-state/v1) и журнал history.jsonl
internal/lock/        — flock-блокировки lock/{global,<ext>} и наследование через CREAM_LOCK_FD
internal/runner/      — запуск хуков и действий: окружение, группа процессов, таймауты, фильтр секретов
internal/systemd/     — юнит cream-configure@.service, drop-in стадии, systemctl
internal/place/       — раскладка files расширения и их снятие при remove (владение по реестру)
internal/paths/       — раскладка путей на машине (§5), --root и режим пользователя (XDG)
internal/envfile/     — грамматика EnvironmentFile= systemd: чтение и правка с сохранением комментариев
internal/fsx/         — файловые примитивы: атомарная запись, копия дерева, дайджест дерева, проверка прав
internal/keytype/     — типы значений ключей и параметров действий (§8.2)
internal/semver/      — SemVer 2.0 и диапазоны (своя реализация, без зависимости)
internal/cerr/        — типизированные ошибки, коды выхода и предупреждения (§11.3)
internal/version/     — версия (ставится -ldflags -X при сборке)
testdata/catalog/     — фикстурный каталог §14; testdata/catalog-next/ — следующие версии noop и upgrade-fixture для upgrade
testdata/invalid/     — заведомо неверные расширения для cream validate
testdata/pilot/       — пилотные расширения (cloud-user, fixture-{agent,docker,teleport}-like) для интеграции и build
test/integration/     — make test-integration: .deb в контейнере с systemd PID 1, сценарии scenario/NN-*.sh
test/build/           — make test-build: фаза build настоящим virt-customize на образе Debian 12
packaging/            — nfpm.yaml, maintainer-скрипты, copyright (полные тексты сторонних лицензий)
scripts/              — dist-release.sh (тарболы, SHA256SUMS), check-deb.sh (состав пакета), repro-check.sh
LICENSE, NOTICE       — Apache-2.0 и уведомления о стороннем коде
docs/                 — README.md (индекс), getting-started.md, guides/ (по задачам), reference/ (CLI, манифест, коды, --json, пути), architecture.md, development.md, runbooks/ (выпуск), adr/, specs/, plans/, research/, STATUS.md
.claude/              — агенты, skills, правила, хуки Claude Code
.github/              — CI (ci, vuln, repro-check, интеграция amd64/arm64), release-please, ассеты релиза
Makefile              — единственный способ что-либо запускать (`make help`)
```

## Как работать
1. **Классифицируй задачу** и скажи это вслух:
   - *тривиальная* (опечатка, очевидный фикс) → правка → `make lint` → коммит;
   - *bounded* (поток в 1–3 файлах) → короткий дизайн в чате → TDD → reviewer → коммит;
   - *архитектурная* (новая подсистема, формат, CLI, интеграция) → спека → skeptic → план с `## Progress Tracker` → TDD по задачам → reviewer → PR.
   Сомневаешься — бери путь тяжелее.
2. **Спека до плана, план до кода.** Трудно откатываемое (формат, CLI, пути, зависимости) — ADR. Новый документ — строка в `docs/README.md`.
3. **TDD:** тест пишется до кода и сначала падает.
4. **Docker-first:** Go на хосте нет; всё — `make …` в контейнере `golang:1.26.10` (кэш — `.cache/`).
5. Хост общий и нагруженный: один `make ci` за раз, без параллельных тяжёлых сборок.

## Definition of Done
Задача закрыта, когда подтверждено **выводом команд**, а не словами: `make lint` и `make test` зелёные, перед PR — `make ci`; документация и `docs/STATUS.md` обновлены; чекбокс плана отмечен в том же коммите.

## Git и выпуск
- Ветки `feature/`, `fix/`, `refactor/`, `chore/`, `docs/` от свежего `origin/main`; в `main` — через PR со squash, заголовок PR = заголовок squash-коммита.
- Версию и `CHANGELOG.md` ведёт **release-please** (`.github/release_please/`): `feat` → minor, `fix` → patch, `!` → major. Теги руками не ставим.
- `git add <пути>`, никогда `git add -A`. Хуки lefthook (`lefthook install`) не обходить: `--no-verify` запрещён.
- В конце коммита от агента — строка `Co-Authored-By:` модели.

## Гигиена и секреты
- Временное (логи прогонов, скриншоты, отчёты) — только в `.scratchpad/` (gitignored); сборка — `dist/`, кэш — `.cache/`.
- Секреты — только в окружении и `.env` (gitignored). `.mcp.json` генерируется из `.mcp.json.example`, ключи берутся из env. Значения секретов не попадают в код, доки, коммиты и логи; gitleaks стоит на pre-commit.
- Новая зависимость — решение спеки, не удобство одной функции.
