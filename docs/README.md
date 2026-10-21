# Документация cream

Начните с [первых шагов](getting-started.md). Новый документ добавляется строкой
в этот индекс.

## Начало

- [Первые шаги](getting-started.md) — установить cream, подключить каталог,
  поставить и настроить расширение.

## Руководства

| Задача | Документ |
|---|---|
| подключить каталог расширений, поднять его версию, работать без сети | [guides/add-catalog.md](guides/add-catalog.md) |
| поставить расширение, передать секрет, поменять настройки, вызвать действие, удалить | [guides/install-extension.md](guides/install-extension.md) |
| написать своё расширение и каталог, проверить без root | [guides/write-extension.md](guides/write-extension.md) |
| выбрать стадию `configure` (`early`, `system`, `cloud-user`, `graphical`), написать и отладить хук настройки на загрузке | [guides/configure-stages.md](guides/configure-stages.md) |
| понять, что сломалось: статусы, журналы, блокировки, коды | [guides/diagnostics.md](guides/diagnostics.md) |
| обновить расширения: план, `--dry-run`, закрепление (`hold`), риск, частичный исход | [guides/upgrade-extensions.md](guides/upgrade-extensions.md) |
| обновить или откатить cream на машине | [guides/upgrade-cream.md](guides/upgrade-cream.md) |
| ставить расширения в образ при сборке (`virt-customize`) | [guides/build-phase.md](guides/build-phase.md) |

## Справочник

| Что | Документ |
|---|---|
| команды и флаги | [reference/cli.md](reference/cli.md) |
| `extension.yaml`, `cream-catalog.yaml`, `sources.yaml` | [reference/manifest.md](reference/manifest.md) |
| коды выхода, `error.code`, предупреждения, коды хуков | [reference/exit-codes.md](reference/exit-codes.md) |
| конверт `--json`, формы `result`, контракт `run`/`actions` для агентов | [reference/json-output.md](reference/json-output.md) |
| пути на машине, переменные окружения, окружение хуков, юниты, блокировки | [reference/files-and-environment.md](reference/files-and-environment.md) |

## Устройство и разработка

| Документ | Зачем читать |
|---|---|
| [architecture.md](architecture.md) | место в экосистеме, ход операции, пакеты Go |
| [development.md](development.md) | сборка, тесты, пакеты, как вносить изменения |
| [runbooks/01_release.md](runbooks/01_release.md) | выпуск версии: release-please, ассеты, проверка, подъём пина у потребителей |
| [adr/README.md](adr/README.md) | архитектурные решения: [0001](adr/0001-contract-cream-v1.md) — контракт `cream/v1`, [0002](adr/0002-single-dependency-stdlib-cli.md) — одна зависимость, CLI на stdlib, nfpm, [0003](adr/0003-upgrade-plan-hold.md) — `upgrade`: план до действий, `hold`, код худшего исхода |
| [../CHANGELOG.md](../CHANGELOG.md) | изменения по версиям |

## Проектные документы

Исторические и рабочие материалы: по ним строился cream, для пользования они не
нужны.

| Документ | Что это |
|---|---|
| [specs/2026-10-18-cream-core.md](specs/2026-10-18-cream-core.md) | исходная спека `cream/v1` с обоснованиями и отвергнутыми вариантами |
| [specs/2026-10-21-bisquite-bundled-cream.md](specs/2026-10-21-bisquite-bundled-cream.md) | спека (редакция 4, вопросы закрыты): bisquite везёт cream и каталог бандлом, скрытый шаг cream, VMFILE с `RUN_COMMAND cream install`, проверка как hadolint, метки из `/etc/image-labels.d`, `bs image diff`, configure по отметке выполнения и признаки `every_boot` |
| [plans/2026-10-21-bisquite-bundled-cream.md](plans/2026-10-21-bisquite-bundled-cream.md) | план к ней: 29 задач по дорожкам cream / cream-extensions / bisquite / Stvor, порядок выпусков cream 1.2.0 → каталог → пин Stvor → bisquite 3.0.0 |
| [plans/2026-10-18-cream-core.md](plans/2026-10-18-cream-core.md) | план MVP (закрыт) |
| [research/2026-10-18-bisquite-extension-system-inventory.md](research/2026-10-18-bisquite-extension-system-inventory.md) | опись системы расширений bisquite до cream |
| [STATUS.md](STATUS.md) | текущее состояние работ (handoff между сессиями) |
