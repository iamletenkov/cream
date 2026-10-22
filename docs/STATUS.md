# Статус cream

**Выпущены 1.0.0–1.1.4** (2026-10-19…21). Документация переложена по задачам: [docs/README.md](README.md) —
первые шаги, руководства, справочник (сверен с 1.1.4).

## Сделано
- Формат `cream/v1`, источники archive/path и `catalog pack`, резолвер, исполнитель (`install`/`upgrade`/`remove`,
  `set`/`show`/`get`/`list`/`config`, `run`/`actions`, `system init`), фаза build (`install --phase build`).
- Упаковка: `.deb` (nfpm) с гостевыми бинарями, тарболы с `copyright`, `SHA256SUMS`, `repro-check`, `vuln`
  (toolchain `golang:1.26.9`, Р26); CI-матрица и ассеты релиза через release-please (первая версия 1.0.0, Р31).
- Проверки: `make test-integration` (systemd PID 1 в контейнере: debian-12, ubuntu-24.04, debian-13) и
  `make test-build` (настоящий `virt-customize` — и контейнер Debian 13, и 1.48 хоста, Р30).
- Финальное ревью (задача 23): секрет в `resolve` только именем (Р28), единые коды ввода-вывода `io.*` (Р29),
  `stage`/`--require-pinned` — 64 `usage.reserved`, окружение хука по списку валидатора, `lock/` 0700.
- Документация: README, [первые шаги](getting-started.md), [руководства](guides/), [справочник](reference/cli.md),
  [ADR 0001](adr/0001-contract-cream-v1.md), [ADR 0002](adr/0002-single-dependency-stdlib-cli.md), карта `AGENTS.md`.

- **OpenWrt** (план bisquite-bundled-cream Task 27, [ADR 0005](adr/0005-openwrt-family-procd.md), выйдет минором
  1.3.0): `platform.family: openwrt`, раскладка `/opt/cream/var`, configure из init-скриптов procd (`S19`/`S95`),
  `TMPDIR` на диске, `IPKG_INSTROOT=/` в фазе build, поставка `tar.gz`; `make test-openwrt` (корневая ФС
  openwrt-builder v0.3.1 в контейнере) и приёмка на ВМ PVE — [руководство](guides/openwrt.md).

- **План bisquite-bundled-cream закрыт** (2026-10-22): cream 1.3.0, каталог 5.4.1, bisquite 3.0.0 со встроенным
  cream-бандлом; итог и расхождения — в [плане](plans/2026-10-21-bisquite-bundled-cream.md#итог-финала-2026-10-22-и-что-разошлось).

## В работе
- `fix/upgrade-plan` ([ADR 0003](adr/0003-upgrade-plan-hold.md)): `upgrade` — план до действий, `--dry-run`,
  `--accept-risk <ext>`, `-y`, `--fail-fast`, частичный исход с кодом худшего; `cream hold`/`unhold`.
  Исправляет падение `upgrade --all` кодом 68 из-за `targets.live: confirm` без новой версии.

## Дальше
1. Проверить PR подъёма пина cream 1.1.4 в cream-extensions, bisquite и Stvor (dispatch выпуска).
2. Интеграция bisquite: фаза build по [guides/build-phase.md](guides/build-phase.md).

## Бэклог
- Drop-in стадии переписывает только `install`/`upgrade` расширения: у `early`, включённого cream 1.1.0, строки
  `shutdown.target` появятся после его upgrade (`system init` переносит только ссылки).
- `runner` ставит действию `Setpgid`: с `stdin: pass` оно в фоновой группе терминала, чтение с tty даст SIGTTIN.
- Образы CI и тестов закреплять по digest, а не по тегу.
- CI на PR release-please: PR, открытый под `GITHUB_TOKEN`, не запускает workflow — проверки релизного PR не идут.

## Сломано
- Ничего известного.
