#!/bin/sh
# Installs Canopy from GitHub releases so it runs from any folder.
#   curl -fsSL https://github.com/ayyshim/canopy/releases/latest/download/install.sh | sh
#   ... | sh -s -- hub               installs the hub instead of the client
#   ... | sh -s -- --version v0.1.0  installs a particular release
# CANOPY_BIN_DIR picks the folder (default ~/.local/bin, or /usr/local/bin as root).
# Private repositories need the GitHub CLI, signed in:
#   gh release download -R ayyshim/canopy -p install.sh -O - | sh
set -eu
REPO="${CANOPY_REPO:-ayyshim/canopy}"
NAME=canopy
VERSION=latest
while [ $# -gt 0 ]; do
  case "$1" in
    hub) NAME=canopy-hub; shift ;;
    client) NAME=canopy; shift ;;
    --version) VERSION="$2"; shift 2 ;;
    *) echo "Unknown option: $1" >&2; exit 2 ;;
  esac
done

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in linux|darwin) ;; *) echo "Canopy runs on Linux and macOS, not $OS." >&2; exit 1 ;; esac
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "There’s no Canopy build for $ARCH." >&2; exit 1 ;;
esac
ASSET="$NAME-$OS-$ARCH"

if [ -n "${CANOPY_BIN_DIR:-}" ]; then BIN_DIR="$CANOPY_BIN_DIR"
elif [ "$(id -u)" = 0 ]; then BIN_DIR=/usr/local/bin
else BIN_DIR="$HOME/.local/bin"; fi
mkdir -p "$BIN_DIR"
# Download next to the target: /tmp is sometimes mounted so nothing there can run.
WORK=$(mktemp -d "$BIN_DIR/.canopy-download.XXXXXX")
trap 'rm -rf "$WORK"' EXIT

if [ "$VERSION" = latest ]; then URL="https://github.com/$REPO/releases/latest/download"
else URL="https://github.com/$REPO/releases/download/$VERSION"; fi

fetch() {
  if curl -fsSL "$URL/$1" -o "$WORK/$1" 2>/dev/null; then return 0; fi
  if command -v gh >/dev/null 2>&1; then
    if [ "$VERSION" = latest ]; then gh release download --repo "$REPO" --pattern "$1" --dir "$WORK"
    else gh release download "$VERSION" --repo "$REPO" --pattern "$1" --dir "$WORK"; fi
    return 0
  fi
  echo "Couldn’t download $1 from $URL." >&2
  echo "If the repository is private, install the GitHub CLI and run: gh auth login" >&2
  exit 1
}

echo "Downloading $ASSET ($VERSION) from github.com/$REPO…"
fetch "$ASSET"
fetch SHA256SUMS
WANT=$(grep " $ASSET\$" "$WORK/SHA256SUMS" | cut -d' ' -f1)
if command -v sha256sum >/dev/null 2>&1; then GOT=$(sha256sum "$WORK/$ASSET" | cut -d' ' -f1)
else GOT=$(shasum -a 256 "$WORK/$ASSET" | cut -d' ' -f1); fi
if [ -z "$WANT" ] || [ "$WANT" != "$GOT" ]; then
  echo "$ASSET doesn’t match its checksum in SHA256SUMS, so it wasn’t installed." >&2
  exit 1
fi

chmod +x "$WORK/$ASSET"
"$WORK/$ASSET" install --dir "$BIN_DIR"

if [ "$NAME" = canopy-hub ]; then
  echo "Start the hub with: canopy-hub"
else
  echo "Pair it with your hub using the command Canopy shows: canopy pair --hub URL --code CODE"
fi
