class SeiwordTrigger:
    """Keyword-based trigger for security events."""

    def __init__(self, keywords=None):
        self.keywords = keywords or ["sei"]

    def monitor(self):
        """Placeholder monitor implementation."""
        print(f"Monitoring for keywords: {', '.join(self.keywords)}")
