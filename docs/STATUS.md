# Статус cream

**1.1.1 (ветка `fix/1.1.1`, патч по ревью 1.1.0):** `cloud-user` тянет `cream-configure-cloud-user.service` из
`multi-user.target.wants` (configure работает и при выключенном cloud-init); `system init` не ждёт `lock/global`
(`system.registry_busy`); `catalog.depends_external` — предупреждение; `early` — `shutdown.target`, таймаут ≤ 2 мин,
`manifest.early_network`; describe после rc — dev; тесты на ложно-зелёные места. `validate.unit_lock` сужено до
реального тупика §7.3 (держатель блокировки синхронно ждёт задание юнита) — без ложных срабатываний на `teleport`. Дальше — ревью PR и выпуск 1.1.1.

**Фаза:** 1.0.0 и 1.1.0 выпущены (2026-10-19); все 23 задачи [плана](plans/2026-10-18-cream-core.md) 1.0 закрыты.

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
1. PR `fix/1.1.1` → `main`, зелёный `gh pr checks` (в том числе arm64), выпуск `v1.1.1`, сверка ассетов
   с `SHA256SUMS`.
2. Шаг 3 §16 спеки: перевод каталога `cream-extensions` на cream/v1 (пин 1.1.1).
3. Интеграция bisquite: фаза build по [docs/cli.md «Для bisquite»](cli.md#для-bisquite-фаза-build).

## Бэклог
- Drop-in стадии переписывает только `install`/`upgrade` расширения: у `early`, включённого cream 1.1.0, строки
  `shutdown.target` появятся после его upgrade (`system init` переносит только ссылки).
- `runner` ставит действию `Setpgid`: с `stdin: pass` оно в фоновой группе терминала, чтение с tty даст SIGTTIN.
- Образы CI и тестов закреплять по digest, а не по тегу.
- CI на PR release-please: PR, открытый под `GITHUB_TOKEN`, не запускает workflow — проверки релизного PR не идут.

## Сломано
- Ничего известного.
