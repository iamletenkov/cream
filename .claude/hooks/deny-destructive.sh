#!/usr/bin/env bash
# PreToolUse-хук (Bash): блокирует деструктивные команды и обход git-хуков.
# Контракт: JSON на stdin; блокировка — exit 2 и причина в stderr.
# https://docs.claude.com/en/docs/claude-code/hooks
set -euo pipefail

INPUT="$(cat)"
COMMAND="$(printf '%s' "$INPUT" | jq -r '.tool_input.command // empty' 2>/dev/null || true)"
[ -z "$COMMAND" ] && exit 0

# Каждый паттерн — extended regex.
# shellcheck disable=SC2016  # одинарные кавычки сохраняют литеральный $ в regex
PATTERNS=(
    'rm[[:space:]]+(-[a-zA-Z]*r[a-zA-Z]*f[a-zA-Z]*|-[a-zA-Z]*f[a-zA-Z]*r[a-zA-Z]*)[[:space:]]+(/|~|\$HOME|\*)'
    ':\(\)\{[[:space:]]*:\|:&[[:space:]]*\};:'
    'mkfs\.[a-z0-9]+'
    'dd[[:space:]]+.*of=/dev/(sd|nvme|hd|vd|mmcblk|disk)'
    '>[[:space:]]*/dev/(sd|nvme|hd|vd|mmcblk)'
    'wipefs|blkdiscard'
    'git[[:space:]]+push([[:space:]]+[^[:space:]]+)*[[:space:]]+(--force|--force-with-lease|-[a-zA-Z]*f)([[:space:]]|$)'
    'git[[:space:]]+reset[[:space:]]+--hard'
    'git[[:space:]]+clean[[:space:]]+-[a-zA-Z]*[fxX]'
    'git[[:space:]]+branch[[:space:]]+-D'
    'git[[:space:]]+filter-(branch|repo)'
    '--no-verify([[:space:]]|$)'
    'gh[[:space:]]+repo[[:space:]]+(delete|archive|rename)'
    'gh[[:space:]]+release[[:space:]]+delete'
    'docker[[:space:]]+system[[:space:]]+prune[[:space:]]+.*-a'
    'docker[[:space:]]+volume[[:space:]]+(rm|prune)'
)

for pattern in "${PATTERNS[@]}"; do
    if printf '%s' "$COMMAND" | grep -Eq -e "$pattern"; then
        cat >&2 <<MSG
БЛОКИРОВКА: команда выглядит деструктивной и остановлена хуком.
Команда: $COMMAND
Паттерн: $pattern

Если действие действительно нужно — попроси пользователя выполнить его вручную.
Не переписывай команду ради обхода паттерна. Хук: .claude/hooks/deny-destructive.sh
MSG
        exit 2
    fi
done
exit 0
