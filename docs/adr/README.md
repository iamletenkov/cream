# Architecture Decision Records

Формат — MADR, шаблон — [0000-template.md](0000-template.md). Когда писать ADR — [.claude/rules/docs.md](../../.claude/rules/docs.md).
Новый ADR: следующий номер, `NNNN-slug.md`, строка в таблице ниже (и в `docs/README.md` не нужно — индекс ADR здесь).

| № | Решение | Статус | Дата |
|---|---|---|---|
| [0001](0001-contract-cream-v1.md) | контракт cream/v1: формат расширения, пути на машине, CLI, коды выхода, `--json` | Accepted | 2026-10-18 |
| [0002](0002-single-dependency-stdlib-cli.md) | одна внешняя зависимость (yaml), CLI на stdlib `flag`, `.deb` через nfpm без goreleaser | Accepted | 2026-10-18 |
