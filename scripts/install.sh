#!/usr/bin/env bash
# Pi-Go installer — curl -fsSL https://raw.githubusercontent.com/Yukaz0/pi-codego/main/scripts/install.sh | bash
# Downloads the latest release binary from GitHub Releases and installs it as `pi-go`.
set -euo pipefail

# --- CONFIG: ganti ke repo GitHub kamu setelah push ---
REPO="${PI_GO_REPO:-Yukaz0/pi-codego}"
BIN_NAME="pi-go"
INSTALL_DIR="${PI_GO_INSTALL_DIR:-$HOME/.local/bin}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  linux) os=linux ;;
  darwin) os=darwin ;;
  msys*|mingw*|cygwin*|windows*) os=windows ;;
  *) echo "unsupported OS: $os" >&2; exit 1 ;;
esac

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac

ext=""
[ "$os" = "windows" ] && ext=".exe"

asset="${BIN_NAME}-${os}-${arch}${ext}"

echo "→ fetching latest release of ${REPO}..."
url="https://api.github.com/repos/${REPO}/releases/latest"
# jq-free extraction of the download url for our asset
download="$(curl -fsSL "$url" \
  | grep -o '"browser_download_url": *"[^"]*'"${asset}"'"' \
  | head -1 | sed 's/.*"browser_download_url": *"//; s/"$//')"

if [ -z "$download" ]; then
  echo "✗ asset ${asset} not found in latest release" >&2
  echo "  check available assets at: https://github.com/${REPO}/releases/latest" >&2
  exit 1
fi

echo "→ downloading ${asset}"
tmp="$(mktemp)"
curl -fsSL -o "$tmp" "$download"
chmod +x "$tmp"

mkdir -p "$INSTALL_DIR"
mv "$tmp" "${INSTALL_DIR}/${BIN_NAME}${ext}"
echo "✓ installed: ${INSTALL_DIR}/${BIN_NAME}${ext}"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    echo ""
    echo "⚠ ${INSTALL_DIR} is not in PATH. Add it:"
    echo "  bash/zsh : echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> ~/.bashrc && source ~/.bashrc"
    echo "  fish     : fish_add_path ${INSTALL_DIR}"
    ;;
esac

echo ""
echo "Run: ${BIN_NAME} --help"
