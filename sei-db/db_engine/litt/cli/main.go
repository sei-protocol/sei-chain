package main

import (
	"fmt"
	"os"

	"github.com/Layr-Labs/eigenda/common"
)

// main is the entry point for the LittDB cli.
func main() {
	logger, err := common.NewLogger(common.DefaultConsoleLoggerConfig())
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Failed to create logger: %v\n", err)
		os.Exit(1)
	}

	err = buildCLIParser(logger).Run(os.Args)
	if err != nil {
		logger.Errorf("Execution failed: %v\n", err)
		os.Exit(1)
	}
}
