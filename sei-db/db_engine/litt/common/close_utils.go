package common

import (
	"fmt"
	"io"
	"log/slog"
)

func CloseLogOnError(c io.Closer, name string, log *slog.Logger) {
	if closeErr := c.Close(); closeErr != nil {
		if log != nil {
			log.Error(fmt.Sprintf("failed to close %s: %s", name, closeErr.Error()))
		} else {
			fmt.Printf("failed to close %s: %s", name, closeErr.Error())
		}
	}
}
