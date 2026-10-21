# Документация cream

Где что лежит и когда писать документ — [.claude/rules/docs.md](../.claude/rules/docs.md).
Новый документ без строки в этой таблице не считается готовым.

| Документ | Зачем читать |
|---|---|
| [STATUS.md](STATUS.md) | состояние работ: что сделано, что дальше, что сломано |
| [adr/README.md](adr/README.md) | индекс архитектурных решений; шаблон — [adr/0000-template.md](adr/0000-template.md) |
| [cli.md](cli.md) | справочник CLI cream 1.1 (по 1.1.3): команды, флаги, коды выхода, формы `--json`, контракт для stvor-agent, фаза build для bisquite |
| [adr/0001-contract-cream-v1.md](adr/0001-contract-cream-v1.md) | ADR: что входит в контракт cream/v1 и как он меняется |
| [adr/0002-single-dependency-stdlib-cli.md](adr/0002-single-dependency-stdlib-cli.md) | ADR: одна зависимость yaml, CLI на stdlib `flag`, nfpm без goreleaser |
| [specs/2026-10-18-cream-core.md](specs/2026-10-18-cream-core.md) | спека cream/v1: формат расширения, жизненный цикл, действия, настройки, источники, CLI, фаза build, MVP |
| [runbooks/01_release.md](runbooks/01_release.md) | выпуск версии: release-please, ассеты, `SHA256SUMS`, воспроизводимая сборка, что тянет выпуск (подъём пина у потребителей) |
| [runbooks/02_diagnostics.md](runbooks/02_diagnostics.md) | диагностика на машине: `cream list`/`show`, журналы `cream-configure@`, блокировки, коды выхода |
| [runbooks/03_upgrade-cream.md](runbooks/03_upgrade-cream.md) | обновление cream на роботе: `.deb`, сверка `SHA256SUMS`, проверка и откат |
| [specs/2026-10-21-cream-apply-bisquite-split.md](specs/2026-10-21-cream-apply-bisquite-split.md) | черновик на утверждение: `cream apply`/`cream.lock`, бутстрап и секреты в госте, bisquite 3.0 без cream, семейство OpenWrt без Teleport |
| [plans/2026-10-18-cream-core.md](plans/2026-10-18-cream-core.md) | план MVP cream: 23 задачи, 9 волн, Progress Tracker |
| [research/2026-10-18-bisquite-extension-system-inventory.md](research/2026-10-18-bisquite-extension-system-inventory.md) | опись системы расширений bisquite на 2026-10-18 — вход спеки |
