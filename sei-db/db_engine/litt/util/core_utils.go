//go:build littdb_wip

package util

import (
	"fmt"
	"io"
	"log/slog"
)

// CloseLogOnError attempts to close the given io.Closer and logs an error if it fails.
// Meant to be called in a defer statement: defer CloseLogOnError(c, "nameOfResourceToClose", log).
func CloseLogOnError(c io.Closer, name string, log *slog.Logger) {
	if closeErr := c.Close(); closeErr != nil {
		if log != nil {
			log.Error("failed to close", "name", name, "error", closeErr)
		} else {
			fmt.Printf("failed to close %s: %s", name, closeErr.Error())
		}
	}
}
