"""Test configuration for attribution helpers."""
from __future__ import annotations

import sys
from pathlib import Path

# Ensure repository root is importable without installing the package.
ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))
