# PoC: узел Teleport на OpenWrt 25.12 с glibc-рантаймом (вариант A)

Дата: 2026-10-22. Проверка осуществимости варианта A спеки [2026-10-20-openwrt.md](../specs/2026-10-20-openwrt.md)
(§3, решения В3 и Р2). Код продуктов не менялся; ВМ, файлы на PVE, токен и узел на стенде удалены.

## Итог
**Вариант A пригоден.** Бинарь форка 18.10.0 после `patchelf` стартует на OpenWrt (musl), регистрируется в кластере
через прокси обратным туннелем, переживает перезагрузку (procd поднимает узел без нового секрета), NSS glibc без
`/etc/nsswitch.conf` разрешает и пользователей, и DNS. Не проверен только вход `tsh ssh` (см. «Не проверено»).

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
- **`tsh ssh root@узел`.** Для входа нужен пользователь стенда с правом на узел без метки `space`; создание временного
  пользователя `tctl users add … --roles=access --logins=root` и выпуск ему identity отклонил классификатор
  прав (Permission Grant) — по условию задачи остановился, ничего не создано. Проверить в срезе 2 спеки (вход на
  узел с меткой `space` штатным пользователем Stvor, либо одноразовым кластером в docker).
- arm64 и Raspberry Pi; поведение под нагрузкой SSH-сессий (дочерние `teleport exec`), `bound_keypair`.

## Следы на стенде
Токен удалён сразу после join, узел удалён (`tctl nodes ls` — пусто). Остались: записи журнала аудита Teleport
(создание/удаление токена, join, удаление узла) и запись инвентаря экземпляра `owrt-glibc-poc`
(`tctl inventory ls`), которая удаляется Teleport сама по истечении TTL и `tctl rm` не поддерживает.

## Рекомендация
Оставить A основным путём (В3) и снять с него риск №1 спеки до уровня «проверить вход по SSH»: набор строится
`patchelf` в контейнере Debian 12, рантайм 3,2 МБ, 1 ГБ ОЗУ достаточно. В `openwrt/teleport` закрепить:
procd без `stdout/stderr` + `log.output: syslog`, `TMPDIR` на `/opt`, `pam`/`enhanced_recording` выключены явно.
Первым шагом среза 2 — `tsh ssh` и `os_logins` на этом же наборе.
