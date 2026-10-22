# PoC: узел Teleport на OpenWrt 25.12 с glibc-рантаймом (вариант A)

Дата: 2026-10-22. Проверка осуществимости варианта A спеки [2026-10-20-openwrt.md](../specs/2026-10-20-openwrt.md)
(§3, решения В3 и Р2). Код продуктов не менялся; ВМ, файлы на PVE, токен и узел на стенде удалены.

## Итог
**Вариант A пригоден.** Бинарь форка 18.10.0 после `patchelf` стартует на OpenWrt (musl), регистрируется в кластере
через прокси обратным туннелем, переживает перезагрузку (procd поднимает узел без нового секрета), NSS glibc без
`/etc/nsswitch.conf` разрешает и пользователей, и DNS. Вход `tsh ssh` (команда, PTY, scp, `-L`, запись сессии)
проверен отдельным прогоном — раздел «tsh ssh».

## Стенд проверки
| Что | Значение |
|---|---|
| Образ | `openwrt-builder` v0.3.1, `openwrt-25.12.6-custom-x86-64-generic-ext4-combined-efi.qcow2`, sha256 сверен с `SHA256SUMS` |
| ВМ | PVE, VMID 990006, 2 vCPU, **1 ГБ ОЗУ**, диск 4 ГБ (корень вырос автоматически до 3,9 ГБ), `vmbr0`, DHCP, ключ через cloud-init Proxmox (`wrt.cloudinit`) |
| Teleport | тарбол форка `teleport-v18.10.0-linux-amd64-bin.tar.gz` с зеркала Stvor, sha256 = пин `TELEPORT_TARBALL_SHA256` |
| glibc | Debian 12, `libc6 2.36-9+deb12u14` |
| Кластер | стенд Stvor: временный токен `node` с TTL 30 мин (удалён сразу после join), узел удалён `tctl rm node/<uuid>` |

## Сборка набора (в контейнере `debian:12`, не на OpenWrt)
- `NEEDED` у `teleport`: `libresolv.so.2`, `libdl.so.2`, `libpthread.so.0`, `libm.so.6`, `libgcc_s.so.1`, `libc.so.6`,
  `ld-linux-x86-64.so.2`; у `fdpass-teleport` — `libgcc_s.so.1`, `libc.so.6`.
- В `/opt/teleport/lib`: эти 7 файлов + `libnss_files.so.2`, `libnss_dns.so.2` (в glibc 2.36 это заглушки — `files`/`dns`
  встроены в `libc.so.6`; класть не обязательно).
- `patchelf --set-interpreter /opt/teleport/lib/ld-linux-x86-64.so.2 --set-rpath /opt/teleport/lib` на 386-МБ Go-бинаре
  отработал за секунды, `RUNPATH` записан, бинарь на хосте glibc ожидаемо не стартует (нет такого загрузчика).

## Размеры и ресурсы
| Что | Размер |
|---|---|
| `teleport` + `fdpass-teleport` после patchelf | 369 МБ |
| glibc-рантайм (`/opt/teleport/lib`) | **3,2 МБ** |
| Набор `tar.gz` (оба бинаря + рантайм) | 99 МБ (103 516 556 байт) |
| Корень ВМ после установки | 520 МБ занято из 3,9 ГБ |
| `/opt/teleport/data` после join | 76 КБ; `/opt/teleport/tmp` пуст |
| RSS `teleport` (узел, простаивает) | 180–186 МБ; в ВМ доступно 824 МБ из 1 ГБ — **1 ГБ хватает**, 2 ГБ не замерял |

