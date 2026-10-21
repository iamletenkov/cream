# Устройство cream

Кратко — из чего состоит cream и как идёт операция. Решения и их причины —
[ADR](adr/README.md) и [спека](specs/2026-10-18-cream-core.md).

## Место в экосистеме

```
cream-extensions ──(архив каталога)──► cream ◄──(cream run --json)── stvor-agent
                                         ▲
                     bisquite ──(resolve на хосте, install --phase build в госте)
```

- **cream-extensions** — каталог расширений `cream/v1`.
- **bisquite** — сборщик образов: разрешает слои `cream resolve` и ставит их
  `cream install --phase build` внутри `virt-customize`.
- **stvor-agent** — служба на машине; управляет ею через действия расширения
  `teleport` (`cream run --json`).

Между ними три контракта: формат `extension.yaml`, CLI с формами `--json` и
кодами выхода, раскладка путей на машине. Они меняются только с мажорной версией.

## Принципы

- **Один статический бинарь**, одна внешняя зависимость (парсер YAML), CLI на
  стандартной библиотеке Go.
- **Сеть — только в `source sync`.** Всё остальное работает по проверенному кешу,
  поэтому одинаково и на машине, и в госте при сборке.
- **Проверки до действий.** Платформа, граф и значения проверяются до первого
  хука; файлы и реестр пишутся атомарно (`rename`).
- **Состояние — в файлах.** Запись реестра на расширение, журнал
  `history.jsonl`, блокировки `flock`.

## Ход `cream install`

1. Найти расширение в кеше источников, проверить манифест и версию cream.
2. Проверить платформу, `targets`, граф (`depends`, `requires`, `conflicts`,
   владение файлами) на множестве «установленное + запрошенное».
3. Взять `lock/global` и `lock/<ext>`.
4. Скопировать дерево в `/opt/cream/ext/.<ext>.new`, затем `rename`.
5. Разложить `files`, записать параметры в `/etc/cream/<ext>/`.
6. Выполнить хук `install`.
7. Включить `cream-configure@<ext>` в цели стадии и на живой машине выполнить
   `configure`.
8. Записать реестр и строку журнала.

При сбое хука запись получает `status: failed`, дерево и файлы остаются для
повтора или `remove`.

## Пакеты Go

| Пакет | Что делает |
|---|---|
| `cmd/cream` | точка входа |
| `internal/cli` | разбор команд и флагов, конверт `--json`, коды выхода, man-страницы |
| `internal/engine` | операции: install, upgrade, remove, set, run, configure, фаза build |
| `internal/resolve` | платформа, граф зависимостей, порядок установки, lineage |
| `internal/catalog` | проверка каталога, поиск расширения в кеше, анализ взаимных блокировок |
| `internal/source` | `sources.yaml`, загрузка и распаковка архивов, кеш, `catalog pack` |
| `internal/manifest` | строгий разбор `extension.yaml` и `cream-catalog.yaml` |
| `internal/config` | настройки расширения: файлы `config`/`secrets`, присваивания |
| `internal/keytype` | типы значений ключей |
| `internal/envfile` | грамматика `EnvironmentFile` systemd с сохранением комментариев |
| `internal/state` | реестр установленного и журнал |
| `internal/lock` | блокировки и их наследование вложенными вызовами |
| `internal/runner` | запуск хуков и действий: окружение, таймауты, фильтр секретов |
| `internal/systemd` | юниты configure, drop-in стадий, `systemctl` |
| `internal/place` | раскладка и снятие `files` |
| `internal/paths` | пути на машине, `--root`, режим пользователя |
| `internal/fsx`, `internal/semver`, `internal/cerr`, `internal/version` | файловые примитивы, SemVer, типизированные ошибки, версия |

Тесты: модульные рядом с кодом; эталоны вывода CLI —
`internal/cli/testdata/golden/`; пакет `.deb` в контейнерах с systemd —
`test/integration/`; фаза build настоящим `virt-customize` — `test/build/`.
