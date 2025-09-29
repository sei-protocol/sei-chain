import unittest
from datetime import datetime, timezone

from holo_protocol.totp import generate_totp_secret, provisioning_uri, totp_code


class TotpTests(unittest.TestCase):
    def test_generate_totp_secret_length(self) -> None:
        secret = generate_totp_secret(10)
        self.assertGreaterEqual(len(secret), 16)

    def test_totp_code_matches_reference(self) -> None:
        secret = "JBSWY3DPEHPK3PXP"
        timestamp = int(datetime(2025, 1, 1, tzinfo=timezone.utc).timestamp())
        self.assertEqual(totp_code(secret, timestamp=timestamp, interval=30, digits=6), "768725")

    def test_provisioning_uri(self) -> None:
        secret = "JBSWY3DPEHPK3PXP"
        uri = provisioning_uri(secret, "alice@holo", "Holo")
        self.assertTrue(uri.startswith("otpauth://totp/Holo:alice@holo"))
        self.assertIn(f"secret={secret}", uri)


if __name__ == "__main__":
    unittest.main()
