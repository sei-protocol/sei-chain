//go:build littdb_wip

package main

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable"
	"github.com/urfave/cli/v2"
)

// called by the CLI to unlock a LittDB file system.
func unlockCommand(ctx *cli.Context) error {
	logger := slog.Default()
	sources := ctx.StringSlice(srcFlag.Name)

	if len(sources) == 0 {
		return fmt.Errorf("at least one source path is required")
	}

	force := ctx.Bool(forceFlag.Name)
	if !force {
		magicString := "I know what I am doing"
		logger.Warn("About to delete LittDB lock files. This is potentially dangerous. "+
			"Type the magic string to continue, or use the --force flag.",
			"magic_string", magicString,
		)
		reader := bufio.NewReader(os.Stdin)
		input, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}
		input = strings.TrimSuffix(input, "\n")
		if input != magicString {
			return fmt.Errorf("unlock operation aborted")
		}
	}

	err := disktable.Unlock(logger, sources)
	if err != nil {
		return fmt.Errorf("failed to unlock LittDB files: %w", err)
	}
	return nil
}
