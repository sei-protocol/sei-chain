package main

// Docker-based push tests have been removed.
// Tests (TestPush1to1, TestPush1toN, TestPushNto1, TestPushNtoN,
// TestPushSnapshot) required Docker containers via util.SetupSSHTestContainer.
// They were already t.Skip()'d as flaky.
// Re-enable if Docker-based SSH integration testing becomes important in this repo.
