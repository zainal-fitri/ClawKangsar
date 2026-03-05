# ClawKangsar

ClawKangsar is a Go-native Raspberry Pi assistant with WhatsApp and Telegram gateways, shared chat memory, real LLM replies, and guarded server-control tools.

## Current state
ClawKangsar is ready to build and run.

What works now:
- Telegram gateway with allow-list security
- WhatsApp gateway with QR pairing and SQLite session persistence
- Real LLM replies through `openai_compat` or `codex_oauth`
- Automatic tool-calling for web and server-control tools
- Shared memory across Telegram and WhatsApp
- Health endpoints
- Raspberry Pi browser tool with idle auto-kill

What is intentionally guarded:
- Shell commands are alias-based only
- `systemctl`, `docker logs`, and `journalctl` use explicit allow-lists
- Empty Telegram allow-list rejects everyone

## Requirements
- Raspberry Pi 5 with Raspberry Pi OS or another Linux distribution
- Go `1.24+`
- `git`, `curl`, `build-essential`, `pkg-config`, `libsqlite3-dev`
- Chromium installed for the browser tool
- `npm` only if you want `codex_oauth` login through Codex CLI

## Raspberry Pi install
Run these on the Pi.

### 1. Install system packages
```bash
sudo apt update
sudo apt install -y git curl ca-certificates build-essential pkg-config libsqlite3-dev
sudo apt install -y chromium-browser || sudo apt install -y chromium
```

### 2. Install Go
Use the official Go install method or your package manager, then confirm the version:
```bash
go version
```

ClawKangsar requires Go `1.24+`.

### 3. Clone the repo
```bash
git clone <your-repo-url> ClawKangsar
cd ClawKangsar
```

### 4. Build the binary
```bash
go mod tidy
go build -o clawkangsar ./cmd/clawkangsar
```

## Interactive setup
ClawKangsar now includes an interactive setup wizard.

Run either:
```bash
./clawkangsar setup
```

Or without building first:
```bash
go run ./cmd/clawkangsar setup
```

The wizard asks for:
- starter profile
- Telegram enable/token/allow-list
- WhatsApp enable
- LLM provider
- server tools enable/allow-lists
- health endpoint settings

If `config.json` already exists, the wizard can back it up before overwriting.

Useful flags:
```bash
./clawkangsar setup --config config.json
./clawkangsar setup --profile systemd-first
./clawkangsar setup --profile docker-first --force
./clawkangsar setup --profile home-assistant --config /home/pi/ClawKangsar/config.json
```

## Starter profiles
The wizard and the example profiles support three starter modes.

### `systemd-first`
Use this if most things on the Pi run as system services.

Included by default:
- shell aliases: `uptime`, `disk_free`, `mem_free`, `cpu_temp`
- `systemctl` allow-list: `clawkangsar.service`, `tailscaled.service`, `caddy.service`
- `journalctl` allow-list for the same units

### `docker-first`
Use this if most workloads run in Docker.

Included by default:
- shell aliases: `uptime`, `disk_free`, `mem_free`, `docker_health`
- `systemctl` allow-list: `clawkangsar.service`, `docker.service`
- Docker log allow-list: `homeassistant`, `traefik`, `portainer`
- `journalctl` allow-list: `clawkangsar.service`, `docker.service`

### `home-assistant`
Use this for automation-focused Pi deployments.

Included by default:
- shell aliases: `uptime`, `disk_free`, `mem_free`, `cpu_temp`, `mqtt_check`
- `systemctl` allow-list: `clawkangsar.service`, `home-assistant@homeassistant.service`, `zigbee2mqtt.service`, `mosquitto.service`, `node-red.service`
- Docker log allow-list: `homeassistant`, `zigbee2mqtt`, `mosquitto`, `nodered`
- `journalctl` allow-list for the same Home Assistant units

Reference config files are also included here:
- `configs/profiles/systemd-first.json`
- `configs/profiles/docker-first.json`
- `configs/profiles/home-assistant.json`

## LLM setup
The wizard supports two LLM modes.

### Option 1: `codex_oauth`
Use this if you want the closest thing to OpenClaw-style login flow.

What it does:
- reuses local Codex CLI auth state
- avoids manually pasting API keys into config if you do not want to

Install Codex CLI first:
```bash
npm install -g @openai/codex
```

Then either:
- let the setup wizard run the login flow
- or run it later yourself:
```bash
./clawkangsar auth codex
```

