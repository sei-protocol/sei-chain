package disktable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Layr-Labs/eigenda/litt/util"
	"github.com/Layr-Labs/eigensdk-go/logging"
)

// Unlocks a LittDB file system.
//
// DANGER: calling this method opens the door for unsafe concurrent operations on LittDB files.
// With great power comes great responsibility.
func Unlock(logger logging.Logger, sourcePaths []string) error {
	for _, sourcePath := range sourcePaths {
		err := filepath.WalkDir(sourcePath, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}

			if strings.HasSuffix(path, util.LockfileName) {
				logger.Infof("Removing lock file %s", path)
				if removeErr := os.Remove(path); removeErr != nil {
					logger.Error("Failed to remove lock file", "path", path, "error", removeErr)
					return fmt.Errorf("failed to remove lock file %s: %w", path, removeErr)
				}
			}

			return nil
		})

		if err != nil {
			return fmt.Errorf("failed to walk directory %s: %w", sourcePath, err)
		}
	}

	return nil
}
