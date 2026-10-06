#!/bin/sh
# Installer jenderalcode — dipakai dengan:
#
#   curl -fsSL https://raw.githubusercontent.com/mohammadirham37/jenderal_code/main/install.sh | sh
#
# Mengunduh rilis terbaru dari GitHub Releases, memverifikasi checksum,
# lalu memasang `jenderalcode` (dan alias `jc`) ke ~/.local/bin.
# Opsi: --version vX.Y.Z  |  --bin-dir <dir>  |  JENDERALCODE_VERSION=vX.Y.Z
set -eu

REPO="mohammadirham37/jenderal_code"
BIN_DIR="${JENDERALCODE_BIN_DIR:-$HOME/.local/bin}"
VERSION="${JENDERALCODE_VERSION:-}"

while [ $# -gt 0 ]; do
    case "$1" in
    --version)
        VERSION="$2"
        shift 2
        ;;
    --bin-dir)
        BIN_DIR="$2"
        shift 2
        ;;
    --help | -h)
        echo "Pemakaian: install.sh [--version vX.Y.Z] [--bin-dir <dir>]"
        exit 0
        ;;
    *)
        echo "✗ opsi tidak dikenal: $1" >&2
        exit 1
        ;;
    esac
done

msg() { printf '%s\n' "$*"; }
fail() { printf '✗ %s\n' "$*" >&2; exit 1; }

# ---- deteksi OS & arsitektur ----
os="$(uname -s)"
arch="$(uname -m)"
case "$os" in
Darwin) os="darwin" ;;
Linux) os="linux" ;;
*) fail "OS tidak didukung: $os (hanya macOS dan Linux)" ;;
esac
case "$arch" in
arm64 | aarch64) arch="arm64" ;;
x86_64 | amd64) arch="amd64" ;;
*) fail "arsitektur tidak didukung: $arch" ;;
esac

# ---- tentukan versi rilis ----
if [ -z "$VERSION" ]; then
    msg "→ mencari rilis terbaru..."
    VERSION="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" |
        grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')"
    [ -n "$VERSION" ] || fail "tidak bisa menentukan rilis terbaru; periksa koneksi, atau tentukan lewat --version"
fi
ver="${VERSION#v}"
name="jenderalcode_${ver}_${os}_${arch}.tar.gz"
url="https://github.com/$REPO/releases/download/${VERSION}/${name}"

msg "→ mengunduh $name ..."
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
curl -fsSL "$url" -o "$tmp/$name" ||
    fail "unduhan gagal ($url); pastikan rilis $VERSION tersedia"

# ---- verifikasi checksum ----
msg "→ memverifikasi checksum..."
curl -fsSL "https://github.com/$REPO/releases/download/${VERSION}/SHA256SUMS" -o "$tmp/SHA256SUMS" ||
    fail "checksum tidak bisa diunduh"
want="$(grep "$name" "$tmp/SHA256SUMS" | awk '{print $1}')"
[ -n "$want" ] || fail "checksum untuk $name tidak ditemukan"
if command -v shasum >/dev/null 2>&1; then
    got="$(shasum -a 256 "$tmp/$name" | awk '{print $1}')"
else
    got="$(sha256sum "$tmp/$name" | awk '{print $1}')"
fi
[ "$got" = "$want" ] || fail "checksum tidak cocok ($got ≠ $want); unduhan korup, coba lagi"

# ---- pasang ----
tar -xzf "$tmp/$name" -C "$tmp"
mkdir -p "$BIN_DIR"
mv "$tmp/jenderalcode" "$BIN_DIR/jenderalcode"
mv "$tmp/jc" "$BIN_DIR/jc"
chmod +x "$BIN_DIR/jenderalcode" "$BIN_DIR/jc"

# ---- cek PATH ----
case ":$PATH:" in
*":$BIN_DIR:"*) ;;
*)
    msg ""
    msg "⚠ $BIN_DIR belum ada di PATH. Tambahkan ke ~/.zshrc (atau ~/.bashrc):"
    msg "    export PATH=\"\$HOME/.local/bin:\$PATH\""
    ;;
esac

installed="$("$BIN_DIR/jenderalcode" version 2>/dev/null || echo "(gagal menjalankan)")"
msg ""
msg "✔ jenderalcode terpasang: $installed"
msg "  lokasi: $BIN_DIR/jenderalcode (alias: jc)"
msg "  mulai:  jenderalcode  ·  set API key: jenderalcode auth login <provider>"
