package common

import (
	"fmt"
	"io"

	"github.com/Layr-Labs/eigensdk-go/logging"
)

// CloseLogOnError attempts to close the given io.Closer and logs an error if it fails.
// Meant to be called in a defer statement: defer CloseLogOnError(c, "nameOfResourceToClose", log).
func CloseLogOnError(c io.Closer, name string, log logging.Logger) {
	if closeErr := c.Close(); closeErr != nil {
		if log != nil {
			log.Errorf("failed to close %s: %s", name, closeErr.Error())
		} else {
			fmt.Printf("failed to close %s: %s", name, closeErr.Error())
		}
	}
}