Notes:
- this mode depends on the local Codex CLI auth file
- if `codex` is not installed, choose `openai_compat` instead

### Option 2: `openai_compat`
Use this if you want standard API-key based access.

You can either:
- store the key directly in `config.json`
- or keep the key in an environment variable such as `OPENAI_API_KEY`

Example before running:
```bash
export OPENAI_API_KEY="your-api-key"
```

## Config notes
Even with the wizard, these rules matter.

### Telegram
- `telegram.enabled=true` requires a bot token
- `telegram.allow_list` must contain your numeric Telegram user ID
- if `allow_list` is empty, everyone is rejected

### WhatsApp
- no token is required in config
- on first run, the terminal prints a QR code
- scan it from WhatsApp Linked Devices
- auth/session state is stored in the local SQLite database defined by `whatsapp.session_dsn`

### Browser tool
The browser tool uses Chromium with Pi-safe flags:
- `--headless=new`
- `--disable-gpu`
- `--no-sandbox`
- `--disable-dev-shm-usage`

The browser process is killed after 5 minutes of inactivity.

### Server tools
The security boundary is `config.json`, not the prompt.

Review these fields before enabling them:
- `tools.shell_enabled`
- `tools.shell_commands`
- `tools.systemctl_enabled`
- `tools.systemctl_allow_services`
- `tools.docker_enabled`
- `tools.docker_allow_containers`
- `tools.journal_enabled`
- `tools.journal_allow_units`

Do not expose destructive commands until you intentionally want them.

## Run ClawKangsar
After setup:
```bash
./clawkangsar -config config.json
```

If you are still iterating and do not want to build each time:
```bash
go run ./cmd/clawkangsar -config config.json
```

## First run checklist
After starting the process, verify these items.

### Telegram
- send a message to the bot from your allow-listed account
- if the ID is missing, the bot replies `Unauthorized.` and the process logs the rejected numeric `user_id`

### WhatsApp
- if no session exists yet, the terminal prints a QR code
- after scanning, later runs should restore the previous session automatically

### LLM
- send a normal message, not just `/status`
- if LLM is enabled and configured correctly, the bot should answer normally

### Health endpoints
If health is enabled:
```bash
curl http://127.0.0.1:18080/health
curl http://127.0.0.1:18080/ready
curl http://127.0.0.1:18080/status
```

## Chat commands
Direct commands available now:
```text
/status
/fetch <url>
/browse <url>
/cmd <alias>
/service status <name>
/service start <name>
/service stop <name>
/service restart <name>
/docker ps
/docker logs <container> [lines]
/logs <unit> [lines]
```

The LLM can also call the relevant tools automatically when they are enabled.

## Install as a service
Create `/etc/systemd/system/clawkangsar.service`:

```ini
[Unit]
Description=ClawKangsar
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=pi
WorkingDirectory=/home/pi/ClawKangsar
ExecStart=/home/pi/ClawKangsar/clawkangsar -config /home/pi/ClawKangsar/config.json
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Adjust `User`, `WorkingDirectory`, and `ExecStart` for your system.

Then enable it:
```bash
sudo systemctl daemon-reload
sudo systemctl enable --now clawkangsar
sudo systemctl status clawkangsar
```

If `systemd-first` is your profile, add `clawkangsar.service` to the allow-lists if it is not already there.

## Raspberry Pi memory check
Run this on the Pi after setup and before calling the deployment done:
```bash
bash scripts/pi-memory-check.sh --limit-mb 20 --warmup-seconds 15 --sample-seconds 60
```

Exit codes:
- `0`: pass
- `1`: fail
- `2`: environment/runtime error

## Troubleshooting
### `telegram token is required when telegram.enabled=true`
Your Telegram gateway is enabled but the token is empty.

### Telegram keeps replying `Unauthorized.`
Your numeric Telegram user ID is not in `telegram.allow_list`.

### WhatsApp does not print a QR code
A previous session may already exist in the SQLite DB. Delete the session DB only if you intentionally want to re-pair.

### `codex` command not found
Install Codex CLI:
```bash
npm install -g @openai/codex
```

Or use `openai_compat` instead.

### Browser tool fails on Pi
Make sure Chromium is installed and available as `chromium-browser` or `chromium`.

## Development notes
Useful commands:
```bash
go build ./...
go run ./cmd/clawkangsar setup
go run ./cmd/clawkangsar auth codex
go run ./cmd/clawkangsar -config config.json
```