# ClawKangsar Product Requirements Document (PRD)

## 1. Vision
ClawKangsar is a high-performance, native Go spinoff of PicoClaw built for Raspberry Pi deployment. It is a lightweight personal assistant that enables users to control home/server workflows through WhatsApp and Telegram with shared AI memory and minimal runtime bloat.

## 2. Product Goals
- Deliver a Go-native assistant optimized for Raspberry Pi resource limits.
- Keep baseline runtime memory under **20MB RAM** (excluding short-lived browser spikes).
- Support multi-channel communication through WhatsApp and Telegram.
- Provide optional real-time web browsing via a Pi-safe browser tool.
- Preserve a single shared agent processing loop across all channels.

## 3. Primary Use Cases
- Send commands from WhatsApp to control server/home tasks.
- Send commands from Telegram with allow-list security restrictions.
- Ask for up-to-date information that triggers controlled web browsing.
- Continue context-aware conversations across both chat platforms.

## 4. Scope (v1)
### In Scope
- Project identity migration to `ClawKangsar`.
- Dependencies:
  - `go.mau.fi/whatsmeow`
  - `github.com/go-telegram/bot`
  - `github.com/chromedp/chromedp`
- Native WhatsApp gateway with QR terminal pairing + SQLite session persistence.
- Native Telegram gateway with allow-list by Telegram user ID.
- Unified routing into one `Agent.Process` loop.
- Pi-optimized browser tool using Chromium flags:
  - `--headless=new`
  - `--disable-gpu`
  - `--no-sandbox`
  - `--disable-dev-shm-usage`
- Browser inactivity watchdog that kills browser process after 5 minutes idle.
- Config updates for `whatsapp` and `telegram` blocks.
- System prompt update:
  - "You are ClawKangsar, a professional assistant running on a Raspberry Pi. Keep responses concise and use your browser tool only when real-time data is needed."

### Out of Scope (v1)
- Multi-node clustering or distributed session replication.
- Rich media workflows beyond basic message handling.
- Horizontal autoscaling.

## 5. Non-Functional Requirements
- **Memory Efficiency:** Target <20MB baseline RSS on Pi during idle operation.
- **Startup Latency:** Fast cold start suitable for Pi services.
- **Reliability:** Gateway reconnect behavior should tolerate network interruptions.
- **Security:** Telegram access restricted by allow list; WhatsApp pairing only via local QR.
- **Maintainability:** Clear modular boundaries between core, gateways, and tools.

## 6. High-Level Architecture
- `internal/core` (agent loop, memory, routing)
- `internal/gateway/whatsapp` (whatsmeow client, QR pairing, SQLite session)
- `internal/gateway/telegram` (Telegram bot client, allow-list filter)
- `internal/tools/browser.go` (chromedp integration + idle watchdog)
- Shared interface: gateways emit normalized inbound messages to a single processor (`Agent.Process`).

## 7. Configuration Requirements
`config.json` must include:
- `whatsapp` block (device/session settings)
- `telegram` block (bot token + allow list)
- system prompt override with ClawKangsar Pi guidance

## 8. Success Criteria
- Project naming is consistently `ClawKangsar` in module/version/log identity.
- WhatsApp can pair via terminal QR and persist auth in SQLite.
- Telegram rejects non-allow-listed IDs and accepts allow-listed IDs.
- Both channels share memory through the same processing loop.
- Browser tool launches with mandatory Pi flags and auto-terminates after 5 idle minutes.
- Configuration supports both gateways and updated system prompt.
