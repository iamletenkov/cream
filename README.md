# cream

Исполнитель расширений для машин и образов: один статический бинарь `cream`
(linux/amd64, linux/arm64), который будет ставиться пакетом `.deb`.

Что будет уметь (по решению 2026-10-18, детали — в спеке):

- читать манифест расширения `extension.yaml`;
- вести жизненный цикл `install` / `upgrade` / `configure` / `remove` на живой
  машине и фазу `build` внутри `virt-customize` (её вызывает
  [bisquite](https://github.com/iamletenkov/bisquite) в слоях сборки образа);
- хранить состояние установленного;
- `cream set` / `cream show` — настройки расширений (бывший `bisquite-conf`);
- брать расширения из источников-каталогов, например
  [cream-extensions](https://github.com/iamletenkov/cream-extensions).

## Статус

**Каркас.** Есть только `cream version`; спека в работе. Текущее состояние —
[docs/STATUS.md](docs/STATUS.md), документация — [docs/README.md](docs/README.md).

## Сборка

Нужны `docker` и `make`; Go на хосте не нужен — всё идёт в контейнере
`golang:1.25.11`.

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
