// configure-logger reads a cryptosim config file and prints shell export
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

	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/cryptosim"
)

/*
This extra binary for setting up logging is an unfortunate complexity, but we can't really avoid it
given that the only way to configure logging is to set environment variables before the main process starts.
*/

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "configure-logger: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: configure-logger <config-file>")
	}

	cfg, err := cryptosim.LoadConfigFromFile(os.Args[1])
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if cfg.LogDir == "" {
		return fmt.Errorf("LogDir is empty, refusing to proceed")
	}

	if cfg.DeleteLogDirOnStartup {
		resolved, err := filepath.Abs(cfg.LogDir)
		if err != nil {
			return fmt.Errorf("failed to resolve log directory: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Deleting log directory: %s\n", resolved)
		if err := os.RemoveAll(resolved); err != nil {
			return fmt.Errorf("failed to delete log directory %s: %w", resolved, err)
		}
	}

	logDir, err := cryptosim.ResolveAndCreateDir(cfg.LogDir)
	if err != nil {
		return fmt.Errorf("resolve log dir: %w", err)
	}

	logFile := filepath.Join(logDir, "cryptosim.log")

	fmt.Printf("export SEI_LOG_OUTPUT=%s\n", shellQuote(logFile))
	fmt.Printf("export SEI_LOG_LEVEL=%s\n", shellQuote(strings.ToLower(cfg.LogLevel)))

	return nil
}

// shellQuote wraps s in single quotes, escaping any embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
