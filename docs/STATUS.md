# Статус cream

**1.1.0 (ветка `feature/cream-1.1`, задачи 1 и 4 плана перевода каталога `cream-extensions`):** стадии configure
`early`, `cloud-user`, `graphical` (§6.4, манифест требует `cream: ">=1.1.0"`; `graphical` — после `multi-user.target`
и менеджера входа), `cream resolve --installed FILE`, граф каталога в `cream validate --catalog`, `system init`
обновляет `configure.target` реестра; сборка не с тега (`git describe`) — dev. Проверено: `make ci`, `make vuln`,
`make test-integration` debian-12 и ubuntu-24.04 (с набором `desktop`: gdm3 + cloud-init, без `ordering cycle`).
Дальше — PR в `main` и выпуск 1.1.0 через release-please.

**Фаза:** 1.0.0 выпущен (2026-10-19) — все 23 задачи [плана](plans/2026-10-18-cream-core.md) закрыты.

## Сделано
- Формат `cream/v1`, источники archive/path и `catalog pack`, резолвер, исполнитель (`install`/`upgrade`/`remove`,
  `set`/`show`/`get`/`list`/`config`, `run`/`actions`, `system init`), фаза build (`install --phase build`).
- Упаковка: `.deb` (nfpm) с гостевыми бинарями, тарболы с `copyright`, `SHA256SUMS`, `repro-check`, `vuln`
  (toolchain `golang:1.26.9`, Р26); CI-матрица и ассеты релиза через release-please (первая версия 1.0.0, Р31).
- Проверки: `make test-integration` (systemd PID 1 в контейнере: debian-12, ubuntu-24.04, debian-13) и
  `make test-build` (настоящий `virt-customize` — и контейнер Debian 13, и 1.48 хоста, Р30).
- Финальное ревью (задача 23): секрет в `resolve` только именем (Р28), единые коды ввода-вывода `io.*` (Р29),
  `stage`/`--require-pinned` — 64 `usage.reserved`, окружение хука по списку валидатора, `lock/` 0700.
- Документация: [docs/cli.md](cli.md) (полные коды ошибок и предупреждений), [ADR 0001](adr/0001-contract-cream-v1.md),
  [ADR 0002](adr/0002-single-dependency-stdlib-cli.md), README, карта `AGENTS.md`.

## Дальше
1. PR `feature/cream-1.1` → `main`, зелёный `gh pr checks` (в том числе arm64), выпуск `v1.1.0`, сверка ассетов
   с `SHA256SUMS`.
2. Шаг 3 §16 спеки: перевод каталога `cream-extensions` на cream/v1 (пин 1.1.0).
3. Интеграция bisquite: фаза build по [docs/cli.md «Для bisquite»](cli.md#для-bisquite-фаза-build).

## Бэклог
- `runner` ставит действию `Setpgid`: с `stdin: pass` оно в фоновой группе терминала, чтение с tty даст SIGTTIN.
- Образы CI и тестов закреплять по digest, а не по тегу.
- CI на PR release-please: PR, открытый под `GITHUB_TOKEN`, не запускает workflow — проверки релизного PR не идут.

## Сломано
- Ничего известного.
