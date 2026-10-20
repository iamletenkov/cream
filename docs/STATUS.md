# Статус cream

**Выпущены 1.0.0–1.1.3** (2026-10-19…20). Ветка `fix/unit-lock-nits` (→ 1.1.4): `validate.unit_lock` —
«анализ неполон» только у расширения, где он случился, вердикт «возможен тупик»; `service`/`invoke-rc.d` — только в
позиции команды. Документация сверена с 1.1.3, добавлены [runbooks](runbooks/01_release.md) (выпуск, диагностика,
обновление cream на роботе). Дальше — ревью PR и выпуск 1.1.4.

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
1. PR `fix/unit-lock-nits` → `main`, зелёный `gh pr checks`, выпуск `v1.1.4` по [runbook 01](runbooks/01_release.md).
2. Каталог `cream-extensions` на cream/v1 (5.1.0, пин cream 1.1.2+) — поднять пин после 1.1.4.
3. Интеграция bisquite: фаза build по [docs/cli.md «Для bisquite»](cli.md#для-bisquite-фаза-build).

## Бэклог
- Drop-in стадии переписывает только `install`/`upgrade` расширения: у `early`, включённого cream 1.1.0, строки
  `shutdown.target` появятся после его upgrade (`system init` переносит только ссылки).
- `runner` ставит действию `Setpgid`: с `stdin: pass` оно в фоновой группе терминала, чтение с tty даст SIGTTIN.
- Образы CI и тестов закреплять по digest, а не по тегу.
- CI на PR release-please: PR, открытый под `GITHUB_TOKEN`, не запускает workflow — проверки релизного PR не идут.

## Сломано
- Ничего известного.
