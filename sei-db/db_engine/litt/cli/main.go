package main

import (
	"fmt"
	"os"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

// main is the entry point for the LittDB cli.
func main() {
	logger, err := util.NewLogger(util.DefaultConsoleLoggerConfig())
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Failed to create logger: %v\n", err)
		os.Exit(1)
	}

	err = buildCLIParser(logger).Run(os.Args)
	if err != nil {
		logger.Error(fmt.Sprintf("Execution failed: %v\n", err))
		os.Exit(1)
	}
}