## Что проверено
| Проверка | Результат |
|---|---|
| `teleport version` | `Teleport v18.10.0 git: go1.25.11`; `fdpass-teleport` — 18.10.0 |
| Join через прокси `:443` по токену | успех за < 1 с, «Service is starting in tunnel mode», keepalive туннеля |
| `tctl nodes ls` | `owrt-glibc-poc`, метка `poc=openwrt-glibc`, 18.10.0 |
| `/proc/<pid>/exe` | `/opt/teleport/bin/teleport` — повторный запуск себя для сессий не сломан (в отличие от `ld.so --library-path`) |
| Перезагрузка | procd (`/etc/init.d/teleport`, `START=95`, `respawn`) поднял узел, туннель поднят без токена (`/opt/teleport/data`) |
| NSS: `getpwnam`/`getgrnam` | glibc `getent passwd root` / `passwd 0` / `group root` на musl-`/etc/passwd` — верно (`/root`, `/bin/ash`) |
| NSS: DNS | `getent hosts`/`ahosts` и сам join по имени прокси — работает через `/etc/resolv.conf` (dnsmasq) |
| `/etc/nsswitch.conf` | **не нужен**: умолчания glibc (`passwd: files`, `hosts: dns files`) подходят; с файлом — то же |
| CA | Go берёт `/etc/ssl/certs/ca-certificates.crt` из пакета `ca-bundle` образа — TLS к прокси проверен без `--insecure` |
| `TMPDIR` | `TMPDIR=/opt/teleport/tmp` через `procd_set_param env` (Р2) — у процесса задан |

## Подводные камни
1. **`procd_set_param stdout/stderr 1` подгружает musl в glibc-процесс.** procd выставляет
   `LD_PRELOAD=/lib/libsetlbf.so` (musl), и в карте процесса оказываются обе libc (`/lib/libc.so` и
   `/opt/teleport/lib/libc.so.6`). Узел работал, но это неопределённое поведение. Решение: в init-скрипте **не**
   ставить `stdout`/`stderr`, а в `teleport.yaml` — `log.output: syslog` (пишет в `logread`; после правки musl в карте
   нет, `LD_*` в окружении нет). Это требование к `openwrt/teleport` и к валидатору расширения.
2. `/var/run/utmp` на OpenWrt нет, `/var/log/wtmp` есть, но на tmpfs: учёт входов Teleport будет пустым/теряться
   при reboot — некритично, ожидать предупреждений в журнале при SSH-сессиях.
3. Локали нет (`LANG` пуст) — для узла не нужна; в сессиях `tsh ssh` будет `C`/POSIX.
4. uid/gid: на OpenWrt только `root` с shell (`/bin/ash`); `os_logins` = `root`, как в спеке; учётки создавать не нужно.
5. В `teleport.yaml` нужны `pam.enabled: false` и `enhanced_recording.enabled: false` явно (PAM и BPF на OpenWrt нет).
6. `libnss_*` в рантайм класть не обязательно (glibc ≥ 2.34), итог — 7 библиотек, как в спеке.

## Не проверено
- ~~`tsh ssh root@узел`~~ — проверено 2026-10-22 (раздел «tsh ssh»; первый прогон отклонил классификатор прав,
  повторный — с разрешения владельца на временные `owrt-*` на стенде).
- arm64 и Raspberry Pi; поведение под нагрузкой SSH-сессий, `bound_keypair`.

## tsh ssh (Task 1 плана, 2026-10-22)
**Итог: зелёный, вариант A подтверждён целиком.** Свежая ВМ PVE 990006 (тот же образ v0.3.1, sha256 сверен,
1 ГБ ОЗУ, OVMF, сеть и ключ — `wrt.cloudinit`), набор собран заново по рецепту выше (debian:12, `libc6
2.36-9+deb12u14`, patchelf, 7 библиотек; тарбол сверен с пином `TELEPORT_TARBALL_SHA256`). `teleport.yaml`:
`log.output: syslog`, `pam`/`enhanced_recording` выключены, метка `owrt=poc`; init — procd без `stdout/stderr`,
`TMPDIR=/opt/teleport/tmp`. На стенде: токен `node` 30 мин (удалён сразу после join), временная роль `owrt-ssh`
(`logins: [root]`, `node_labels: {owrt: poc}`, локальный `-L` разрешён), локальный пользователь `owrt-ssh-<дата>-<rand>`,
identity `tctl auth sign --format=file --ttl=30m` (без браузера).

