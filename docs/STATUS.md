# Статус cream

**Фаза:** старт — каркас (2026-10-18).

## Сделано
- Каркас Go-модуля: `cream version`, `make build/build-all/test/lint/ci` в контейнере `golang:1.25.11`.
- CI (lint, test, build-all) и release-please (`release-type: go`, первая версия — 0.1.0).
- Обвязка Claude Code: AGENTS.md, CLAUDE.md, `.claude/` (агенты, skills, правила, хуки), lefthook.

## Дальше
1. Спека cream (`docs/specs/`): формат `extension.yaml`, жизненный цикл и фаза build,
   состояние установленного, `cream set/show`, источники каталогов, CLI-библиотека.
2. Упаковка `.deb` (amd64/arm64) и выкладка к релизу — решит спека; в CI пока нет.
3. ADR на трудно откатываемое из спеки (формат, пути на машине, зависимости).

## Сломано
- Ничего известного.
