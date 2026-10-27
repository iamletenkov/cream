# Выпуск версии

Версию ведёт release-please по Conventional Commits: `fix` — patch, `feat` —
minor, `!` — major. Теги и `CHANGELOG.md` руками не трогаем.

## Шаги

1. Изменение уходит в `main` PR-ом со squash; заголовок PR = заголовок коммита
   (`fix(catalog): …`).
2. После зелёного CI на `main` job **Release Please** открывает или обновляет PR
   `chore(main): release X.Y.Z`:
   ```bash
   gh pr list --label "autorelease: pending"
   ```
3. Релизный PR открывается от секрета `RELEASE_PR_TOKEN` и запускает CI сам (без
   секрета — под `GITHUB_TOKEN`, и проверок на PR нет). Убедиться, что CI зелёный,
   и прочитать `CHANGELOG.md` в диффе:
   ```bash
   gh run list -L 3 --branch main
   gh pr diff <N>
   ```
4. Слить релизный PR (squash). Слияние создаёт тег `vX.Y.Z` и релиз; job
   **Release assets** собирает `make dist-release` на теге (`SOURCE_DATE_EPOCH` —
   время коммита тега) и `BUILDINFO.json` и передаёт их артефактом; job
   **Release source check** (без секретов) сверяет, что коммит тега — предок
   `origin/main` и `BUILDINFO.json` собран из него; job **Sign and upload
   release** (environment `release`, кода тега не запускает) подписывает
   `SHA256SUMS` ключом `stvor-platform`, проверяет подпись без сети и
   прикладывает ассеты.

## Подпись

Схема — как у релиза форка Teleport: cosign своим ключом ECDSA P-256, без
Rekor (`scripts/release-sign.sh`, версия cosign и его sha256 — `env` в
`.github/workflows/ci.yml`). Публичные ключи — `release/keys/stvor-platform.pub`
(текущий) и `stvor-platform-next.pub` (следующий, офлайн у владельца).

- Environment `release` (Settings → Environments): ветка `main` и теги `v*`;
  секреты `COSIGN_PRIVATE_KEY` (PEM ключа `stvor-platform`) и `COSIGN_PASSWORD`.
  Нет секрета — job подписи падает, ассеты в релиз не загружаются.
- Подписывается только тег, чей коммит — предок `origin/main`, и только
  сборка с `BUILDINFO.json` от этого коммита: environment проверяет ref
  запуска, а не тег.
- Подпись проверяется ключом `release/keys/stvor-platform.pub`: секрет от
  другого ключа — отказ до загрузки.
- Загрузка — всегда полный набор с `--clobber`. Опубликованная подпись
  сохраняется, только если `SHA256SUMS` совпал байт в байт и `.sig` и
  `.bundle.json` проверились ключом; иначе (оборванная загрузка, выпуск до
  этой схемы) — подпись заново. Повтор — ручной запуск с `upload=true`.
- Самопроверка без секретов (одноразовый ключ) — job **Release signing
  selftest** на каждом PR.

## Проверка

```bash
gh release view vX.Y.Z --json assets --jq '.assets[].name'
# cream_X.Y.Z_amd64.deb, cream_X.Y.Z_arm64.deb, cream_X.Y.Z_linux_{amd64,arm64}.tar.gz,
# BUILDINFO.json, SHA256SUMS, SHA256SUMS.sig, SHA256SUMS.bundle.json
mkdir -p .scratchpad/release && cd .scratchpad/release && gh release download vX.Y.Z --clobber
sha256sum -c SHA256SUMS            # все OK
base64 -d SHA256SUMS.sig > sig.der
openssl dgst -sha256 -verify ../../release/keys/stvor-platform.pub -signature sig.der SHA256SUMS   # Verified OK
```

Воспроизводимость: сборка чистого checkout тега с тем же `SOURCE_DATE_EPOCH`
даёт те же байты.

```bash
make dist-release VERSION=vX.Y.Z SOURCE_DATE_EPOCH=$(git log -1 --format=%ct vX.Y.Z)
grep -v ' BUILDINFO.json$' .scratchpad/release/SHA256SUMS | diff dist/release/SHA256SUMS -   # пусто
make repro-check                    # две сборки с разными кешами — SHA256SUMS совпадают
```

## Подъём пина у потребителей

После ассетов job **Bump cream in …** (матрица `dispatch` в
`.github/workflows/ci.yml`) запускает подъём пина у потребителей; каждый
открывает PR в своём репозитории (повтор идемпотентен).

| Цель | Workflow там | Что меняет PR | Выключатель (переменная здесь) | Секрет здесь |
|---|---|---|---|---|
| cream-extensions | `cream-bump.yml` | `tools/cream-pin.env` | `EXTENSIONS_BUMP_ENABLED` | `EXTENSIONS_DISPATCH_TOKEN` |
| bisquite | `cream-bump.yml` | `cream-pin.env`, фикстуры `tests/fixtures/cream/<v>/` | `BISQUITE_BUMP_ENABLED` | `BISQUITE_DISPATCH_TOKEN` |
| stvor | `robot-pins-bump.yml` | `CREAM_*` в `.env.example` | `STVOR_BUMP_ENABLED` | `STVOR_DISPATCH_TOKEN` |

Секрет — fine-grained PAT только на целевой репозиторий с правом Actions: Read
and write. Включить — `gh variable set EXTENSIONS_BUMP_ENABLED -R iamletenkov/cream --body true`,
выключить — `gh variable delete …`. Без секрета job пишет предупреждение и
остаётся зелёным. Повторить подъём руками — в целевом репозитории
Actions → Run workflow с `tag=vX.Y.Z`.

## Если ассетов нет или они битые

Actions → CI → Run workflow (с ветки `main`) с `tag=vX.Y.Z`: без `upload` —
сухой прогон (сборка, сверка `SHA256SUMS` и источника, без секретов и подписи),
с `upload=true` — подпись и загрузка полного набора в релиз с заменой файлов.
Неверная запись в changelog чинится revert-коммитом; история `main` не
переписывается.
