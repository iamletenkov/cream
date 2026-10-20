# 01. Выпуск версии cream

Версию ведёт release-please по Conventional Commits (`fix` → patch, `feat` → minor, `!` → major). Теги и
`CHANGELOG.md` руками не трогаем. Подробности — skill `/release` (`.claude/skills/release/SKILL.md`).

## Шаги
1. Изменение уходит в `main` PR-ом со squash; заголовок PR = заголовок коммита (`fix(catalog): …`).
2. После зелёного CI на `main` job **Release Please** открывает или обновляет PR `chore(main): release X.Y.Z`.
   ```bash
   gh pr list --label "autorelease: pending"
   ```
3. PR `chore(main): release …`, открытый под `GITHUB_TOKEN`, проверок не запускает. Проверить, что `main` зелёный, и
   прочитать `CHANGELOG.md` в диффе:
   ```bash
   gh run list -L 3 --branch main
   gh pr diff <N>
   ```
4. Слить релизный PR (squash). Слияние создаёт тег `vX.Y.Z` и релиз; job **Release assets** собирает
   `make dist-release` на теге (`SOURCE_DATE_EPOCH` = время коммита тега) и прикладывает ассеты.

## Проверка
```bash
gh release view vX.Y.Z --json assets --jq '.assets[].name'
# cream_X.Y.Z_amd64.deb, cream_X.Y.Z_arm64.deb, cream_X.Y.Z_linux_{amd64,arm64}.tar.gz, SHA256SUMS
mkdir -p .scratchpad/release && cd .scratchpad/release && gh release download vX.Y.Z --clobber
sha256sum -c SHA256SUMS            # все OK
```
Воспроизводимость: сборка на чистом checkout тега с тем же `SOURCE_DATE_EPOCH` даёт те же байты.
```bash
make dist-release VERSION=vX.Y.Z SOURCE_DATE_EPOCH=$(git log -1 --format=%ct vX.Y.Z)
diff dist/release/SHA256SUMS .scratchpad/release/SHA256SUMS   # пусто
make repro-check                    # две сборки с разными кешами — SHA256SUMS совпадают
```
(Каталог вывода — `RELEASE_DIR` в `Makefile`.)

## Если ассетов нет или они битые
Actions → CI → Run workflow с `tag=vX.Y.Z`: без `upload` — сухой прогон (сборка и сверка `SHA256SUMS`), с
`upload=true` — загрузка в релиз с заменой файлов. Неверная запись в changelog чинится revert-коммитом, история
`main` не переписывается.
