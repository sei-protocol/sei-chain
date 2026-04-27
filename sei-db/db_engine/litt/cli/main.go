//go:build littdb_wip

package main

import (
	"fmt"
	"log/slog"
	"os"
)

// main is the entry point for the LittDB cli.
func main() {
	logger := slog.Default()

	err := buildCLIParser(logger).Run(os.Args)
	if err != nil {
		logger.Error(fmt.Sprintf("Execution failed: %v\n", err))
		os.Exit(1)
	}
}
