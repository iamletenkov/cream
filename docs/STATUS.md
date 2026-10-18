# Статус cream

**Фаза:** старт — каркас (2026-10-18).

## Сделано
- Каркас Go-модуля: `cream version`, `make build/build-all/test/lint/ci` в контейнере `golang:1.25.11`.
- CI (lint, test, build-all) и release-please (`release-type: go`, первая версия — 0.1.0).
- Обвязка Claude Code: AGENTS.md, CLAUDE.md, `.claude/` (агенты, skills, правила, хуки), lefthook.

## Дальше
1. План MVP по [спеке](specs/2026-10-18-cream-core.md) §17 (спека прошла skeptic 2026-10-18) → реализация.
2. Упаковка `.deb` (amd64/arm64, nfpm) — в плане MVP.
3. ADR на трудно откатываемое из спеки (формат, пути на машине, зависимости).

## Сломано
- Ничего известного.
