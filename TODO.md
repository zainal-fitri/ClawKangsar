# ClawKangsar TODO

## Core
- [x] Rename project identity to `ClawKangsar` across `go.mod`, version metadata, and startup logs.
- [x] Add required dependencies:
  - [x] `go.mau.fi/whatsmeow`
  - [x] `github.com/go-telegram/bot`
  - [x] `github.com/chromedp/chromedp`
- [x] Define/confirm a shared inbound message model used by all gateways.
- [x] Ensure WhatsApp and Telegram both route to a single `Agent.Process` loop.
- [x] Add Pi memory validation harness with pass/fail gate (`scripts/pi-memory-check.sh`).
- [ ] Keep baseline idle footprint aligned with <20MB Pi target (run `scripts/pi-memory-check.sh` on Raspberry Pi).

## Gateways
- [x] Create `internal/gateway/whatsapp`.
- [x] Integrate whatsmeow client bootstrap and lifecycle.
- [x] Implement terminal QR rendering for first-time pairing.
- [x] Persist WhatsApp session/auth state in local SQLite DB.
- [x] Create `internal/gateway/telegram`.
- [x] Integrate `go-telegram/bot` polling/webhook (project-appropriate mode).
- [x] Add Telegram allow-list validation by numeric user ID.
- [x] Reject unauthorized Telegram users with safe response/logging.
- [x] Normalize inbound events from both gateways into one router.

## Tools
- [x] Implement `internal/tools/browser.go` with chromedp.
- [x] Enforce Pi-safe Chromium flags:
  - [x] `--headless=new`
  - [x] `--disable-gpu`
  - [x] `--no-sandbox`
  - [x] `--disable-dev-shm-usage`
- [x] Add inactivity watchdog to terminate browser process after 5 minutes idle.
- [x] Expose browser tool API for on-demand real-time queries.
- [x] Update `config.json` with `whatsapp` and `telegram` blocks.
- [x] Add system prompt:
  - [x] "You are ClawKangsar, a professional assistant running on a Raspberry Pi. Keep responses concise and use your browser tool only when real-time data is needed."

## Spinoff Porting
- [x] Clone PicoClaw as local reference (`PicoClaw-reference/`).
- [x] Create source-to-target migration map (`PORTING_MAP.md`).
- [x] Port session persistence patterns from PicoClaw session manager.
- [x] Add lightweight `web_fetch` fallback tool to avoid unnecessary browser launches.
- [x] Add gateway health/status surface for Raspberry Pi ops.
