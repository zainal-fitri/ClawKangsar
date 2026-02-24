# ClawKangsar

ClawKangsar is a Go-native, Raspberry Pi-focused spinoff of PicoClaw with multi-channel gateways (WhatsApp + Telegram), shared agent routing, and lightweight web tooling.

## What is implemented
- Native WhatsApp gateway using `whatsmeow` with terminal QR pairing and SQLite session storage.
- Native Telegram gateway using `go-telegram/bot` with allow-list security.
- Unified `Agent.Process` flow shared by both channels.
- Web tools: `web_fetch` (lightweight HTTP fetch, first choice) and `browser` via `chromedp` with Pi flags and idle auto-kill.
- Health endpoints: `/health`, `/ready`, `/status`
- Session persistence for agent message history.

## Requirements
- Go `1.24+`
- Linux/Raspberry Pi for production runtime
- For browser tool on Pi: Chromium installed

## Quick start
```bash
go mod tidy
go build ./...
go run ./cmd/clawkangsar -config config.json
```

## Configuration
Edit `config.json`:
- `telegram.enabled`: set `true` to enable Telegram
- `telegram.token`: your bot token
- `telegram.allow_list`: your allowed numeric Telegram user IDs
- `whatsapp.enabled`: set `true` to enable WhatsApp
- `whatsapp.session_dsn`: SQLite DSN for WhatsApp auth/session state
- `health.enabled`: enable/disable health server
- `storage.session_dir`: persistent session store path

## Runtime behavior
- On first WhatsApp start, QR code is printed in terminal for pairing.
- Telegram rejects users not in `allow_list`.
- `/browse <url>` tries lightweight fetch first, then launches browser fallback.
- `/fetch <url>` uses lightweight fetch only.
- `/status` reports basic agent/session stats.

## Health endpoints
If `health.enabled=true`:
- `GET /health` -> liveness
- `GET /ready` -> readiness
- `GET /status` -> gateway + agent + browser snapshot

Default bind from `config.json`:
- host: `0.0.0.0`
- port: `18080`

## Raspberry Pi memory validation
Run on Raspberry Pi (Linux):
```bash
bash scripts/pi-memory-check.sh --limit-mb 20 --warmup-seconds 15 --sample-seconds 60
```

Exit codes:
- `0`: pass
- `1`: fail (RSS above limit)
- `2`: environment/runtime error

## Git guide (for later)
This folder is currently not a git repo. Initialize and push when ready:

```bash
git init
git branch -M main
git add .
git commit -m "feat: bootstrap ClawKangsar core + gateways + tools"
git remote add origin <your-repo-url>
git push -u origin main
```

If you do not want to version the upstream reference clone, exclude `PicoClaw-reference/` before `git add .` (or add it to `.gitignore`).

## Project docs
- `PRD.md`
- `TODO.md`
- `PORTING_MAP.md`
