# Разработка

Нужны `docker` и `make`; Go на хосте не нужен — сборка и проверки идут в
контейнере `golang:1.26.10`, кеш модулей — в `.cache/`.

```bash
make help              # все цели
make build             # dist/cream под архитектуру хоста
make build-all         # dist/cream-linux-amd64, dist/cream-linux-arm64
make test              # go test -race ./...
make test-golden       # переписать эталоны вывода CLI после намеренного изменения
make lint              # gofmt, go vet, golangci-lint
make ci                # lint + test + build-all — то же, что CI
make man               # dist/man/cream.1, cream-extension.yaml.5; смотреть: man -l dist/man/cream.1
```

Версия зашивается из `git describe`: `dist/cream version`.

## Пакеты и интеграция

```bash
make deb ARCH=amd64          # dist/release/cream_<v>_amd64.deb
make dist-release            # оба .deb, тарболы и SHA256SUMS
make repro-check             # две сборки с одним SOURCE_DATE_EPOCH дают те же байты
make test-integration        # .deb в контейнере с systemd; DISTRO=debian-12|ubuntu-24.04|debian-13|all
make test-build              # фаза build настоящим virt-customize (вручную, не в ci)
make test-openwrt            # cream на OpenWrt 25.12 (musl, busybox, init-скрипты procd) в контейнере из корневой ФС openwrt-builder
make vuln                    # govulncheck
```

## Карта репозитория
```
cmd/cream/            — точка входа: run(args, stdout, stderr) int → internal/cli
internal/cli/         — диспетчер команд: flag с перестановкой, глобальные флаги, конверт --json, коды выхода; golden в testdata/golden
internal/engine/      — исполнитель: реестр + домен настроек + блокировки + хуки + systemd = install/upgrade/remove/set/run/build
internal/resolve/     — резолвер: платформа и применимость, граф depends/requires/conflicts, порядок, lineage (§9.4, §12.4)
internal/catalog/     — валидация каталога (§4.1) и поиск расширения по ссылке в кеше источников (§9.3)
internal/source/      — sources.yaml, единственный сетевой доступ (sync), кеш и sources.lock, catalog pack
internal/manifest/    — строгий разбор extension.yaml cream/v1 и cream-catalog.yaml
internal/config/      — домен настроек расширения: /etc/cream/<ext>/{config,secrets}, присваивания, params-file
internal/state/       — реестр установленного (cream-state/v1) и журнал history.jsonl
internal/lock/        — flock-блокировки lock/{global,<ext>} и наследование через CREAM_LOCK_FD
internal/runner/      — запуск хуков и действий: окружение, группа процессов, таймауты, фильтр секретов
internal/systemd/     — юнит cream-configure@.service, drop-in стадии, systemctl
internal/procd/       — OpenWrt: init-скрипты cream-configure{,-early} (S95/S19), отметки стадий /opt/cream/var/stages
internal/initsys/     — выбор init по семейству раскладки (systemd | procd, ADR-0005)
internal/place/       — раскладка files расширения и их снятие при remove (владение по реестру)
internal/paths/       — раскладка путей на машине (§5), --root, режим пользователя (XDG), семейство openwrt (/opt/cream/var)
internal/envfile/     — грамматика EnvironmentFile= systemd: чтение и правка с сохранением комментариев
internal/fsx/         — файловые примитивы: атомарная запись, копия дерева, дайджест дерева, проверка прав
internal/keytype/     — типы значений ключей и параметров действий (§8.2)
internal/semver/      — SemVer 2.0 и диапазоны (своя реализация, без зависимости)
internal/cerr/        — типизированные ошибки, коды выхода и предупреждения (§11.3)
internal/version/     — версия (ставится -ldflags -X при сборке)
testdata/catalog/     — фикстурный каталог §14; testdata/catalog-next/ — следующие версии noop и upgrade-fixture для upgrade
testdata/invalid/     — заведомо неверные расширения для cream validate
testdata/check/       — каталоги core и local для cream check (unit и golden)
testdata/contract/    — контрактные фикстуры: поля новее выпущенного cream, старый выпуск отказывает по cream: (make contract-old)
testdata/openwrt/     — фикстуры семейства openwrt (fixture-openwrt, fixture-openwrt-early) для unit и make test-openwrt
testdata/pilot/       — пилотные расширения (cloud-user, fixture-{agent,docker,teleport}-like) для интеграции и build
test/integration/     — make test-integration: .deb в контейнере с systemd PID 1, сценарии scenario/NN-*.sh
test/build/           — make test-build: фаза build настоящим virt-customize на образе Debian 12
test/openwrt/         — make test-openwrt: корневая ФС OpenWrt 25.12 (openwrt-builder) в контейнере, сценарий под busybox sh
packaging/            — nfpm.yaml, maintainer-скрипты, copyright (полные тексты сторонних лицензий)
scripts/              — dist-release.sh (тарболы, SHA256SUMS), check-deb.sh (состав пакета), repro-check.sh, contract-old.sh (фикстуры testdata/contract на релизном cream), release-sign.sh (BUILDINFO.json, cosign-подпись SHA256SUMS)
LICENSE, NOTICE       — Apache-2.0 и уведомления о стороннем коде
release/keys/         — публичные ключи подписи выпусков: stvor-platform.pub (текущий), stvor-platform-next.pub (следующий)
docs/                 — README.md (индекс), getting-started.md, guides/ (по задачам), reference/ (CLI, манифест, коды, --json, пути), architecture.md, development.md, runbooks/ (выпуск), adr/, specs/, plans/, research/, STATUS.md
.claude/              — агенты, skills, правила, хуки Claude Code
.github/              — CI (ci, vuln, repro-check, интеграция amd64/arm64), release-please, ассеты и подпись релиза (environment release)
Makefile              — единственный способ что-либо запускать (`make help`)
```

## Как вносить изменения

- Ветка от свежего `origin/main`, в `main` — через PR со squash.
- Коммиты — [Conventional Commits](https://www.conventionalcommits.org/ru/): тип и
  область латиницей, текст по-русски (`fix(cli): …`). Версию и `CHANGELOG.md`
  ведёт release-please; выпуск — [runbook](runbooks/01_release.md).
- Тест пишется до кода. Перед PR — зелёный `make ci`.
- Изменение формата, CLI, путей или зависимостей — через [ADR](adr/README.md).

Регламент для людей и агентов целиком — [AGENTS.md](../AGENTS.md).
