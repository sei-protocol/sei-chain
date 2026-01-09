// +build darwin

package ante_test

import (
    "os"
    "runtime"
    "testing"
)

func TestKeychainAccess(t *testing.T) {
    if runtime.GOOS == "darwin" && os.Getenv("CI") == "true" {
        t.Skip("Skipping keychain tests on CI macOS")
    }

    // Original keychain test logic here
}
