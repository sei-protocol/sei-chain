#!/data/data/com.termux/files/usr/bin/bash
#
# Install the system and Python dependencies required to run the Solo claim
# transaction builder from a Termux environment.
set -euo pipefail

if ! command -v pkg >/dev/null 2>&1; then
  echo "[termux-setup] This script must be executed inside Termux." >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REQUIREMENTS_FILE="$SCRIPT_DIR/requirements.txt"

# Ensure the Termux repositories and base packages are up to date.
pkg update -y
pkg upgrade -y

# Install python and build tooling required for compiling secp256k1 wheels.
pkg install -y python rust binutils clang libffi openssl git

# Upgrade pip to a version that is aware of Termux paths and install deps.
python3 -m pip install --upgrade pip
python3 -m pip install --upgrade --requirement "$REQUIREMENTS_FILE"

echo "[termux-setup] Dependencies installed successfully."
