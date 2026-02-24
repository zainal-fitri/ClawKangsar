# ClawKangsar Porting Map (from PicoClaw Reference)

## Upstream Snapshot
- Upstream repo: `https://github.com/sipeed/picoclaw.git`
- Local reference clone: `PicoClaw-reference/`
- Pinned commit: `7cbfa89a96059c1e6e6139ebc5c2e305ff4f7746`
- Upstream license: `MIT` (keep attribution when reusing code)

## Porting Strategy
- Keep ClawKangsar as a **clean Go-native implementation**.
- Port **behavior and design patterns** first, then selectively port code where it improves reliability without adding bloat.
- Avoid inheriting broad multi-channel complexity not required for Raspberry Pi + WhatsApp + Telegram scope.

## Source -> Target Map

### Core
- `PicoClaw-reference/pkg/bus/types.go`
  - Port to: `internal/core/message.go`
  - Goal: unified inbound/outbound message model across channels.
  - Status: done (minimal model implemented).

- `PicoClaw-reference/pkg/agent/loop.go`
  - Port to: `internal/core/agent.go`
  - Goal: shared process loop semantics, channel-agnostic routing, response lifecycle.
  - Status: partial (single `Agent.Process` loop done; advanced session summarization/tool-loop pending).

- `PicoClaw-reference/pkg/session/manager.go`
  - Port to: `internal/core/session_store.go`
  - Goal: persistent chat memory with atomic writes and safe session-key filename handling.
  - Status: done (implemented in `internal/core/session_store.go`).

### Gateways
- `PicoClaw-reference/pkg/channels/base.go`
  - Port to: `internal/gateway/*` shared allow-list helper (planned)
  - Goal: centralized allow-list matching behavior.
  - Status: partial (Telegram allow-list implemented; shared helper pending).

- `PicoClaw-reference/pkg/channels/telegram.go`
  - Port to: `internal/gateway/telegram/gateway.go`
  - Goal: robust message parsing, command handling, and outbound send behavior.
  - Status: partial (allow-list + text pipeline done; media/placeholder UX/formatting pending by design).

- `PicoClaw-reference/pkg/channels/whatsapp.go`
  - Port decision: **do not copy implementation**.
  - Reason: upstream WhatsApp relies on a bridge WebSocket; ClawKangsar requires native `whatsmeow`.
  - Status: done (native gateway implemented separately in `internal/gateway/whatsapp/gateway.go`).

- `PicoClaw-reference/pkg/channels/manager.go`
  - Port to: lightweight gateway bootstrap in `cmd/clawkangsar/main.go`
  - Goal: deterministic startup/shutdown and fan-in routing.
  - Status: done for v1 scope (startup orchestration + health/status surface via `/health`, `/ready`, `/status`).

### Tools
- `PicoClaw-reference/pkg/tools/web.go`
  - Port to: `internal/tools/browser.go` + `internal/tools/webfetch.go`
  - Goal: keep fast HTTP fetch fallback and query normalization patterns.
  - Status: done (`web_fetch` added in `internal/tools/webfetch.go` and used before chromedp launch).

## Explicitly Not Porting (to avoid bloat)
- Multi-platform channels not in scope (`discord`, `slack`, `line`, `wecom`, etc.).
- Provider matrix / fallback chain complexity until core gateways stabilize.
- Subagent/spawn orchestration and cron workflows in v1.
- Large workspace/memory scaffolding beyond what is needed for shared channel context.

## Next Priority Backlog for ClawKangsar
1. Add memory compaction/summarization to keep persisted session growth bounded on long-running Pi nodes.
2. Add richer unified command layer (`/show`, `/help`, channel diagnostics) across Telegram and WhatsApp.
3. Add integration tests for gateway allow-list, session persistence, and readiness transitions.
