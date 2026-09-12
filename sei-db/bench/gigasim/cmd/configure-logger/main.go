// configure-logger reads a gigasim config file and prints shell export
// statements that configure seilog's environment variables. Intended to be
// called via eval in a shell script before launching the benchmark binary.
//
// Usage:
//
//	eval "$(configure-logger config.json)"
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sei-protocol/sei-chain/sei-db/bench/gigasim"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "configure-logger: %v\n", err)
		os.Exit(1)
	}
}

// run prints the seilog environment for the config file named on the command line.
func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: configure-logger <config-file>")
	}

	cfg := gigasim.DefaultGigasimConfig()
	if err := utils.LoadConfigFromFile(os.Args[1], cfg); err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logDir, err := utils.ResolveAndCreateDir(cfg.LogDir)
	if err != nil {
		return fmt.Errorf("resolve log dir: %w", err)
	}

	logFile := filepath.Join(logDir, "gigasim.log")

	fmt.Printf("export SEI_LOG_OUTPUT=%s\n", shellQuote(logFile))
	fmt.Printf("export SEI_LOG_LEVEL=%s\n", shellQuote(strings.ToLower(cfg.LogLevel)))

	return nil
}

// shellQuote wraps a value in single quotes so a shell reads it literally.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
