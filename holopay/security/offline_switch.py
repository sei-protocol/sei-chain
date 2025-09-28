class OfflineSwitch:
    """Module to toggle an offline mode."""

    def __init__(self):
        self.offline = False

    def activate(self):
        """Activate offline mode."""
        self.offline = True
        print("Offline switch activated")
