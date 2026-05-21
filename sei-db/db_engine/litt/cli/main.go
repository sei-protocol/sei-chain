package main

import (
	"log/slog"
	"os"
)

// main is the entry point for the LittDB cli.
func main() {
	logger := slog.Default()

	err := buildCLIParser(logger).Run(os.Args)
	if err != nil {
		logger.Error("Execution failed", "error", err)
		os.Exit(1)
	}
}
