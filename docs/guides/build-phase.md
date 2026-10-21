# Фаза build: расширения при сборке образа

Для авторов сборщиков образов (так работает
[bisquite](https://github.com/iamletenkov/bisquite)). Расширение с
`targets.build: exec` ставится внутрь образа: cream на хосте разрешает слои по
кешу, а cream в госте (`virt-customize`) выполняет хук `install` без сети. Хост
сборки — Debian или Ubuntu с пакетом cream и `libguestfs-tools`; root на хосте
не нужен.

## 1. На хосте: разрешить слои

```bash
cream --json resolve --phase build \
  --target-arch amd64 --host-arch "$(dpkg --print-architecture)" --family deb \
  --sources "$DATA_DIR/sources.yaml" --cache "$DATA_DIR/cream" \
  core/apt-guard core/btop core/locale LOCALE=ru_RU.UTF-8
```

- Источники и кеш — свои (`--sources`, `--cache`), машинные не трогаются;
  синхронизировать их — `cream source sync` с теми же флагами.
- Каждый элемент `result.layers` — будущий `record.json` слоя; `dir` — дерево
  расширения в кеше. Секретные параметры называются только именем (`KEY` или
  `KEY=-`): значения в `resolve` не нужны.
- Если в базовом образе часть расширений уже стоит, передайте их списком:
  `--installed FILE` с JSON-массивом `[{"name", "version", "provides"?}]` или
  `[{"token": "<источник>/<имя>@<версия>#<rev>"}]` — они закроют `depends` и
  `requires`, но слоями не станут.
- Отказы: 67 — граф; 68 `platform.arch_barrier` — слой `exec` для чужой
  архитектуры (собирайте на хосте той же архитектуры); 69 — источник не
  синхронизирован.

## 2. На хосте: подготовить слой

Каталог 0700, в нём:

- `<name>/` — копия `dir`;
- `record.json` — элемент `layers` (можно добавить `source_type`, `source_origin`);
- `params.env` — все параметры слоя `KEY=VALUE`, включая секреты, права 0600.

Упаковать с владельцем root, иначе в госте `params.env` окажется не у root и будет
отвергнут (код 77):

```bash
tar --owner=0 --group=0 --numeric-owner -C "$STAGING" -cf "$STAGING/$ID.tar" .
```

Архив удаляется после вызова.

## 3. Один вызов `virt-customize` на слой

```
--copy-in /usr/lib/cream/guest/linux-<arch>/cream:/usr/bin
--chmod 0755:/usr/bin/cream
--run-command '/usr/bin/cream system init'
--mkdir /var/lib/cream/incoming/<id>
--copy-in <staging>/<id>.tar:/var/lib/cream/incoming/
--run-command 'tar -C /var/lib/cream/incoming/<id> --numeric-owner -xf /var/lib/cream/incoming/<id>.tar
     && rm -f /var/lib/cream/incoming/<id>.tar'
--run-command '/usr/bin/cream --json install --phase build
     --from-dir /var/lib/cream/incoming/<id>/<name>
     --record /var/lib/cream/incoming/<id>/record.json
     --params-file /var/lib/cream/incoming/<id>/params.env --remove-params-file'
```

- Гостевой бинарь копируется всегда: версия cream в образе равна версии на хосте.
- Последовательность работает и на `virt-customize` 1.48 (Debian 12), где нет
  `--tar-in`; поэтому каталог создаётся `--mkdir`.
- Параметры не попадают в argv `virt-customize` и в текст ошибок cream.

## 4. Что делает cream в госте

Под блокировкой `lock/global`:

1. сверяет имя, версию, отпечаток дерева и параметров с `record.json`
   (65 `record.mismatch`, поле — в `details.field`);
2. проверяет платформу и граф по **реестру образа** — расширения из базового
   образа учитываются;
3. ставит `/usr/sbin/policy-rc.d` (exit 101) на время хука, если его не было, и
   снимает после, в том числе при сбое — пакеты не запускают службы в образе;
4. выполняет хук `install` с `CREAM_PHASE=build`;
5. включает `configure` ссылкой в `*.wants`, не запуская его — он выполнится на
   первой загрузке на своей [стадии](configure-stages.md);
6. пишет реестр с `phase: build` и удаляет `incoming/<id>`; при сбое каталог
   остаётся для разбора. `params.env` с `--remove-params-file` удаляется в любом
   исходе.

## Коды фазы build

| Код | Причина |
|---|---|
| 0 | успех ([пример ответа](../../internal/cli/testdata/golden/install-build.json)) |
| 64 | неполный вызов: `--from-dir`/`--record` без `--phase build`, ссылки или `K=V` в argv, `--source`/`--reinstall`/`--accept-risk` |
| 65 | `record.invalid`, `record.mismatch` |
| 66 | `record.not_found`, `not_found.params_file` |
| 67 | граф по реестру образа |
| 68 | `target.build` (`targets.build: none`), `platform.*`, `hook.not_applicable` |
| 75 | блокировка |
| 77 | `perm.params_file`: файл не 0600 или не root |
| 79 | `hook.failed` |
