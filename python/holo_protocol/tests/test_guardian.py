import unittest

from holo_protocol.guardian import GuardianEngine, Transaction


class GuardianTests(unittest.TestCase):
    def test_guardian_allows_low_risk(self) -> None:
        engine = GuardianEngine()
        tx = Transaction(
            amount=25.0,
            denom="usei",
            sender="sei1alice",
            recipient="sei1bob",
            location="united_states",
            memo="Lunch",
            risk_tags=[],
        )
        decision = engine.evaluate(tx)
        self.assertEqual(decision.verdict, "allow")
        self.assertEqual(decision.score, 0.0)

    def test_guardian_escalates_high_risk(self) -> None:
        engine = GuardianEngine()
        tx = Transaction(
            amount=12_000.0,
            denom="usei",
            sender="sei1alice",
            recipient="sei1evil",
            location="north_korea",
            memo="Payment for services rendered",
            risk_tags=["new_recipient", "velocity"],
        )
        decision = engine.evaluate(tx)
        self.assertEqual(decision.verdict, "deny")
        self.assertIn("high_amount", decision.reasons)
        self.assertIn("suspicious_location", decision.reasons)


if __name__ == "__main__":
    unittest.main()
