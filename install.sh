#!/usr/bin/env bash
# agent-master installer — downloads the release binary for this platform.
#
#   curl -fsSL https://raw.githubusercontent.com/Ken-Chy129/agent-master/main/install.sh | bash
#
# Env:
#   AGENT_MASTER_VERSION  specific version (default: latest release)
#   INSTALL_DIR           install prefix (default: /usr/local/bin)
set -euo pipefail

REPO="Ken-Chy129/agent-master"
BIN="agent-master"
# Default to a user-owned dir so no sudo is needed. Override with INSTALL_DIR
# (e.g. INSTALL_DIR=/usr/local/bin, which then falls back to sudo).
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

die() { echo "Error: $*" >&2; exit 1; }

detect_target() {
  local os arch
  case "$(uname -s)" in
    Linux)  os=linux ;;
    Darwin) os=darwin ;;
    *) die "unsupported OS: $(uname -s)" ;;
  esac
  case "$(uname -m)" in
    x86_64|amd64)  arch=amd64 ;;
    arm64|aarch64) arch=arm64 ;;
    *) die "unsupported arch: $(uname -m)" ;;
  esac
  echo "${os}-${arch}"
}

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}';
  elif command -v shasum >/dev/null 2>&1; then shasum -a 256 "$1" | awk '{print $1}';
  else echo ""; fi
}

main() {
  command -v curl >/dev/null 2>&1 || die "curl is required"

  local target ver base asset tmp
  target="$(detect_target)"

  ver="${AGENT_MASTER_VERSION:-}"
  if [ -z "$ver" ]; then
    ver="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
      | grep '"tag_name"' | head -1 | sed 's/.*"v\{0,1\}\([^"]*\)".*/\1/')" || true
  fi
  [ -n "$ver" ] || die "cannot determine version; set AGENT_MASTER_VERSION"

  base="https://github.com/${REPO}/releases/download/v${ver}"
  asset="${BIN}-${target}"
  tmp="$(mktemp -d)"; trap 'rm -rf "${tmp:-}"' EXIT

  echo "Downloading ${asset} (v${ver})..."
  curl -fsSL "${base}/${asset}" -o "${tmp}/${BIN}" || die "download failed"

  if curl -fsSL "${base}/${asset}.sha256" -o "${tmp}/sum" 2>/dev/null; then
    local expected actual
    expected="$(awk '{print $1}' "${tmp}/sum")"
    actual="$(sha256 "${tmp}/${BIN}")"
    if [ -n "$actual" ] && [ "$expected" != "$actual" ]; then
      die "checksum mismatch (expected $expected, got $actual)"
    fi
    echo "Checksum OK."
  fi

  chmod +x "${tmp}/${BIN}"
  # A user-owned dir (the default) installs without sudo; a system dir falls
  # back to sudo.
  if mkdir -p "$INSTALL_DIR" 2>/dev/null && [ -w "$INSTALL_DIR" ]; then
    mv "${tmp}/${BIN}" "${INSTALL_DIR}/${BIN}"
  elif command -v sudo >/dev/null 2>&1; then
    echo "Installing to ${INSTALL_DIR} (requires sudo)..."
    sudo mkdir -p "$INSTALL_DIR"
    sudo mv "${tmp}/${BIN}" "${INSTALL_DIR}/${BIN}"
  else
    die "${INSTALL_DIR} not writable and sudo unavailable"
  fi

  echo ""
  echo "Installed ${INSTALL_DIR}/${BIN} (v${ver})"

  # Nudge if the install dir isn't on PATH (common for ~/.local/bin on macOS).
  case ":${PATH}:" in
    *":${INSTALL_DIR}:"*)
      run="${BIN}" ;;
    *)
      run="${INSTALL_DIR}/${BIN}"
      echo ""
      echo "Note: ${INSTALL_DIR} is not on your PATH. Add it, e.g.:"
      echo "  echo 'export PATH=\"${INSTALL_DIR}:\$PATH\"' >> ~/.zshrc && exec \$SHELL"
      ;;
  esac

  echo ""
  echo "Next:"
  echo "  ${run} service install     # run as a background service"
  echo "  ${run} pair                # show URL/token/QR to connect a client"
}

main "$@"
