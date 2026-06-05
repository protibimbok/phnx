#!/usr/bin/env bash
# Install phnx from GitHub releases.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/protibimbok/phnx/main/scripts/install.sh | bash
#   curl -fsSL ... | bash -s -- --version v1.0.2
#   curl -fsSL ... | bash -s -- --install-dir ~/.local/bin
#   INSTALL_DIR=~/.local/bin curl -fsSL ... | bash

set -e

REPO="protibimbok/phnx"
BINARY="phnx"
RELEASE="latest"
INSTALL_DIR="${INSTALL_DIR:-}"
SKIP_BREW="false"
SKIP_PATH_HINT="false"

# ── helpers ────────────────────────────────────────────────────────────────

info()  { printf '\033[0;32m[phnx]\033[0m %s\n' "$*"; }
warn()  { printf '\033[0;33m[phnx]\033[0m %s\n' "$*" >&2; }
error() { printf '\033[0;31m[phnx] error:\033[0m %s\n' "$*" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || error "required tool not found: $1"; }

usage() {
  cat <<EOF
Install phnx from GitHub releases.

Usage:
  curl -fsSL https://raw.githubusercontent.com/${REPO}/main/scripts/install.sh | bash
  curl -fsSL ... | bash -s -- [options]

Options:
  -d, --install-dir <dir>   Install directory (default: ~/.local/bin, or /usr/local/bin)
  -r, --version <tag>       Release tag to install (default: latest)
      --force-binary        Skip Homebrew on macOS and download the binary
      --skip-path-hint      Do not print PATH setup instructions
  -h, --help                Show this help

Environment:
  INSTALL_DIR               Same as --install-dir
EOF
}

parse_args() {
  while [ $# -gt 0 ]; do
    case "$1" in
      -d | --install-dir)
        [ -n "${2:-}" ] || error "--install-dir requires a path"
        INSTALL_DIR=$2
        shift 2
        ;;
      -r | --version | --release)
        [ -n "${2:-}" ] || error "--version requires a tag"
        RELEASE=$2
        shift 2
        ;;
      --force-binary | --force-no-brew)
        SKIP_BREW="true"
        shift
        ;;
      --skip-path-hint)
        SKIP_PATH_HINT="true"
        shift
        ;;
      -h | --help)
        usage
        exit 0
        ;;
      *)
        error "unknown argument: $1 (try --help)"
        ;;
    esac
  done
}

detect_os() {
  case "$(uname -s)" in
    Linux*)  echo linux ;;
    Darwin*) echo darwin ;;
    *) error "unsupported OS: $(uname -s) (supported: Linux, macOS)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64 | amd64)  echo amd64 ;;
    aarch64 | arm64) echo arm64 ;;
    *) error "unsupported architecture: $(uname -m) (supported: amd64, arm64)" ;;
  esac
}

default_install_dir() {
  if [ -n "$INSTALL_DIR" ]; then
    printf '%s\n' "$INSTALL_DIR"
    return
  fi

  case "${OS}" in
    darwin)
      if [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
        printf '/usr/local/bin\n'
      else
        printf '%s\n' "${HOME}/.local/bin"
      fi
      ;;
    *)
      printf '%s\n' "${HOME}/.local/bin"
      ;;
  esac
}

resolve_release_tag() {
  if [ "$RELEASE" = "latest" ]; then
    local json
    json=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest")
    printf '%s\n' "$json" \
      | grep -m1 '"tag_name"' \
      | sed -E 's/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/'
  else
    printf '%s\n' "$RELEASE"
  fi
}

verify_checksum() {
  local archive=$1
  local checksums=$2
  local archive_name expected actual

  archive_name=$(basename "$archive")
  expected=$(grep "${archive_name}" "${checksums}" | awk '{print $1}')
  [ -n "$expected" ] || error "checksum not found for ${archive_name}"

  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$(dirname "$archive")" && grep "${archive_name}" "$(basename "$checksums")" | sha256sum -c -)
  elif command -v shasum >/dev/null 2>&1; then
    actual=$(shasum -a 256 "$archive" | awk '{print $1}')
    [ "$actual" = "$expected" ] || error "checksum mismatch for ${archive}"
  else
    error "sha256sum or shasum is required to verify downloads"
  fi
}