| Проверка | Результат |
|---|---|
| `tsh ls` | узел `owrt-poc-20261022`, `⟵ Tunnel`, `owrt=poc` |
| `tsh ssh root@… 'uname -a; id; echo $SHELL; cat /etc/openwrt_release'` | `Linux owrt-poc-tsh 6.12.112 … x86_64`, `uid=0(root) gid=0(root)`, `/bin/ash`, OpenWrt 25.12.6 r33311 |
| Интерактивная сессия с PTY | баннер OpenWrt, приглашение `root@owrt-poc-tsh:~#`, `[ -t 0 ]` — истина, `/dev/pts/0`, `TERM=xterm`, `exit` закрывает сессию; `tty`/`stty` в busybox образа нет — размер окна не замерен |
| `tsh scp` туда и обратно | 5 МБ на узел (sha256 совпал), `/etc/openwrt_release` с узла; в аудите — события `sftp` |
| `tsh ssh -N -L 18080:127.0.0.1:80` | uhttpd узла отвечает через туннель (HTTP 307 LuCI); в аудите — `port.local` |
| Запись сессии (режим `node`) | `tctl recordings ls` — 4 записи пользователя на узле; `session.start/end/leave`, `session.data`, `session.upload`; на узле `data/log` после выгрузки пуст, `data` = 84 КБ |
| Карта процессов во время сессии | `teleport start` и дочерний `teleport exec`: `/lib/libc.so` (musl) — 0, `libc.so.6` рантайма — есть, `LD_*` в окружении — нет |
| `logread -e teleport` | 99 строк, все `INFO`; ни одного `WARN`/`ERROR`, ошибок utmp/wtmp/PAM нет |
| Ресурсы | RSS `teleport` ≈ 200 МБ после сессий; доступно ≈ 690 МБ из 1 ГБ |

Наблюдения:
1. **Клиенту через Traefik нужен `TELEPORT_TLS_ROUTING_CONN_UPGRADE=true`** (как в гейтах Stvor): без него `tsh`
   с identity-файлом падает `tls: internal error` на gRPC прокси. К узлу OpenWrt отношения не имеет.
2. После `tsh ssh -N -L` и завершения клиента дочерний `teleport networking` жил на узле ещё > 1 мин (до остановки
   службы). Скорее всего штатное поведение Teleport (процесс на соединение), но сверить с узлом Debian в приёмке
   (Task 13); на 1 ГБ это ≈ лишний процесс, не утечка памяти по наблюдению.
3. Пакета `coreutils-stty` в образе нет — интерактивной работе не мешает; для отладки размера окна — `opkg/apk add`.

Следы прогона: пользователь, роль `owrt-ssh`, токен и узел удалены (`tctl users ls`/`nodes ls`/`tokens ls`/
`get roles` — `owrt` не найдено), identity-файл и профиль `tsh` удалены, ВМ 990006 и файлы на PVE удалены.
Остались записи аудита и сессий, запись инвентаря экземпляра (уходит по TTL).

## Следы на стенде
Токен удалён сразу после join, узел удалён (`tctl nodes ls` — пусто). Остались: записи журнала аудита Teleport
(создание/удаление токена, join, удаление узла) и запись инвентаря экземпляра `owrt-glibc-poc`
(`tctl inventory ls`), которая удаляется Teleport сама по истечении TTL и `tctl rm` не поддерживает.

## Рекомендация
Оставить A основным путём (В3) и снять с него риск №1 спеки до уровня «проверить вход по SSH»: набор строится
`patchelf` в контейнере Debian 12, рантайм 3,2 МБ, 1 ГБ ОЗУ достаточно. В `openwrt/teleport` закрепить:
procd без `stdout/stderr` + `log.output: syslog`, `TMPDIR` на `/opt`, `pam`/`enhanced_recording` выключены явно.
Первым шагом среза 2 — `tsh ssh` и `os_logins` на этом же наборе.
