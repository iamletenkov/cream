# Статус cream

Выпущена **1.4.0** (2026-10-28). Версия и CHANGELOG — через release-please.
Документация: [README.md](README.md); договорённости — [ADR](adr/README.md).

## Сделано
- Ядро `cream/v1`: источники, resolver, install/upgrade/remove, конфигурация,
  действия, system init, фаза build; упаковка `.deb`/`tar.gz`, воспроизводимость.
- OpenWrt: семейство, procd, состояние под `/opt/cream/var`, `make test-openwrt`
  ([руководство](guides/openwrt.md)); bundled cream — [закрытый план](plans/2026-10-21-bisquite-bundled-cream.md).
- `install --allow-downgrade` и журнал `op: downgrade` — выпущено в 1.4.0.
- `upgrade`: план, dry-run, accept-risk по имени, hold, частичный исход
  ([ADR 0003](adr/0003-upgrade-plan-hold.md)).

## В работе
- #57 реализована и проверена; ветка `feat/with-deps-conflict-preflight`, PR #58:
  files всего графа до записи, `install --dry-run`, прежний отказ 67;
  minor через `feat`. [План и результаты](plans/2026-11-14-with-deps-conflict-preflight.md).
- `make ci` зелёный (race, contract-old, amd64/arm64); независимое ревью APPROVED.
- Go поднят до 1.26.11: `make ci` и `make vuln` зелёные (GO-2026-6859 закрыта).
- PVE-приёмка пройдена: конфликт 67 без записей, граф без конфликта 0,
  kiosk 4.1.1 → 5.0.0 с wallpaper 0. VMID 990002 удалена вместе с дисками и ISO.

## Бэклог
- Drop-in стадии обновляется при install/upgrade расширения, не при system init.
- `runner`: Setpgid с stdin: pass может дать SIGTTIN при чтении tty.
- Образы CI и тестов закреплять по digest; CI release-please под GITHUB_TOKEN
  не запускает workflow релизного PR.
