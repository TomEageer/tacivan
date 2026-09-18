# Changelog

## 0.1.0 — 2026-09-18

First public release.

- MySQL, PostgreSQL, SQLite, Redis
- Streaming result sets with chunk cache, disk spill and a global memory budget
- Progress reporting with elapsed time and a Stop button that cancels the network read
- MySQL metadata via `SHOW` only; hand-picked database lists; protocol compression
- Connection groups with drag-and-drop, favourites, whole-database search
- Table designer, data grid editing, export, SQL editor with drafts auto-recovered after a crash
- Plugin connections: stdio JSON-RPC, bidirectional, host-enforced read-only rules, custom node status and actions
- MCP server connections: stdio and Streamable HTTP, tools and resources, form-driven calls
- Chinese and English, including the native macOS menu bar
- Universal macOS binary, ad-hoc signed

### Known limitations

- macOS only; Windows and Linux are unbuilt and untested
- Not notarised: first launch needs right-click → Open
- No import wizard, scheduled backup, ER diagram, data dictionary or visual EXPLAIN yet
- Plugin SQL guard is a lexer-level check (SELECT/WITH prefix, single statement, LIMIT clamp, deny list), not a full parser
