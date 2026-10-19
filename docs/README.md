# Документация cream

Где что лежит и когда писать документ — [.claude/rules/docs.md](../.claude/rules/docs.md).
Новый документ без строки в этой таблице не считается готовым.

| Документ | Зачем читать |
|---|---|
| [STATUS.md](STATUS.md) | состояние работ: что сделано, что дальше, что сломано |
| [adr/README.md](adr/README.md) | индекс архитектурных решений; шаблон — [adr/0000-template.md](adr/0000-template.md) |
| [cli.md](cli.md) | справочник CLI cream 1.0: команды, флаги, коды выхода, формы `--json`, контракт для stvor-agent, фаза build для bisquite |
| [adr/0001-contract-cream-v1.md](adr/0001-contract-cream-v1.md) | ADR: что входит в контракт cream/v1 и как он меняется |
| [adr/0002-single-dependency-stdlib-cli.md](adr/0002-single-dependency-stdlib-cli.md) | ADR: одна зависимость yaml, CLI на stdlib `flag`, nfpm без goreleaser |
| [specs/2026-10-18-cream-core.md](specs/2026-10-18-cream-core.md) | спека cream/v1: формат расширения, жизненный цикл, действия, настройки, источники, CLI, фаза build, MVP |
| [plans/2026-10-18-cream-core.md](plans/2026-10-18-cream-core.md) | план MVP cream: 23 задачи, 9 волн, Progress Tracker |
| [research/2026-10-18-bisquite-extension-system-inventory.md](research/2026-10-18-bisquite-extension-system-inventory.md) | опись системы расширений bisquite на 2026-10-18 — вход спеки |
