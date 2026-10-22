# Фикстуры cream 1.2.0 для e2e и integration

Каталог для `make test-build` (`test/build/scenario.sh`) и `make test-integration`
(`test/integration/scenario/64…66-*.sh`): отметка выполнения configure, `every_boot`,
код `20`, падающий configure, `cream configure --again` из хука, ключ `scope: build`
(спека `docs/specs/2026-10-21-bisquite-bundled-cream.md` §10.7–§10.9). Хуки configure
считают вызовы в `$CREAM_STATE_DIR/configure.count` и пишут `reason=… phase=…` в
`configure.log`. В unit-тестах не используется: фикстуры unit — `testdata/`.
