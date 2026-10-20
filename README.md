# cream

Исполнитель расширений для машин и образов: один статический бинарь `cream`
(linux/amd64, linux/arm64), ставится пакетом `.deb`.

cream:

- читает манифест расширения `extension.yaml` (формат `cream/v1`);
- ведёт жизненный цикл `install` / `upgrade` / `configure` / `remove` на живой
  машине и фазу `build` внутри `virt-customize` — её вызывает
  [bisquite](https://github.com/iamletenkov/bisquite) в слоях сборки образа;
- хранит состояние установленного в `/var/lib/cream`;
- `cream set` / `cream show` — типизированные настройки расширений (бывший
  `bisquite-conf`), совместимые с `EnvironmentFile` systemd;
- `cream run` / `cream actions` — действия расширений с машинно-читаемым
  выводом (через них работает stvor-agent);
- берёт расширения из источников-каталогов, например
  [cream-extensions](https://github.com/iamletenkov/cream-extensions); сеть —
  только в `cream source sync`.

## Установка

Пакеты лежат в [выпусках GitHub](https://github.com/iamletenkov/cream/releases):
`cream_<версия>_amd64.deb`, `cream_<версия>_arm64.deb`, тарболы
`cream_<версия>_linux_<arch>.tar.gz` и `SHA256SUMS`.

```bash
V=1.1.3; ARCH=$(dpkg --print-architecture)   # amd64 | arm64
curl -fLO "https://github.com/iamletenkov/cream/releases/download/v$V/cream_${V}_${ARCH}.deb"
curl -fLO "https://github.com/iamletenkov/cream/releases/download/v$V/SHA256SUMS"
sha256sum -c --ignore-missing SHA256SUMS
sudo apt install "./cream_${V}_${ARCH}.deb"
cream version
```

Пакет кладёт `/usr/bin/cream`, шаблон юнита `cream-configure@.service`,
юнит стадии `cloud-user` `cream-configure-cloud-user.service` (с 1.1.1) и
гостевые бинари обеих архитектур для хоста сборки
(`/usr/lib/cream/guest/linux-{amd64,arm64}/cream`); `postinst` делает
`cream system init`. Зависимостей нет; `Suggests: git, libguestfs-tools`.

## Быстрый старт

```bash
# 1. источник — архив каталога по URL и sha256, сразу синхронизировать
sudo cream source add core \
  --archive https://example.org/cream-extensions-5.1.0.tar.gz \
  --sha256 <sha256 архива> --sync

# 2. установить расширение с параметрами; секрет — только со stdin (KEY=-)
sudo cream install core/teleport TELEPORT_PROXY=proxy.example:443

# 3. поменять настройку: проверка всех ключей, запись, применение по apply расширения
sudo cream set teleport TELEPORT_PROXY=proxy2.example:443

# 4. действие расширения; флаги cream — до имени расширения
cream actions teleport
sudo cream run --json teleport status

# 5. что установлено и в каком состоянии
cream list
cream show teleport
```

Все команды и флаги — [docs/cli.md](docs/cli.md); коды выхода и `error.code` —
[docs/cli.md «Коды выхода»](docs/cli.md#коды-выхода-спека-113): 0 — успех, 64 — неверный
вызов, 65 — неверные данные, 66 — не найдено, 75 — блокировка занята, 79 — упал хук или
действие; у `cream run` — код самого действия (0–63).

## Документация

- [docs/cli.md](docs/cli.md) — справочник CLI 1.1: команды, флаги, коды выхода и
  предупреждений, формы `--json`, контракт для stvor-agent, фаза build для bisquite;
- [спека cream/v1](docs/specs/2026-10-18-cream-core.md) — формат манифеста
  `extension.yaml` (§4), жизненный цикл, блокировки, настройки, источники;
- runbooks: [01 выпуск](docs/runbooks/01_release.md),
  [02 диагностика на машине](docs/runbooks/02_diagnostics.md),
  [03 обновление cream на роботе](docs/runbooks/03_upgrade-cream.md);
- [ADR](docs/adr/README.md) — решения, которые трудно откатить;
- [docs/README.md](docs/README.md) — полный индекс, [docs/STATUS.md](docs/STATUS.md) —
  текущее состояние;
- [CHANGELOG.md](CHANGELOG.md) — изменения по версиям.

## Сборка

Нужны `docker` и `make`; Go на хосте не нужен — всё идёт в контейнере
`golang:1.26.9`.

```bash
make help        # список целей
make build       # dist/cream под архитектуру хоста
make build-all   # dist/cream-linux-amd64, dist/cream-linux-arm64
make test        # go test -race ./...
make lint        # gofmt, go vet, golangci-lint
make ci          # lint + test + build-all — то же, что CI
```

Версия зашивается при сборке из `git describe`: `dist/cream version`.

## Участие

Регламент разработки — [AGENTS.md](AGENTS.md). Коммиты — Conventional Commits
(текст по-русски), версии и `CHANGELOG.md` ведёт release-please.

## Лицензия

[Apache-2.0](LICENSE), © 2026 Letenkov Maksim.
