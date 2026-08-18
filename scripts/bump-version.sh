#!/usr/bin/env bash
#
# Bump MoonBit's version across every file that hardcodes it, and refresh the
# AUR source checksum from the actual published tag tarball.
#
#   scripts/bump-version.sh 1.5.0            # update files, checksum left as TODO
#   scripts/bump-version.sh 1.5.0 --checksum # also fetch the tag tarball and hash it
#
# The --checksum step only works AFTER the tag is pushed, because it hashes the
# GitHub-generated archive. The usual order is:
#
#   1. scripts/bump-version.sh X.Y.Z
#   2. commit, tag vX.Y.Z, push --tags
#   3. scripts/bump-version.sh X.Y.Z --checksum
#   4. commit the refreshed PKGBUILD/.SRCINFO, push to AUR
#
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="${1:-}"
WANT_CHECKSUM="${2:-}"

if [ -z "$VERSION" ]; then
    echo "usage: $0 <version> [--checksum]" >&2
    exit 1
fi
if ! printf '%s' "$VERSION" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$'; then
    echo "error: version must look like 1.5.0 (no leading v)" >&2
    exit 1
fi

OLD_VERSION=$(awk -F= '/^pkgver=/{print $2}' PKGBUILD)
echo "==> $OLD_VERSION -> $VERSION"

# PKGBUILD
sed -i -E "s/^pkgver=.*/pkgver=${VERSION}/" PKGBUILD
sed -i -E "s/^pkgrel=.*/pkgrel=1/" PKGBUILD

# flake.nix
sed -i -E "s/^(        version = \")[^\"]+(\";)/\1${VERSION}\2/" flake.nix

if [ "$WANT_CHECKSUM" = "--checksum" ]; then
    TARBALL_URL="https://github.com/Nomadcxx/moonbit/archive/v${VERSION}.tar.gz"
    echo "==> fetching $TARBALL_URL"
    TMP=$(mktemp)
    trap 'rm -f "$TMP"' EXIT
    if ! curl -fsSL -o "$TMP" "$TARBALL_URL"; then
        echo "error: could not fetch the tag tarball. Is v${VERSION} pushed?" >&2
        exit 1
    fi
    SUM=$(sha256sum "$TMP" | awk '{print $1}')
    echo "==> sha256 $SUM"
    sed -i -E "s/^sha256sums=\('.*'\)/sha256sums=('${SUM}')/" PKGBUILD
else
    echo "==> checksum NOT updated (re-run with --checksum after pushing the tag)"
fi

# .SRCINFO is generated, never hand-edited.
if command -v makepkg >/dev/null 2>&1; then
    makepkg --printsrcinfo > .SRCINFO
    echo "==> regenerated .SRCINFO"
else
    echo "!!  makepkg not found; regenerate .SRCINFO manually:" >&2
    echo "      makepkg --printsrcinfo > .SRCINFO" >&2
fi

echo
echo "Changed:"
git --no-pager diff --stat -- PKGBUILD .SRCINFO flake.nix
echo
echo "Verify with:  grep -n '${VERSION}' PKGBUILD .SRCINFO flake.nix"
