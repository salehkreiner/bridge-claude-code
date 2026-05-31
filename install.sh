#!/bin/sh
# Scrubadubber bridge installer (Linux and macOS).
#
#   curl -fsSL https://raw.githubusercontent.com/salehkreiner/bridge-claude-code/main/install.sh | sh
#
# It downloads the prebuilt scrub-claude and scrub-setup binaries for your
# OS/arch from GitHub Releases, verifies their checksums when possible, puts them
# on your PATH, and runs scrub-setup once. It does nothing else — read it first if
# you like; that is the whole point of this project.
#
# Environment overrides:
#   SCRUBADUBBER_INSTALL_DIR   where to install   (default: ~/.local/bin)
#   SCRUBADUBBER_VERSION       release tag to get (default: latest)
#   SCRUBADUBBER_HUB_URL       Hub URL passed through to scrub-setup
set -eu

REPO="salehkreiner/bridge-claude-code"
INSTALL_DIR="${SCRUBADUBBER_INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${SCRUBADUBBER_VERSION:-latest}"

say() { printf '%s\n' "$*"; }
err() { printf 'install.sh: error: %s\n' "$*" >&2; exit 1; }

# --- detect platform ---------------------------------------------------------
os=$(uname -s 2>/dev/null || echo unknown)
case "$os" in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) err "unsupported OS '$os' (Linux/macOS only; on Windows use install.ps1)" ;;
esac

arch=$(uname -m 2>/dev/null || echo unknown)
case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*) err "unsupported architecture '$arch'" ;;
esac
suffix="${os}_${arch}"

if [ "$VERSION" = "latest" ]; then
	base="https://github.com/$REPO/releases/latest/download"
else
	base="https://github.com/$REPO/releases/download/$VERSION"
fi

# --- download helper ---------------------------------------------------------
fetch() { # url -> stdout
	if command -v curl >/dev/null 2>&1; then
		curl -fSL --proto '=https' --tlsv1.2 "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -qO- "$1"
	else
		err "need curl or wget"
	fi
}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

say "Installing the Scrubadubber bridge ($suffix) into $INSTALL_DIR"
mkdir -p "$INSTALL_DIR"

# --- optional checksum verification -----------------------------------------
sums=""
if sums=$(fetch "$base/SHA256SUMS" 2>/dev/null); then :; else sums=""; fi

verify() { # file asset
	[ -n "$sums" ] || {
		say "  (no SHA256SUMS published; skipping checksum verification)"
		return 0
	}
	expected=$(printf '%s\n' "$sums" | awk -v a="$2" '$2==a||$2=="*"a{print $1; exit}')
	[ -n "$expected" ] || {
		say "  (no checksum entry for $2; skipping)"
		return 0
	}
	if command -v sha256sum >/dev/null 2>&1; then
		actual=$(sha256sum "$1" | awk '{print $1}')
	elif command -v shasum >/dev/null 2>&1; then
		actual=$(shasum -a 256 "$1" | awk '{print $1}')
	else
		say "  (no sha256 tool; skipping verification)"
		return 0
	fi
	[ "$expected" = "$actual" ] || err "checksum mismatch for $2 (expected $expected, got $actual)"
}

# --- download binaries -------------------------------------------------------
for bin in scrub-claude scrub-setup; do
	asset="${bin}_${suffix}"
	say "  downloading $asset"
	fetch "$base/$asset" >"$tmp/$bin" || err "download failed: $base/$asset"
	verify "$tmp/$bin" "$asset"
	chmod +x "$tmp/$bin"
	mv "$tmp/$bin" "$INSTALL_DIR/$bin"
done
say "Installed scrub-claude and scrub-setup."

# --- ensure PATH -------------------------------------------------------------
ensure_path() {
	dir=$1
	case ":$PATH:" in
	*":$dir:"*) return 0 ;;
	esac
	case "$(basename "${SHELL:-sh}")" in
	zsh) rc="${ZDOTDIR:-$HOME}/.zshrc"; line="export PATH=\"$dir:\$PATH\"" ;;
	fish) rc="$HOME/.config/fish/config.fish"; line="fish_add_path $dir" ;;
	*) rc="$HOME/.bashrc"; line="export PATH=\"$dir:\$PATH\"" ;;
	esac
	if [ -f "$rc" ] && grep -F "$dir" "$rc" >/dev/null 2>&1; then
		:
	else
		mkdir -p "$(dirname "$rc")"
		printf '\n# Added by the Scrubadubber bridge installer\n%s\n' "$line" >>"$rc"
		say "Added $dir to your PATH in $rc"
	fi
}
ensure_path "$INSTALL_DIR"

# --- run setup ---------------------------------------------------------------
say ""
say "Running scrub-setup..."
if [ -n "${SCRUBADUBBER_HUB_URL:-}" ]; then
	"$INSTALL_DIR/scrub-setup" --yes --hub-url "$SCRUBADUBBER_HUB_URL" ||
		say "scrub-setup did not finish; run '$INSTALL_DIR/scrub-setup' yourself when ready."
else
	"$INSTALL_DIR/scrub-setup" --yes ||
		say "scrub-setup did not finish; run '$INSTALL_DIR/scrub-setup' yourself when ready."
fi

say ""
say "Done. Open a new shell so PATH and the 'claude' alias take effect."
