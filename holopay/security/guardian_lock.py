class GuardianLock:
    """Simple guardian lock mechanism."""

    def __init__(self):
        self.enabled = False

    def enable(self):
        """Enable the guardian lock."""
        self.enabled = True
        print("Guardian lock enabled")
