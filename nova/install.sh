#!/usr/bin/env bash
set -euo pipefail

PROJECT_ROOT=$(cd "$(dirname "$0")" && pwd)
VENV_DIR="$PROJECT_ROOT/.venv"

python3 -m venv "$VENV_DIR"
source "$VENV_DIR/bin/activate"
pip install --upgrade pip
pip install -e "$PROJECT_ROOT"[ml]
pre-commit install || true

echo "Nova installation complete. Activate the environment with:"
echo "source $VENV_DIR/bin/activate"
