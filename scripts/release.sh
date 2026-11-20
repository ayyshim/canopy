#!/usr/bin/env bash
# Builds Canopy's two deliverables into dist/:
#   canopy-hub-<os>-<arch>  the hub: API, web app, client downloads and PostgreSQL in one binary
#   canopy-<os>-<arch>      the client people run on their machines
# PLATFORMS="linux/amd64" scripts/release.sh builds just one platform.
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION="${VERSION:-$(git describe --tags --always 2>/dev/null || echo 0.1.0-dev)}"
PLATFORMS="${PLATFORMS:-linux/amd64 linux/arm64 darwin/amd64 darwin/arm64}"
PG_VERSION=16.9.0
OUT=dist
WEB=hub/internal/web
PGBIN=hub/internal/pgserver/pgbin
CACHE=.cache/postgres
LDFLAGS="-s -w -X main.version=$VERSION"
mkdir -p "$OUT" "$CACHE" "$PGBIN"

clean() { find "$1" -mindepth 1 ! -name README.txt -exec rm -rf {} +; }

echo "==> Web app"
(cd frontend && NEXT_TELEMETRY_DISABLED=1 NEXT_OUTPUT=export pnpm exec next build >/dev/null)
clean "$WEB/dist"
cp -R frontend/out/. "$WEB/dist/"

echo "==> Clients ($PLATFORMS)"
clean "$WEB/clients"
for p in $PLATFORMS; do
  os=${p%/*}; arch=${p#*/}
  (cd client && CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "$LDFLAGS" -o "../$WEB/clients/canopy-$os-$arch" ./cmd/canopy)
  cp "$WEB/clients/canopy-$os-$arch" "$OUT/canopy-$os-$arch"
done

echo "==> Hubs"
for p in $PLATFORMS; do
  os=${p%/*}; arch=${p#*/}
  pgarch=$arch; [ "$arch" = arm64 ] && pgarch=arm64v8
  name="embedded-postgres-binaries-$os-$pgarch-$PG_VERSION"
  if [ ! -f "$CACHE/$name.txz" ]; then
    echo "    fetching PostgreSQL $PG_VERSION for $os/$arch"
    curl -fsSL "https://repo1.maven.org/maven2/io/zonky/test/postgres/embedded-postgres-binaries-$os-$pgarch/$PG_VERSION/$name.jar" -o "$CACHE/$name.jar"
    unzip -p "$CACHE/$name.jar" '*.txz' > "$CACHE/$name.txz"
  fi
  clean "$PGBIN"
  cp "$CACHE/$name.txz" "$PGBIN/"
  (cd hub && CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -tags embedpg -trimpath -ldflags "$LDFLAGS" -o "../$OUT/canopy-hub-$os-$arch" ./cmd/hub)
done
clean "$PGBIN"

echo "==> Built $VERSION:"
ls -lh "$OUT"
