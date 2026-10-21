# Стадии configure: когда выполняется настройка на загрузке

Руководство для автора расширения: что такое стадии `early`, `system`,
`cloud-user`, `graphical` хука `configure`, чем они отличаются от фаз build и
live, как выбрать стадию, написать хук и разобраться, почему он не отработал.
Краткая справка по полям — в [манифесте](../reference/manifest.md#стадии-configure),
юниты и пути — в [файлах и окружении](../reference/files-and-environment.md#юниты-systemd).

## Фазы и стадии — разные вещи

**Фаза** — где выполняется операция cream, её видно хуку в `CREAM_PHASE`:

| Фаза | Где | Что происходит с `configure` |
|---|---|---|
| `build` | внутри `virt-customize` при сборке образа ([фаза build](build-phase.md)) | хук **не запускается**: cream только включает юнит ссылкой в `*.wants` и пишет порядок |
| `live` | на работающей машине | хук запускается сразу после `install`/`upgrade`, по `cream set` (если у расширения нет `apply`), по `cream configure` и на каждой загрузке |

**Стадия** (`hooks.configure.stage`) — в какой момент **загрузки** systemd
запускает `configure`. Хук `install` ставит пакеты и раскладывает файлы один
раз; `configure` приводит машину к настройкам расширения **на каждой загрузке**
— например, строит правила файрвола, пишет конфиг под пользователя cloud-init,
запускает VNC поверх экрана входа. Стадии касаются только `configure`: у
остальных хуков поля `stage`, `after`, `before`, `wants` запрещены
(65 `manifest.invalid`).

Как это устроено: у каждого расширения с `configure` есть экземпляр
`cream-configure@<ext>.service` (`Type=oneshot`, `RemainAfterExit=yes`,
`ExecStart=/usr/bin/cream configure --from-unit <ext>`). Стадия решает, в
`.wants` какой цели лежит ссылка на экземпляр и какой порядок cream пишет в
drop-in `/etc/systemd/system/cream-configure@<ext>.service.d/10-stage.conf`.
Сбой одного расширения не останавливает остальные: у каждого свой экземпляр.

## Стадии

| Стадия | Когда на загрузке | Что уже есть | Чего ещё нет | Типичные задачи | В каталоге cream-extensions |
|---|---|---|---|---|---|
| `early` | до сети: после `local-fs.target`, до `network-pre.target` и `sysinit.target` | локальные файловые системы, журнал | сети, D-Bus, большинства служб; пользователя cloud-init | файрвол, правила ядра, то, что должно быть готово раньше любого интерфейса | `firewall` (`after: [nftables.service]`) |
| `system` | в `multi-user.target`, после `network.target` | сеть поднимается (интерфейсы настроены, но адрес и маршрут не гарантированы), системные службы стартуют параллельно | гарантии, что cloud-init закончил; графики | службы, Docker, настройка до менеджера входа (`before: [display-manager.service]`) | `docker` (`after: [docker.service]`), `desktop`, `vino-vnc`, `rustdesk`, `chromium-kiosk` |
| `cloud-user` | после `cloud-final.service`, если cloud-init работает; без cloud-init — вместе с `multi-user.target` | пользователь и userdata cloud-init (при работающем cloud-init) | графики, входа пользователя | настройки в домашнем каталоге пользователя cloud-init, его группы и файлы | `code-server`, `wallpaper`, `jetson-stats` |
| `graphical` | в `graphical.target`, после `multi-user.target` и менеджера входа | экран входа (`display-manager.service`), все системные службы | входа пользователя в сеанс (стадия его не ждёт) | то, что работает **поверх** запущенного менеджера входа: VNC, киоск, стриминг экрана | `x11vnc`, `kiosk`, `selkies`, `pixelflux-tegra`, `pixelflux-v4l2m2m` |

Стадии `early`, `cloud-user`, `graphical` есть с cream 1.1.0: манифест с ними
требует `cream: ">=1.1.0"` (иначе 65 `manifest.invalid`). `system` — с 1.0.

Особенности каждой стадии:

- **`early`.** Экземпляр без зависимостей по умолчанию
  (`DefaultDependencies=no`), загрузка ждёт его до `sysinit.target`. Поэтому
  таймаут `configure` — не больше 2 мин (по умолчанию 2 мин; больший cream
  уменьшает до 2 мин с предупреждением `manifest.early_timeout`). `after` на
  `network.target` или `network-online.target` даёт цикл порядка —
  предупреждение `manifest.early_network`. При выключении машины экземпляр
  останавливается вместе со всеми (`Conflicts=shutdown.target`).
- **`system`.** `network.target` — не «сеть доступна»: адреса и DNS могут
  появиться позже. Нужна сеть — проверяйте её в хуке с ограниченным ожиданием
  либо ставьте настройку так, чтобы она не требовала сети на загрузке.
- **`cloud-user`.** Экземпляры тянет вспомогательный
  `cream-configure-cloud-user.service` из `multi-user.target.wants`; у
  экземпляра `After=cloud-final.service`. При работающем cloud-init стадия
  ждёт `cloud-final`. **Без cloud-init** (не установлен, выключен
  `/etc/cloud/cloud-init.disabled`, нет источника данных) `cloud-final` в
  загрузке нет, и стадия стартует сразу, без порядка относительно служб
  `system` — с cream 1.1.1. cream 1.1.0 при выключенном cloud-init стадию не
  запускал вовсе; `cream system init` из 1.1.1 (его зовёт `postinst` пакета)
  переносит такие ссылки. Ставить или выключать cloud-init после установки
  расширения можно — переустанавливать ничего не нужно. Имя пользователя
  берите библиотекой `cloud-user` каталога: при отсутствии cloud-init она
  возвращает первую обычную учётную запись с домашним каталогом.
- **`graphical`.** Ссылка — в `graphical.target.wants`, поэтому стадия
  выполняется, **только если машина грузится в `graphical.target`**
  (`systemctl get-default`). На сервере с целью по умолчанию
  `multi-user.target` она на загрузке не запустится. На машине с
  `graphical.target`, но без менеджера входа стадия ждёт `multi-user.target`.
  `graphical` не ждёт входа пользователя в сеанс: нужен сеанс — ждите его в
  хуке с ограничением или запускайте свою пользовательскую службу.

## Как выбрать стадию

```
Должно сработать до того, как поднимется хоть один сетевой интерфейс?
├─ да → early (≤ 2 мин, без сети, без D-Bus)
└─ нет → Нужен пользователь или userdata cloud-init?
         ├─ да → cloud-user
         └─ нет → Работает поверх запущенного экрана входа (VNC, киоск, захват экрана)?
                  ├─ да → graphical (машина должна грузиться в graphical.target)
                  └─ нет → system
                           ├─ менеджер входа должен застать настройку готовой
                           │  (автологин, сеанс по умолчанию) → before: [display-manager.service]
                           └─ зависит от своей службы → after: [<служба>.service]
```

Чего не делать:

- Не эмулировать `graphical` через `system` с `after: [display-manager.service]`:
  `multi-user.target` начнёт тянуть менеджер входа раньше себя.
- Не ставить `system` с `after: [cloud-final.service]`: `cloud-final` сам
  `After=multi-user.target` — цикл порядка. Для этого есть `cloud-user`.
  Допустимо `after: [cloud-init.service]` (сеть и пользователь уже созданы,
  `cloud-final` ещё нет), как у `desktop`.
- Не ставить `early` с `after: [network.target]` или `network-online.target` — цикл.

## Пример

`extension.yaml`:

```yaml
schema: cream/v1
name: greeter
version: 1.0.0
summary: Приветствие пользователя cloud-init
cream: ">=1.1.0"          # стадия cloud-user есть с 1.1.0
platform: {family: deb, arch: [amd64, arm64]}
depends:
  - {name: cloud-user, version: ">=1.0.0 <2"}
hooks:
  install: {timeout: 5m}
  configure:
    stage: cloud-user
    timeout: 1m           # по умолчанию 15 мин; у early — 2 мин и не больше
config:
  keys:
    GREETING: {type: str, default: "Привет", description: "текст приветствия"}
```

`hooks/configure`:

```bash
#!/usr/bin/env bash
# Каждая загрузка, после install/upgrade на живой машине и после cream set
# (своего apply нет — cream перезапускает cream-configure@greeter).
set -euo pipefail
echo "configure: причина $CREAM_REASON, фаза $CREAM_PHASE"

# Пользователь cloud-init. Код 1 — userdata ещё нет: подождать ограниченно.
user=""
for _ in $(seq 1 30); do
    user="$("${CREAM_ROOT:-}/opt/cream/ext/cloud-user/get_cloud_user.sh" 2>/dev/null)" && break
    sleep 1
done
if [ -z "$user" ]; then
    echo "пользователь не найден — пропускаю" >&2
    exit 0                       # не сбой: на машине без пользователя делать нечего
fi

home="$(getent passwd "$user" | cut -d: -f6)"
want="$GREETING"
# Идемпотентно: пишем, только если содержимое другое.
if [ "$(cat "$home/.greeting" 2>/dev/null)" != "$want" ]; then
    printf '%s\n' "$want" > "$home/.greeting.tmp"
    chown "$user:" "$home/.greeting.tmp"
    mv "$home/.greeting.tmp" "$home/.greeting"
fi
```

Проверка без root — `cream validate` по каталогу расширения; полный цикл —
`cream --root` ([написать расширение](write-extension.md)).

## Что получает хук

- **Окружение** — как у всех хуков ([окружение хуков](../reference/files-and-environment.md)):
  `CREAM_PHASE=live`, `CREAM_EXT`, `CREAM_EXT_DIR`, `CREAM_STATE_DIR`
  (`/var/lib/cream/ext/<ext>`), ключи настроек; секреты — из
  `$CREAM_SECRETS_FILE`. `PATH` и `LANG` фиксированные.
- **`CREAM_REASON`** — почему запущен: `boot` (загрузка; на первой загрузке
  после сборки образа — тоже `boot`), `install`, `upgrade` (сразу после
  операции на живой машине), `apply` (`cream set` без хука `apply` и ручной
  `cream configure`). Причину cream передаёт юниту маркером
  `/run/cream/<ext>.reason` и удаляет его при чтении; нет маркера — `boot`.
- **Вывод** stdout и stderr уходит в журнал юнита, известные значения секретов
  заменяются на `***`.

**Важно:** после `install`/`upgrade` на живой машине `configure` запускается
**сразу, какой бы ни была стадия**. Хук `graphical` может застать машину без
экрана, хук `cloud-user` — без пользователя: такие случаи хук обрабатывает сам
(ограниченное ожидание, сообщение и выход 0).

### Коды возврата `configure`

| Код хука | Что делает cream | Что видно |
|---|---|---|
| 0 | успех | юнит `active (exited)` |
| 10 | успех, нужна перезагрузка: `reboot_required` в реестре, `/run/reboot-required` | `cream list` — «нужна перезагрузка» |
| 20 | «неприменимо здесь» — для `configure` это **сбой**: cream выходит с 68 | юнит `failed` |
| другое, сигнал, таймаут | сбой: cream выходит с 79 `hook.failed`, запись `configure` со статусом `failed` в `history.jsonl` | юнит `failed`, `systemctl --failed` |

Сбой `configure` не меняет запись реестра: расширение остаётся `installed`.
cream сам **не повторяет** упавший `configure` — следующая попытка будет на
следующей загрузке или по команде (см. ниже). Если условие «неприменимо»
нормально для машины (нет экрана, нет пользователя), выходите с 0 и пишите
сообщение, а не 20.

### Повтор и идемпотентность

Маркера «выполнено» у `configure` нет: юнит выполняется один раз за загрузку
(`RemainAfterExit=yes`) и снова — на каждой следующей, а между загрузками — при
каждом `install`, `upgrade`, `set` и `cream configure`. Поэтому хук обязан быть
**идемпотентным**: повтор с теми же настройками не меняет систему, не
перезаписывает резервные копии, не дублирует строки. Нужно сделать что-то
**один раз** (первая загрузка после сборки образа) — заведите свой маркер в
`$CREAM_STATE_DIR`, он переживает перезагрузку и удаляется с `remove --purge`.

Хук держит блокировку `lock/<ext>`. Синхронно перезапускать из `configure`
юнит, который сам зовёт `cream run` этого расширения, нельзя — взаимная
блокировка; используйте `systemctl --no-block`. На стадии `early` синхронный
`systemctl start` чего-то, что стоит после `sysinit.target`, тоже повесит
загрузку до таймаута.

## Отладка

```bash
# стадия и цель расширения
jq .configure /var/lib/cream/installed/<ext>.json      # {"stage": "...", "target": "..."}
cat /etc/systemd/system/cream-configure@<ext>.service.d/10-stage.conf
ls -l /etc/systemd/system/*.wants/cream-configure@*   # куда включены экземпляры

# выполнился ли и что напечатал
systemctl status cream-configure@<ext>.service
journalctl -u cream-configure@<ext>.service -b --no-pager
journalctl -u cream-configure-cloud-user.service -b --no-pager   # стадия cloud-user
systemctl --failed
tail -n 20 /var/lib/cream/history.jsonl | jq -c 'select(.op == "configure")'

# порядок на загрузке
systemd-analyze critical-chain cream-configure@<ext>.service
journalctl -b | grep -i 'ordering cycle'
systemctl get-default                       # для graphical — должно быть graphical.target
```

Перезапустить `configure` вручную:

```bash
sudo cream configure --wait <ext>    # CREAM_REASON=apply, ждать результата; код 79 — хук упал
sudo cream configure <ext>           # то же без ожидания: перезапуск ставится в очередь
sudo systemctl restart cream-configure@<ext>.service   # как на загрузке: CREAM_REASON=boot
```

`cream configure` при работающем systemd перезапускает экземпляр
`cream-configure@<ext>`; без systemd (контейнер) запускает хук сам. Пока
systemd ещё загружается, `--wait` не ждёт, а ставит перезапуск в очередь с
предупреждением `configure.no_block`. Отдельной команды «перезапустить всю
стадию» нет: перезапускайте экземпляры по одному. `cream configure --from-unit`
вызывает только сам юнит — руками его не зовите.

Стадию меняют в манифесте и выпускают новую версию: drop-in и ссылку cream
переписывает при `install`/`upgrade` расширения. `sudo cream system init`
пересчитывает цели ссылок по реестру (например, после обновления cream с 1.1.0).

## Частые ошибки

| Симптом | Причина | Что сделать |
|---|---|---|
| `graphical` не запускается на загрузке | цель по умолчанию — `multi-user.target` | `systemctl get-default`; стадия для серверов — `system` |
| в журнале `ordering cycle`, загрузка медленная | `system` с `after: [cloud-final.service]` или `early` с `after: [network.target]` | `cloud-user` вместо первого; убрать сеть из `after` у `early` (`manifest.early_network`) |
| `early` обрывается через 2 мин | таймаут стадии не больше 2 мин (`manifest.early_timeout`) | тяжёлое — в `install`, на загрузке только применять |
| `cloud-user` не находит пользователя | userdata ещё нет или cloud-init выключен и обычной учётной записи нет | ограниченное ожидание в хуке, выход 0 с сообщением |
| `configure` на cream 1.1.0 при выключенном cloud-init не запускается | так было до 1.1.1 | обновить cream до 1.1.1+ (`postinst` вызывает `cream system init`) |
| после `install` хук упал «нет экрана/пользователя» | `configure` на живой машине выполняется сразу, а не на своей стадии | обрабатывать отсутствие и выходить с 0 |
| юнит `failed` с 68 | хук вернул 20 | для `configure` 20 — сбой; «неприменимо» — выход 0 с сообщением |
| повтор портит файлы, дублирует строки | хук не идемпотентен | сравнивать с желаемым и писать атомарно, свой маркер — в `$CREAM_STATE_DIR` |
| загрузка висит на `configure` | синхронный `systemctl restart/start` из хука | `systemctl --no-block` |
