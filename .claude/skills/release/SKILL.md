---
name: release
description: Как выходит версия cream — release-please по Conventional Commits; что проверить перед слиянием релизного PR.
---
# /release

Версию не ставят руками: ни тегов, ни правок `CHANGELOG.md`.

1. Каждый PR сливается squash-ем; заголовок PR = заголовок коммита в `main` (`feat(…)`/`fix(…)`/`feat!:` …, текст по-русски).
2. На push в `main` после зелёных проверок job **Release Please** (`.github/workflows/ci.yml`, конфиг — `.github/release_please/`) ведёт PR «chore(main): release X.Y.Z» с `CHANGELOG.md`. До 1.0.0 `feat` поднимает minor, `fix` — patch; первая версия — 0.1.0.
3. Слияние релизного PR создаёт тег `vX.Y.Z` и опубликованный релиз.
4. Перед слиянием релизного PR: `gh pr checks <N>` зелёный; `gh run list -L 3` — main зелёный; в changelog нет мусорных записей (кривой заголовок чинится revert-ом, история `main` не переписывается).
5. К опубликованному релизу job **Release assets** прикладывает `cream_<v>_{amd64,arm64}.deb`, `cream_<v>_linux_{amd64,arm64}.tar.gz` и `SHA256SUMS` — `make dist-release` на теге с `SOURCE_DATE_EPOCH` = время коммита тега (спека §15). Пересобрать или дозагрузить: Actions → CI → Run workflow, `tag=vX.Y.Z` (сухой прогон: сборка и сверка `SHA256SUMS`), `upload=true` — загрузка в релиз с заменой файлов. Локально те же байты даёт `make dist-release VERSION=vX.Y.Z SOURCE_DATE_EPOCH=$(git log -1 --format=%ct vX.Y.Z)`.

Проверить состояние: `gh release list -L 5`, `gh pr list --label "autorelease: pending"`.
