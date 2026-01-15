#!/bin/bash
set -e

# Bootstrap script for evmone
# Downloads pre-built evmone artifacts from GitHub releases

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LIB_DIR="${PROJECT_ROOT}/giga/executor/lib"

# evmone v0.12.0 is compatible with EVMC v12.x
EVMONE_VERSION="${EVMONE_VERSION:-0.12.0}"
EVMONE_RELEASE_URL="https://github.com/ethereum/evmone/releases/download/v${EVMONE_VERSION}"

# Detect OS and architecture
OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
    Linux)
        case "$ARCH" in
            x86_64|amd64)
                ARTIFACT_NAME="evmone-${EVMONE_VERSION}-linux-x86_64.tar.gz"
                OUTPUT_NAME="libevmone.${EVMONE_VERSION}_linux_amd64.so"
                LIB_EXT="so"
                ;;
            *)
                echo "Error: Linux $ARCH is not supported by evmone releases"
                exit 1
                ;;
        esac
        ;;
    Darwin)
        case "$ARCH" in
            arm64|aarch64)
                ARTIFACT_NAME="evmone-${EVMONE_VERSION}-darwin-arm64.tar.gz"
                OUTPUT_NAME="libevmone.${EVMONE_VERSION}_darwin_arm64.dylib"
                LIB_EXT="dylib"
                ;;
            *)
                echo "Error: macOS $ARCH is not supported by evmone releases"
                exit 1
                ;;
        esac
        ;;
    *)
        echo "Error: Unsupported OS: $OS"
        exit 1
        ;;
esac

DOWNLOAD_URL="${EVMONE_RELEASE_URL}/${ARTIFACT_NAME}"
CHECKSUM_URL="${DOWNLOAD_URL}.sha256"

echo "=== evmone Bootstrap Script ==="
echo "OS: $OS ($ARCH)"
echo "evmone version: v${EVMONE_VERSION}"
echo "Artifact: $ARTIFACT_NAME"
echo "Output: $LIB_DIR/$OUTPUT_NAME"
echo ""

# Create lib directory
mkdir -p "$LIB_DIR"

# Create temp directory for download
TEMP_DIR=$(mktemp -d)
trap "rm -rf $TEMP_DIR" EXIT

cd "$TEMP_DIR"

echo "Downloading evmone..."
curl -sSL -o "$ARTIFACT_NAME" "$DOWNLOAD_URL"

echo "Downloading checksum..."
curl -sSL -o "${ARTIFACT_NAME}.sha256" "$CHECKSUM_URL"

echo "Verifying checksum..."
if command -v sha256sum &> /dev/null; then
    sha256sum -c "${ARTIFACT_NAME}.sha256"
elif command -v shasum &> /dev/null; then
    shasum -a 256 -c "${ARTIFACT_NAME}.sha256"
else
    echo "Warning: No checksum tool found, skipping verification"
fi

echo "Extracting..."
tar -xzf "$ARTIFACT_NAME"

# Find and copy the library with the correct output name
# Look for the versioned library first (e.g., libevmone.0.12.0.dylib)
LIB_FILE=$(find . -name "libevmone.${EVMONE_VERSION}.${LIB_EXT}" -type f | head -1)
if [ -z "$LIB_FILE" ]; then
    # Fall back to unversioned name
    LIB_FILE=$(find . -name "libevmone.${LIB_EXT}" -type f | head -1)
fi
if [ -z "$LIB_FILE" ]; then
    echo "Error: Could not find libevmone.${LIB_EXT} in archive"
    echo "Archive contents:"
    tar -tzf "$ARTIFACT_NAME"
    exit 1
fi

cp "$LIB_FILE" "$LIB_DIR/$OUTPUT_NAME"

echo ""
echo "=== Bootstrap Complete ==="
echo "Library installed to: $LIB_DIR/$OUTPUT_NAME"
