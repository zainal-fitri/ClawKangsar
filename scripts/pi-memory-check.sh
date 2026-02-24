#!/usr/bin/env bash
set -euo pipefail

LIMIT_MB=20
WARMUP_SECONDS=10
SAMPLE_SECONDS=45
TMP_DIR=""
KEEP_ARTIFACTS=0

usage() {
  cat <<'EOF'
Usage: scripts/pi-memory-check.sh [options]

Options:
  --limit-mb <int>         RSS limit in MB (default: 20)
  --warmup-seconds <int>   Warmup time before sampling (default: 10)
  --sample-seconds <int>   Sampling duration in seconds (default: 45)
  --keep-artifacts         Keep temporary binary/config after run
  -h, --help               Show help

Pass criteria:
  Peak VmRSS during sampling <= limit-mb.
Exit codes:
  0 = pass, 1 = fail, 2 = runtime/setup error.
EOF
}

while (($#)); do
  case "$1" in
    --limit-mb)
      LIMIT_MB="${2:-}"
      shift 2
      ;;
    --warmup-seconds)
      WARMUP_SECONDS="${2:-}"
      shift 2
      ;;
    --sample-seconds)
      SAMPLE_SECONDS="${2:-}"
      shift 2
      ;;
    --keep-artifacts)
      KEEP_ARTIFACTS=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage
      exit 2
      ;;
  esac
done

if ! command -v go >/dev/null 2>&1; then
  echo "ERROR: go is required but not found on PATH." >&2
  exit 2
fi

if [[ ! -r /proc/meminfo ]]; then
  echo "ERROR: /proc is unavailable. Run this script on Linux (Raspberry Pi)." >&2
  exit 2
fi

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/clawkangsar-memcheck-XXXXXX")"
BINARY_PATH="${TMP_DIR}/clawkangsar"
CONFIG_PATH="${TMP_DIR}/config.memcheck.json"
LOG_PATH="${TMP_DIR}/stdout.log"

cleanup() {
  if [[ -n "${APP_PID:-}" ]] && kill -0 "${APP_PID}" >/dev/null 2>&1; then
    kill "${APP_PID}" >/dev/null 2>&1 || true
    wait "${APP_PID}" >/dev/null 2>&1 || true
  fi
  if [[ "${KEEP_ARTIFACTS}" -ne 1 ]]; then
    rm -rf "${TMP_DIR}"
  fi
}
trap cleanup EXIT

cat > "${CONFIG_PATH}" <<'EOF'
{
  "log_level": "WARN",
  "system_prompt": "You are ClawKangsar, a professional assistant running on a Raspberry Pi. Keep responses concise and use your browser tool only when real-time data is needed.",
  "whatsapp": {
    "enabled": false,
    "session_dsn": "file:clawkangsar_whatsapp.db?_foreign_keys=on"
  },
  "telegram": {
    "enabled": false,
    "token": "",
    "allow_list": []
  },
  "browser": {
    "idle_timeout_seconds": 300
  },
  "storage": {
    "session_dir": "data/sessions"
  },
  "health": {
    "enabled": false,
    "host": "127.0.0.1",
    "port": 18080
  },
  "tools": {
    "web_fetch_timeout_seconds": 20,
    "web_fetch_max_chars": 4000
  }
}
EOF

echo "Building ClawKangsar for memory check..."
(cd "${ROOT_DIR}" && go build -o "${BINARY_PATH}" ./cmd/clawkangsar)

echo "Starting ClawKangsar (idle mode)..."
"${BINARY_PATH}" -config "${CONFIG_PATH}" >"${LOG_PATH}" 2>&1 &
APP_PID=$!

sleep 1
if ! kill -0 "${APP_PID}" >/dev/null 2>&1; then
  echo "ERROR: ClawKangsar exited before sampling. Check ${LOG_PATH}" >&2
  exit 2
fi
if [[ ! -r "/proc/${APP_PID}/status" ]] || ! grep -q '^VmRSS:' "/proc/${APP_PID}/status"; then
  echo "ERROR: VmRSS metrics are unavailable for PID ${APP_PID}. Run this script on Linux (Raspberry Pi)." >&2
  exit 2
fi

echo "Warmup: ${WARMUP_SECONDS}s"
sleep "${WARMUP_SECONDS}"

limit_kb=$((LIMIT_MB * 1024))
max_kb=0
sum_kb=0
samples=0

echo "Sampling RSS for ${SAMPLE_SECONDS}s..."
for _ in $(seq 1 "${SAMPLE_SECONDS}"); do
  if ! kill -0 "${APP_PID}" >/dev/null 2>&1; then
    echo "ERROR: ClawKangsar exited during sampling. Check ${LOG_PATH}" >&2
    exit 2
  fi

  rss_kb="$(awk '/VmRSS:/ { print $2 }' "/proc/${APP_PID}/status" 2>/dev/null || true)"
  rss_kb="${rss_kb:-0}"
  if ((rss_kb <= 0)); then
    echo "ERROR: VmRSS sample unavailable for PID ${APP_PID}." >&2
    exit 2
  fi

  if ((rss_kb > max_kb)); then
    max_kb="${rss_kb}"
  fi

  sum_kb=$((sum_kb + rss_kb))
  samples=$((samples + 1))
  sleep 1
done

avg_kb=0
if ((samples > 0)); then
  avg_kb=$((sum_kb / samples))
fi

max_mb="$(awk "BEGIN { printf \"%.2f\", ${max_kb} / 1024 }")"
avg_mb="$(awk "BEGIN { printf \"%.2f\", ${avg_kb} / 1024 }")"

echo
echo "ClawKangsar Pi Memory Check"
echo "  Limit MB:        ${LIMIT_MB}"
echo "  Peak RSS MB:     ${max_mb}"
echo "  Average RSS MB:  ${avg_mb}"
echo "  Samples:         ${samples}"
echo "  Binary:          ${BINARY_PATH}"

if ((max_kb <= limit_kb)); then
  echo "RESULT: PASS (peak RSS <= ${LIMIT_MB} MB)"
  exit 0
fi

echo "RESULT: FAIL (peak RSS > ${LIMIT_MB} MB)"
echo "Hint: inspect logs at ${LOG_PATH}"
exit 1
