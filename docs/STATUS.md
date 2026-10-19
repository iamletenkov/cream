# Статус cream

**Фаза:** MVP 1.0 — волна 7 [плана](plans/2026-10-18-cream-core.md) (2026-10-19).

## Сделано
- Задачи 1–17 в `feature/cream-mvp`: формат `cream/v1`, источники и `catalog pack`, резолвер, ядро исполнителя,
  `install`/`upgrade`/`remove`, настройки, действия `run`/`actions`, фаза build (`install --phase build`).
- Раунд исправлений 5: сигналы SIGINT/SIGTERM/SIGHUP, повтор старта при ETXTBSY, `flock` кеша своих источников
  (Р24), запрет зарезервированных имён параметров; задача 18 — `.deb` через nfpm, `repro-check`, `vuln`.
- Документация (задача 21): [docs/cli.md](cli.md), [ADR 0001](adr/0001-contract-cream-v1.md) и
  [ADR 0002](adr/0002-single-dependency-stdlib-cli.md), README с установкой и быстрым стартом.

## Дальше
1. Задачи 19 (интеграция с systemd в контейнерах) и 20 (CI-матрица, ассеты релиза), затем 22 (`make test-build`).
2. Задача 23: ревью ветки, `make ci`, PR `feature/cream-mvp` → `main` с `Release-As: 1.0.0`.
3. После выпуска — шаг 2 §16 спеки: пилот в `cream-extensions`.
4. Карта репозитория в `AGENTS.md` (`internal/*`, `packaging/`, `test/`, `testdata/`) — ещё «каркас», обновить.

## Сломано
- Ничего известного.
