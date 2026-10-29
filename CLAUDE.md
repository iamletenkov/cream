@AGENTS.md

## Claude Code
- **Субагенты** (`.claude/agents/`, Opus 5.5): `explorer` → `planner` → `skeptic` → реализация → `reviewer`; `test-runner` — прогоны `make`. Большие чтения и длинные прогоны — им: главный контекст дорогой. Отчёт агента — ≤ 30 строк в ответе, полный текст — в `.scratchpad/<тема>/`. Правки и следующий раунд — тому же агенту через SendMessage.
- **Skills** (`.claude/skills/`): `/plan`, `/skeptic`, `/fullcheck`, `/release`. Плагины проекта: `superpowers`, `code-review`, `commit-commands`, `security-guidance`; остальные выключены в `.claude/settings.json`.
- **Правила** (`.claude/rules/`, по путям): `go.md` — Go-код; `docs.md` — `docs/**` и `*.md`.
- **Хуки** — enforcement: деструктивные команды и `--no-verify` блокируются; `Read` > 2 МБ без `limit` блокируется; после правки `.go` — `gofmt`; на старте — git-контекст и `docs/STATUS.md`.
- **MCP** (`.mcp.json` из `.mcp.json.example`): `codebase-memory` — навигация по коду в первую очередь; `knowledge-graph` — память между сессиями (решения, причины багов, поправки владельца); `context7` — документация библиотек.
- **Модель:** Opus 5.5 (`high`, рутина — `medium`; архитектура — `xhigh` в plan mode), выбирается в начале сессии. `/clear` между несвязанными задачами; параллельные исполнители — в своих worktree с непересекающимися файлами.
