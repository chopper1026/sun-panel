#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FRONTEND_PORT="${FRONTEND_PORT:-1002}"
PNPM_VERSION="${PNPM_VERSION:-8.15.9}"

cleanup() {
  if [[ -n "${BACKEND_PID:-}" ]] && kill -0 "$BACKEND_PID" 2>/dev/null; then
    kill "$BACKEND_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

if [[ -f "$ROOT_DIR/service/assets/bindata.go" ]]; then
  echo "Removing generated service/assets/bindata.go; standard embed is used for local dev."
  rm -f "$ROOT_DIR/service/assets/bindata.go"
fi

if [[ ! -d "$ROOT_DIR/node_modules" ]]; then
  echo "Installing frontend dependencies with pnpm ${PNPM_VERSION}..."
  (cd "$ROOT_DIR" && npx "pnpm@${PNPM_VERSION}" install --frozen-lockfile)
fi

echo "Starting backend. Default URL: http://127.0.0.1:3002"
(
  cd "$ROOT_DIR/service"
  GOPROXY="${GOPROXY:-https://goproxy.cn,direct}" go run main.go
) &
BACKEND_PID=$!
sleep 1
if ! kill -0 "$BACKEND_PID" 2>/dev/null; then
  wait "$BACKEND_PID"
fi

echo "Starting frontend on http://127.0.0.1:${FRONTEND_PORT}"
echo "Press Ctrl+C to stop both processes."
(cd "$ROOT_DIR" && npx "pnpm@${PNPM_VERSION}" dev --host 0.0.0.0 --port "$FRONTEND_PORT")
