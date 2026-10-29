# cream — регламент для агентов и людей

**cream** — исполнитель расширений: один статический Go-бинарь `cream` (linux/amd64, linux/arm64), ставится `.deb` (на OpenWrt — из `tar.gz`, ADR-0005). Читает `extension.yaml`, ведёт жизненный цикл install / upgrade / configure / remove на живой машине и фазу build внутри `virt-customize`, хранит состояние установленного, даёт `cream set` / `cream show` (бывший `bisquite-conf`) и работает с источниками каталогов. Лицензия Apache-2.0.

Ядро сделано по [спеке](docs/specs/2026-10-18-cream-core.md) и [плану](docs/plans/2026-10-18-cream-core.md). Контракт — [ADR 0001](docs/adr/0001-contract-cream-v1.md), реализация CLI и сборки — [ADR 0002](docs/adr/0002-single-dependency-stdlib-cli.md), справочник CLI — [docs/reference/cli.md](docs/reference/cli.md), пользовательская документация — [docs/README.md](docs/README.md). Версия и состояние — `CHANGELOG.md` и [docs/STATUS.md](docs/STATUS.md).

## Место в экосистеме
- **bisquite** — собирает образы ВМ; в слоях сборки вызывает `cream` (фаза build).
- **cream-extensions** — каталог расширений, которые исполняет `cream`.
- **stvor-agent** — агент портала Stvor на машине; ставится расширением.
- **Stvor** — приватный портал (identity & access hub); с ним говорит stvor-agent, не cream.

Контракты между ними (формат `extension.yaml`, CLI, пути на машине) меняются только через спеку/ADR и с планом совместимости.

## Язык
Общение, документация, комментарии в планах — **по-русски**. Код, идентификаторы, имена файлов и веток — **по-английски**. Коммиты и заголовки PR — Conventional Commits: тип и скоуп латиницей, заголовок и тело **по-русски** (`feat(cli): подкоманда show печатает настройки`).

## Карта репозитория
Каталоги и пакеты с назначением — [docs/development.md § Карта репозитория](docs/development.md#карта-репозитория); код ищи через `codebase-memory` (`search_graph`, `get_code_snippet`).

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
