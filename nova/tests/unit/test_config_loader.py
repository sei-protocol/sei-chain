from pathlib import Path

import pytest

from nova.config.loader import load_config


def test_load_config(tmp_path: Path):
    config_file = tmp_path / "profile.yaml"
    config_file.write_text(
        """
wallet:
  address: sei1test
chain:
  id: pacific-1
  rpc: https://rpc
strategy:
  buffer: 1000
  validators:
    - val1
  schedule:
    interval_minutes: 60
    jitter_seconds: 10
"""
    )

    config = load_config(config_file)
    assert config.wallet.address == "sei1test"
    assert config.chain.id == "pacific-1"
    assert config.strategy.buffer == 1000


def test_load_config_missing_file(tmp_path: Path):
    with pytest.raises(FileNotFoundError):
        load_config(tmp_path / "missing.yaml")
