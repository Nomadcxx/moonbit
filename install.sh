#!/bin/bash
set -e

# MoonBit Installer Script
# Downloads and runs the MoonBit installer

echo "MoonBit Installer"
echo "================="
echo ""

# Check for root
if [ "$EUID" -ne 0 ]; then
    echo "Error: This installer requires root privileges."
    echo "Please run with sudo:"
    echo "  curl -sSL https://raw.githubusercontent.com/Nomadcxx/moonbit/main/install.sh | sudo bash"
    exit 1
fi

# Check dependencies
echo "Checking dependencies..."
for tool in go git make; do
    if ! command -v "$tool" &> /dev/null; then
        echo "Error: $tool is not installed."
        case "$tool" in
            go)   echo "  Install Go 1.24 or newer." ;;
            git)  echo "  Install git." ;;
            # The build goes through the Makefile, which is the single source of
            # truth for the version LDFLAGS. A bare `go build` would silently
            # drop them, so make is a genuine requirement, not an accident.
            make) echo "  Install make (Arch: base-devel, Debian: build-essential)." ;;
        esac
        exit 1
    fi
done

# Create temp directory. The trap guarantees cleanup even though `set -e` means
# any failure below exits before reaching the end of the script.
TEMP_DIR=$(mktemp -d)
trap 'cd /; rm -rf "$TEMP_DIR"' EXIT
cd "$TEMP_DIR"

echo "Downloading MoonBit..."
git clone --depth 1 https://github.com/Nomadcxx/moonbit.git
cd moonbit

# Gate on the Go version declared in go.mod, now that we have the source, rather
# than failing later with an opaque toolchain error.
REQUIRED_GO=$(awk '/^go /{print $2; exit}' go.mod)
GO_VERSION=$(go env GOVERSION 2>/dev/null | sed 's/^go//')
if [ -n "$REQUIRED_GO" ] && [ -n "$GO_VERSION" ]; then
    if [ "$(printf '%s\n' "$REQUIRED_GO" "$GO_VERSION" | sort -V | head -n1)" != "$REQUIRED_GO" ]; then
        echo "Error: Go $REQUIRED_GO or newer is required (found $GO_VERSION)."
        exit 1
    fi
fi

echo "Building installer..."
make installer

echo ""
echo "Starting installer..."
echo ""

# Run the installer. Propagate its exit status instead of unconditionally
# reporting success -- `set -e` does not fire here because we capture the code.
INSTALLER_STATUS=0
./moonbit-installer || INSTALLER_STATUS=$?

echo ""
if [ "$INSTALLER_STATUS" -ne 0 ]; then
    echo "Installation FAILED (installer exited with status $INSTALLER_STATUS)."
    exit "$INSTALLER_STATUS"
fi

echo "Installation complete!"
