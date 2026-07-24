#!/usr/bin/env bash
# Download and unpack augmem/cortext release assets for offline / CI use.
set -euo pipefail

VERSION="${CORTEXT_ASSETS_VERSION:-1.2.4}"
VERSION="${VERSION#v}"
DEST="${1:-${CORTEXT_ASSETS_DIR:-$HOME/.cache/augmem/cortext/assets/${VERSION}}}"
BASE_URL="${CORTEXT_ASSETS_URL:-https://github.com/augmem/cortext/releases/download/v${VERSION}/cortext-assets-${VERSION}.tar.gz}"
SUM_URL="${BASE_URL}.sha256"

mkdir -p "$DEST"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "Fetching $BASE_URL"
curl -fsSL -o "$TMP/assets.tgz" "$BASE_URL"
curl -fsSL -o "$TMP/assets.tgz.sha256" "$SUM_URL"

(
  cd "$TMP"
  if command -v sha256sum >/dev/null; then
    sha256sum -c assets.tgz.sha256
  else
    EXPECTED="$(awk '{print $1}' assets.tgz.sha256)"
    ACTUAL="$(shasum -a 256 assets.tgz | awk '{print $1}')"
    test "$EXPECTED" = "$ACTUAL"
  fi
)

tar -xzf "$TMP/assets.tgz" -C "$DEST"
echo "$VERSION" >"$DEST/.cortext-assets-ok"
echo "Unpacked assets into $DEST"
echo "export CORTEXT_ASSETS_DIR=$DEST"
