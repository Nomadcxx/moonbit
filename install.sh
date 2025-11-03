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
if ! command -v go &> /dev/null; then
    echo "Error: Go is not installed. Please install Go 1.21+ first."
    exit 1
fi

if ! command -v git &> /dev/null; then
    echo "Error: git is not installed. Please install git first."
    exit 1
fi

# Create temp directory
TEMP_DIR=$(mktemp -d)
cd "$TEMP_DIR"

echo "Downloading MoonBit..."
git clone --depth 1 https://github.com/Nomadcxx/moonbit.git
cd moonbit

echo "Building installer..."
make installer

echo ""
echo "Starting installer..."
echo ""

# Run the installer
./moonbit-installer

# Cleanup
cd /
rm -rf "$TEMP_DIR"

echo ""
echo "Installation complete!"
