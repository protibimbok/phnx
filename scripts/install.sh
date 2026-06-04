#!/usr/bin/env bash
# Install the latest phnx release binary.
# Usage: curl -fsSL https://raw.githubusercontent.com/protibimbok/phnx/main/scripts/install.sh | bash

set -e

REPO="protibimbok/phnx"
BINARY="phnx"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# ── helpers ────────────────────────────────────────────────────────────────

info()  { printf '\033[0;32m[phnx]\033[0m %s\n' "$*"; }
error() { printf '\033[0;31m[phnx] error:\033[0m %s\n' "$*" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || error "required tool not found: $1"; }

detect_os() {
  case "$(uname -s)" in
    Linux*)  echo linux ;;
    Darwin*) echo darwin ;;
    *) error "unsupported OS: $(uname -s)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64)  echo amd64 ;;
    aarch64|arm64) echo arm64 ;;
    *) error "unsupported architecture: $(uname -m)" ;;
  esac
}

latest_tag() {
  need curl
  curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' \
    | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/'
}

# ── main ───────────────────────────────────────────────────────────────────

need curl

OS=$(detect_os)
ARCH=$(detect_arch)
TAG=$(latest_tag)

ARCHIVE="${BINARY}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${TAG}/${ARCHIVE}"

info "Installing phnx ${TAG} (${OS}/${ARCH})…"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

info "Downloading ${URL}"
curl -fsSL "$URL" -o "${TMP}/${ARCHIVE}"

# Verify checksum
CHECKSUM_URL="https://github.com/${REPO}/releases/download/${TAG}/checksums.txt"
curl -fsSL "$CHECKSUM_URL" -o "${TMP}/checksums.txt"
(cd "$TMP" && grep "${ARCHIVE}" checksums.txt | sha256sum -c -)
info "Checksum verified"

tar -xzf "${TMP}/${ARCHIVE}" -C "$TMP"

if [ ! -w "$INSTALL_DIR" ]; then
  info "Writing to ${INSTALL_DIR} (sudo required)"
  sudo install -m 755 "${TMP}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
else
  install -m 755 "${TMP}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
fi

info "Installed: $(command -v phnx)"
phnx --version
info "Run 'phnx configure' to get started."
