import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

from holo_protocol.soulkey import SoulKeyManager
from holo_protocol.totp import generate_totp_secret


class SoulKeyTests(unittest.TestCase):
    def test_create_and_load_profile(self) -> None:
        with TemporaryDirectory() as tmp:
            path = Path(tmp) / "profile.json"
            manager = SoulKeyManager(profile_path=path)
            secret = generate_totp_secret()
            created = manager.create("sei1alice", secret, label="alice@holo")
            self.assertTrue(path.exists())
            loaded = manager.load()
            self.assertEqual(loaded.address, "sei1alice")
            self.assertEqual(loaded.label, "alice@holo")
            self.assertEqual(loaded.totp_secret, secret)
            self.assertEqual(loaded.soulkey, created.soulkey)

    def test_save_creates_parent_directory(self) -> None:
        with TemporaryDirectory() as tmp:
            path = Path(tmp) / "nested" / "profile.json"
            manager = SoulKeyManager(profile_path=path)
            secret = generate_totp_secret()
            manager.create("sei1bob", secret)
            self.assertTrue(path.exists())
            loaded = manager.load()
            self.assertEqual(loaded.address, "sei1bob")


if __name__ == "__main__":
    unittest.main()
