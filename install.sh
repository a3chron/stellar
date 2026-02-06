#!/bin/bash
set -e

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case $ARCH in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

BINARY="stellar-${OS}-${ARCH}"
URL="https://github.com/a3chron/stellar/releases/latest/download/${BINARY}"

echo "Downloading stellar for ${OS}-${ARCH}..."
curl -L -o /usr/local/bin/stellar "$URL"
chmod +x /usr/local/bin/stellar

echo "✳ stellar installed! Run: stellar --help"