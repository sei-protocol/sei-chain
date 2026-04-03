package util

// Docker-based SSH tests have been removed.
// Tests (TestSSHSession_NewSSHSession, TestSSHSession_Mkdirs,
// TestSSHSession_FindFiles, TestSSHSession_Rsync) required Docker containers
// via SetupSSHTestContainer. They were already t.Skip()'d as flaky.
// Re-enable if Docker-based SSH integration testing becomes important in this repo.
