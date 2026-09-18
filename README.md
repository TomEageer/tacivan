<div align="center">

<img src="assets/icon_1024.png" width="120" alt="Tacivan icon">

# Tacivan

**A fast, open-source database client for macOS — built for people who live in MySQL all day**

Streams million-row results without freezing,<br>
opens a 3,000-database MySQL 5.7 instance in seconds, and stays out of your way.<br>
Go + native WebView, no Electron, no account, no telemetry.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/TomEageer/tacivan?color=brightgreen&label=release)](https://github.com/TomEageer/tacivan/releases/latest)
[![Download](https://img.shields.io/badge/download-22%20MB%20dmg-brightgreen)](https://github.com/TomEageer/tacivan/releases/latest)
[![Downloads](https://img.shields.io/github/downloads/TomEageer/tacivan/total?color=brightgreen&label=downloads)](https://github.com/TomEageer/tacivan/releases)
[![Telemetry](https://img.shields.io/badge/telemetry-none-success)](#privacy)
[![Platform](https://img.shields.io/badge/platform-macOS%2013%2B%20·%20universal-lightgrey)](#requirements)

[**⬇ Download**](https://github.com/TomEageer/tacivan/releases/latest) · [Quick start](#quick-start) · [How it works](#how-it-works) · [Plugins](docs/plugins.md) · [FAQ](#faq) · [**中文文档**](README.zh-CN.md)

</div>

---

## Why Tacivan

Most GUI clients are either a Java app that takes four seconds to open a menu, or a paid tool that locks half its features behind a licence. Tacivan is what I wanted on my own desk:

- 🚀 **Never blocks on a slow query** — results stream through a chunked cache with a global memory budget and disk spill; a stalled 87-second fetch on one tab does not delay a 2-row table on another
- 🐬 **Made for big, old MySQL** — all metadata goes through `SHOW …`, never `information_schema` (which is a temp-table scan on 5.7). A 3,165-database instance lists in one round trip; you can also hand-pick the databases a connection shows
- ⏱ **Progress you can see and stop** — every long operation reports its stage and elapsed time, and has a Stop button that actually cancels the network read
- 🔌 **Plugin connections** — any language, one process, JSON-RPC over stdio. A plugin becomes a new connection type with its own fields, tree, context-menu actions and a host-enforced read-only guard. Company-specific gateways stay in your private plugin, not in this repo
- 🤖 **MCP servers as connections** — stdio or Streamable HTTP; browse tools and resources, call a tool from a form generated from its JSON Schema, tabular results land in the grid
- 🔎 **Search a whole database** for a value across every table, with per-engine escaping
- 🌍 **Chinese and English** — UI *and* the native macOS menu bar follow one setting
- 🔐 **Passwords in the Keychain**, drafts auto-recovered after a crash, connection groups with drag-and-drop
- 🆓 **MIT**, one binary, no sign-in

|  | Tacivan | DBeaver CE | TablePlus |
|---|---|---|---|
| Price | **Free (MIT)** | Free (Apache-2.0) | Free tier · paid |
| Source | **Open** | Open | Closed |
| Runtime | **Go + native WebView** | Java / SWT | Native |
| Engines | MySQL · PostgreSQL · SQLite · Redis · **plugins** · **MCP** | Many (JDBC) | Many |
| Extend with your own connection type | **Yes — process plugin, any language** | Java plugin | No |
| Talk to MCP servers | **Yes** | No | No |

Feature breadth is not the point: the established clients support far more engines. Tacivan is the one that opens fast, streams everything, and lets you bolt on the weird internal gateway your company makes you use.

## Quick start

1. **[Download the DMG](https://github.com/TomEageer/tacivan/releases/latest)**, drag `Tacivan.app` into **Applications**
2. First launch: **right-click → Open** (the build is ad-hoc signed, not notarised)
3. **Connection → New**, pick an engine, fill in host / user / password (stored in the macOS Keychain)
4. Double-click a table. That is it.

<details>
<summary><b>Build from source</b></summary>

Requires Go 1.26+, Node 20+, Xcode command-line tools and the Wails CLI (`go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0`).

```bash
git clone https://github.com/TomEageer/tacivan.git && cd tacivan
scripts/vendor.sh          # go mod vendor + the macOS menu patch in patches/
scripts/install.sh         # universal build → /Applications/Tacivan.app
scripts/package.sh         # same, plus dist/Tacivan-<version>.dmg
```

`wails dev` gives hot reload for the frontend.

</details>

## How it works

```
Vue 3 + CodeMirror 6 (frontend/)          ← WebView, i18n keyed on the Chinese source strings
        │  Wails bindings
internal/app        orchestration: tabs, progress events, cancel tokens, search, drafts
internal/rs         streaming result sets: chunk cache · disk spill · global memory budget
internal/dbx        engine-neutral model + Dialect (quoting, SELECT/COUNT/DDL generation)
internal/driver     mysqldrv · pgdrv · sqlitedrv · redisdrv · plugindrv · mcpdrv
internal/plugin     stdio JSON-RPC 2.0, bidirectional (plugins may call host.* for read-only metadata)
internal/mcp        MCP client, protocol 2025-06-18, stdio + Streamable HTTP
internal/store      connections, groups, favourites, drafts, settings; secrets → Keychain
```

- **Result sets** are cursors, not arrays. A tab holds a window of chunks; the rest is evicted to disk under a process-wide budget, so ten open million-row tabs still fit in memory. Stats are read from atomics so the UI never waits on a fetch lock
- **Cancellation** goes all the way down: a Stop button cancels the driver context, which interrupts the blocked socket read
- **Plugins** are separate processes. The host enforces `selectOnly` / `singleStatement` / `forceLimit` / `denyPatterns` before SQL reaches the plugin, and a read-only plugin simply has no Exec path. See [docs/plugins.md](docs/plugins.md)
- **MCP** connections are the same tree: server → tools / resources → double-click to call. See [docs/mcp.md](docs/mcp.md)
- **Wails** is used unmodified except for one macOS patch (menu localisation, a HIG Settings… item, title-bar double-click) kept in [patches/](patches/README.md)

## Privacy

No telemetry, no crash reporting, no update checks, no account. The app talks only to the database hosts, plugin processes and MCP servers you configure. Passwords live in the macOS Keychain; everything else is plain JSON under `~/Library/Application Support/Tacivan/`.

## FAQ

**Can it be my only database client?**
For MySQL / PostgreSQL / SQLite / Redis daily work: browsing, editing data, designing tables, running SQL, exporting, searching — yes. Not (yet): import wizards, scheduled backups, ER diagrams, data dictionary, visual EXPLAIN. Deliberately never: stored-procedure debugger, dashboards, an in-app scheduler.

**Windows / Linux?**
Wails can target both, but Tacivan is only built and tested on macOS today. The vendored patch is macOS-only and compiles out elsewhere; everything else is portable Go and TypeScript. PRs welcome.

**Why not Rust / Tauri / Electron?**
Go gives one static binary, first-class database drivers, and a result-set engine that was easy to make cancellable and memory-bounded. The WebView is the system one, so the app is 22 MB, not 200.

**Where do plugins go?**
`~/Library/Application Support/Tacivan/plugins/<id>/` via **Settings → Plugins → Import**. They are never part of a release build; the packaging script refuses to ship one.

## Requirements

macOS 13 or later, Apple Silicon or Intel (universal binary).

## Support

Free and open source with no paid tier. If it saved you a licence, see [DONATE.md](DONATE.md).

## Changelog

See [CHANGELOG.md](CHANGELOG.md).

## License

[MIT](LICENSE). Engine logos are used for identification only: the MySQL dolphin is from [devicon](https://github.com/devicons/devicon) (MIT), the others from [simple-icons](https://github.com/simple-icons/simple-icons) (CC0 1.0); all marks belong to their owners.

<sub>database client · MySQL GUI · PostgreSQL GUI · SQLite · Redis · TablePlus alternative · DBeaver alternative · MCP client · macOS · Go · Wails · Vue</sub>