install_binary() {
  local src=$1
  local dest="${INSTALL_DIR}/${BINARY}"

  mkdir -p "$INSTALL_DIR"

  if [ -w "$INSTALL_DIR" ]; then
    install -m 755 "$src" "$dest"
  elif command -v sudo >/dev/null 2>&1; then
    info "Writing to ${INSTALL_DIR} (sudo required)"
    sudo install -m 755 "$src" "$dest"
  else
    error "cannot write to ${INSTALL_DIR} — choose a writable directory with --install-dir"
  fi
}

path_hint() {
  [ "$SKIP_PATH_HINT" = "true" ] && return

  case ":${PATH}:" in
    *":${INSTALL_DIR}:"*) return ;;
  esac

  warn "${INSTALL_DIR} is not on your PATH"
  printf '\nAdd it to your shell profile:\n\n'

  case "$(basename "${SHELL:-}")" in
    fish)
      printf '  fish_add_path %s\n' "$INSTALL_DIR"
      ;;
    zsh)
      printf '  echo '\''export PATH="%s:$PATH"'\'' >> ~/.zshrc\n' "$INSTALL_DIR"
      printf '  source ~/.zshrc\n'
      ;;
    *)
      printf '  echo '\''export PATH="%s:$PATH"'\'' >> ~/.bashrc\n' "$INSTALL_DIR"
      printf '  source ~/.bashrc\n'
      ;;
  esac
  printf '\n'
}

install_via_brew() {
  [ "$OS" = "darwin" ] || return 1
  [ "$SKIP_BREW" = "true" ] && return 1
  command -v brew >/dev/null 2>&1 || return 1

  info "Installing via Homebrew..."
  if ! brew tap | grep -qx 'protibimbok/pkg-dist'; then
    brew tap protibimbok/pkg-dist
  fi
  brew install phnx
  INSTALL_DIR="$(brew --prefix)/bin"
  return 0
}

install_via_release() {
  TAG=$(resolve_release_tag)
  [ -n "$TAG" ] || error "could not resolve release tag"

  ARCHIVE="${BINARY}_${OS}_${ARCH}.tar.gz"
  URL="https://github.com/${REPO}/releases/download/${TAG}/${ARCHIVE}"

  info "Installing phnx ${TAG} (${OS}/${ARCH}) to ${INSTALL_DIR}..."

  TMP=$(mktemp -d)
  trap 'rm -rf "$TMP"' EXIT

  info "Downloading ${URL}"
  curl -fsSL "$URL" -o "${TMP}/${ARCHIVE}"

  CHECKSUM_URL="https://github.com/${REPO}/releases/download/${TAG}/checksums.txt"
  curl -fsSL "$CHECKSUM_URL" -o "${TMP}/checksums.txt"
  verify_checksum "${TMP}/${ARCHIVE}" "${TMP}/checksums.txt"
  info "Checksum verified"

  tar -xzf "${TMP}/${ARCHIVE}" -C "$TMP"
  install_binary "${TMP}/${BINARY}"
}

check_dependencies() {
  need curl
  need tar
}

# ── main ───────────────────────────────────────────────────────────────────

parse_args "$@"

OS=$(detect_os)
ARCH=$(detect_arch)
INSTALL_DIR=$(default_install_dir)

check_dependencies

if install_via_brew; then
  :
else
  if [ "$OS" = "darwin" ] && [ "$SKIP_BREW" != "true" ] && ! command -v brew >/dev/null 2>&1; then
    warn "Homebrew not found — downloading the macOS binary from GitHub"
    warn "Install Homebrew first if you prefer managed upgrades: https://brew.sh"
  fi
  install_via_release
fi

if command -v phnx >/dev/null 2>&1; then
  info "Installed: $(command -v phnx)"
  phnx --version
else
  warn "phnx was installed to ${INSTALL_DIR}/${BINARY} but is not on your PATH yet"
fi

path_hint
info "Run 'phnx configure' to get started."
