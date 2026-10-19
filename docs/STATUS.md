# Статус cream

**1.1.0 (ветка `feature/cream-1.1`, задача 1 плана перевода каталога `cream-extensions`):** стадии configure
`early`, `cloud-user`, `graphical` (§6.4, манифест требует `cream: ">=1.1.0"`) и `cream resolve --installed FILE`
готовы; сборка не с тега (`git describe`) теперь dev. Проверено: `make ci`, `make vuln`, `make test-integration`
debian-12 и ubuntu-24.04 (с набором `desktop`: gdm3 + cloud-init, без `ordering cycle`). Статическая проверка графа
в `cream validate --catalog` и выпуск 1.1.0 — задача 4 того же плана.

**Фаза:** MVP 1.0 готов к выпуску — все 23 задачи [плана](plans/2026-10-18-cream-core.md) закрыты (2026-10-19);
ветка `feature/cream-mvp` ждёт PR в `main` с футером `Release-As: 1.0.0`.

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
1. PR `feature/cream-mvp` → `main` (`Release-As: 1.0.0`), зелёный `gh pr checks` (в том числе arm64), выпуск
   `v1.0.0`, сверка ассетов с `SHA256SUMS`.
2. Шаг 2 §16 спеки: пилот каталога в `cream-extensions`.
3. Интеграция bisquite: фаза build по [docs/cli.md «Для bisquite»](cli.md#для-bisquite-фаза-build).

## Бэклог
- `cream system init` перемещает ссылку стадии `cloud-user`, но `configure.target` в реестре остаётся прежним
  (поле справочное; пересчёт — при следующем `install`/`upgrade`).
- `runner` ставит действию `Setpgid`: с `stdin: pass` оно в фоновой группе терминала, чтение с tty даст SIGTTIN.
- Образы CI и тестов закреплять по digest, а не по тегу.
- CI на PR release-please: PR, открытый под `GITHUB_TOKEN`, не запускает workflow — проверки релизного PR не идут.

## Сломано
- Ничего известного.
