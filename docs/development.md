# Разработка

Нужны `docker` и `make`; Go на хосте не нужен — сборка и проверки идут в
контейнере `golang:1.26.9`, кеш модулей — в `.cache/`.

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
make vuln                    # govulncheck
```

## Как вносить изменения

- Ветка от свежего `origin/main`, в `main` — через PR со squash.
- Коммиты — [Conventional Commits](https://www.conventionalcommits.org/ru/): тип и
  область латиницей, текст по-русски (`fix(cli): …`). Версию и `CHANGELOG.md`
  ведёт release-please; выпуск — [runbook](runbooks/01_release.md).
- Тест пишется до кода. Перед PR — зелёный `make ci`.
- Изменение формата, CLI, путей или зависимостей — через [ADR](adr/README.md).

Регламент для людей и агентов целиком — [AGENTS.md](../AGENTS.md).
