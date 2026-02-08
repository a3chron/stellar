#!/usr/bin/env bash
set -euo pipefail

# Configurable install prefix
PREFIX="${PREFIX:-$HOME/.local}"
BIN_DIR="$PREFIX/bin"

# Detect OS and architecture
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

BINARY="stellar-${OS}-${ARCH}"
URL="https://github.com/a3chron/stellar/releases/latest/download/${BINARY}"

echo "Installing stellar for ${OS}-${ARCH}"
echo "Target directory: $BIN_DIR"

mkdir -p "$BIN_DIR"

curl -fsSL "$URL" -o "$BIN_DIR/stellar"
chmod +x "$BIN_DIR/stellar"

echo "stellar installed successfully!"

# PATH hint
if ! command -v stellar >/dev/null 2>&1; then
  echo
  echo "Warning: $BIN_DIR is not in your PATH"
  echo "Add this to your shell config:"
  echo "  export PATH=\"$BIN_DIR:\$PATH\""
fi

echo
echo "Run:"
echo "  stellar --help"
