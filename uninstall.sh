#!/usr/bin/env bash
set -euo pipefail

# Uninstaller for stellar on Linux / macOS.
# Usage: curl -fsSL https://raw.githubusercontent.com/a3chron/stellar/main/uninstall.sh | bash
#
# Prefers `stellar uninstall`, which detaches your prompt from the theme cache
# (so starship keeps working), tells the hub this install is gone, and removes
# the binary. Extra arguments are passed through, e.g. `bash -s -- --keep-config`.
# Falls back to removing the files directly for builds that predate the command.

if [[ "$OSTYPE" == "msys" || "$OSTYPE" == "cygwin" || "$OSTYPE" == "win32" ]]; then
  echo "On Windows, uninstall stellar with PowerShell instead:"
  echo "  irm https://raw.githubusercontent.com/a3chron/stellar/main/uninstall.ps1 | iex"
  exit 1
fi

# Same defaults as install.sh
PREFIX="${PREFIX:-$HOME/.local}"
BIN_DIR="$PREFIX/bin"
STELLAR_HOME="${STELLAR_HOME:-$HOME/.config/stellar}"

# Find the binary: the install location first, then whatever is on PATH.
STELLAR_BIN=""
if [[ -x "$BIN_DIR/stellar" ]]; then
  STELLAR_BIN="$BIN_DIR/stellar"
elif command -v stellar >/dev/null 2>&1; then
  STELLAR_BIN="$(command -v stellar)"
fi

if [[ -n "$STELLAR_BIN" ]] && "$STELLAR_BIN" uninstall --help >/dev/null 2>&1; then
  exec "$STELLAR_BIN" uninstall --yes "$@"
fi

# Fallback for older builds without `stellar uninstall`.
echo "Removing stellar manually (this build has no uninstall command)"

if [[ -L "$HOME/.config/starship.toml" ]]; then
  TARGET="$(readlink "$HOME/.config/starship.toml")"
  case "$TARGET" in
    "$STELLAR_HOME"/*)
      # Keep the prompt working: replace the link into the cache with a copy.
      cp "$TARGET" "$HOME/.config/starship.toml.stellar-detach"
      mv "$HOME/.config/starship.toml.stellar-detach" "$HOME/.config/starship.toml"
      echo "Detached ~/.config/starship.toml (now a regular file)"
      ;;
  esac
fi

if [[ -d "$STELLAR_HOME" ]]; then
  rm -rf "$STELLAR_HOME"
  echo "Removed $STELLAR_HOME"
fi

if [[ -n "$STELLAR_BIN" ]]; then
  rm -f "$STELLAR_BIN"
  echo "Removed $STELLAR_BIN"
fi

echo "stellar has been uninstalled."
