#!/usr/bin/env bash
# Records the workspace board, driven by fakeclaude, to docs/assets/board.gif.
# Starts its own throwaway Postgres in docker. Needs docker, go, node, pnpm and ffmpeg. Uses its own ports, so it can run beside a dev setup.
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT=$PWD
GIF=${1:-docs/assets/board.gif}

HUB_PORT=18080 RUNNER_PORT=18443 WEB_PORT=13000 PROXY_PORT=18000
export BASE=http://localhost:$PROXY_PORT
WORK=$(mktemp -d)
PGC=canopy-record-pg-$$
PIDS=()

cleanup() {
  [ -z "${KEEP_LOGS:-}" ] || cp "$WORK"/*.log "$WORK"/video/board.webm /tmp/ 2>/dev/null || true
  for p in "${PIDS[@]:-}"; do [ -n "$p" ] && kill "$p" 2>/dev/null || true; done
  lsof -ti tcp:$WEB_PORT 2>/dev/null | xargs kill 2>/dev/null || true
  docker rm -f "$PGC" >/dev/null 2>&1 || true
  rm -rf "$WORK"
}
trap cleanup EXIT

echo "==> Infra"
docker run -d --rm --name "$PGC" -e POSTGRES_USER=canopy -e POSTGRES_PASSWORD=canopy -e POSTGRES_DB=canopy \
  -p 127.0.0.1::5432 postgres:16-alpine >/dev/null
PGPORT=$(docker port "$PGC" 5432/tcp | head -1 | sed 's/.*://')
for _ in $(seq 60); do docker exec "$PGC" pg_isready -U canopy -q && break; sleep 1; done

echo "==> Build"
export CANOPY_BIN=$WORK/canopy FAKECLAUDE_BIN=$WORK/fakeclaude
(cd hub && go build -o "$WORK/hub" ./cmd/hub)
(cd client && go build -o "$CANOPY_BIN" ./cmd/canopy && go build -o "$FAKECLAUDE_BIN" ./cmd/fakeclaude)
[ -d frontend/node_modules ] || (cd frontend && pnpm install --frozen-lockfile >/dev/null)
(cd scripts/record-board && npm install --no-audit --no-fund >/dev/null && npx playwright install chromium >/dev/null)

wait_for() { for _ in $(seq 120); do curl -fs -o /dev/null "$1" && return 0; sleep 1; done; echo "timed out waiting for $1" >&2; exit 1; }

echo "==> Hub, web app, proxy"
DATABASE_URL="postgres://canopy:canopy@127.0.0.1:$PGPORT/canopy?sslmode=disable" HUB_ADDR=127.0.0.1:$HUB_PORT RUNNER_ADDR=127.0.0.1:$RUNNER_PORT \
  PUBLIC_URL=$BASE COOKIE_SECURE=false TRUST_PROXY=true BLOB_DIR=$WORK/blobs HUB_SETUP_TOKEN=record-board \
  REDIS_URL= SMTP_ADDR= AUTH_RATE_LIMIT=1000 "$WORK/hub" --foreground --data "$WORK/hub-data" >"$WORK/hub.log" 2>&1 &
PIDS+=($!)
(cd frontend && HUB_URL=http://127.0.0.1:$HUB_PORT NEXT_TELEMETRY_DISABLED=1 node_modules/.bin/next dev -p $WEB_PORT >"$WORK/web.log" 2>&1) &
PIDS+=($!)
node scripts/record-board/proxy.mjs $PROXY_PORT $HUB_PORT $WEB_PORT &
PIDS+=($!)
wait_for http://127.0.0.1:$HUB_PORT/healthz
wait_for http://127.0.0.1:$WEB_PORT/login

echo "==> Record"
export WORK OUT=$WORK/video
mkdir -p "$OUT"
(cd scripts/record-board && node record.mjs)

echo "==> GIF"
mkdir -p "$(dirname "$GIF")"
# Skip the first second, play 1.5x faster, drop the empty right and bottom edge, 8 fps, 840px wide.
FILTER="setpts=PTS/1.5,fps=8,crop=iw*0.8:ih*0.88:0:0,scale=840:-1:flags=lanczos"
ffmpeg -v error -y -ss 1 -i "$OUT/board.webm" -vf "$FILTER,palettegen=max_colors=16:stats_mode=diff" "$WORK/palette.png"
ffmpeg -v error -y -ss 1 -i "$OUT/board.webm" -i "$WORK/palette.png" \
  -lavfi "[0:v]$FILTER[x];[x][1:v]paletteuse=dither=none:diff_mode=rectangle" -loop 0 "$GIF"
echo "Wrote $GIF ($(du -h "$GIF" | cut -f1))"
