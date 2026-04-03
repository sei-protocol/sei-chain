package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/Layr-Labs/eigenda/common"
	"github.com/Layr-Labs/eigenda/litt/disktable"
	"github.com/urfave/cli/v2"
)

// called by the CLI to unlock a LittDB file system.
func unlockCommand(ctx *cli.Context) error {
	logger, err := common.NewLogger(common.DefaultConsoleLoggerConfig())
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}
	sources := ctx.StringSlice(srcFlag.Name)

	if len(sources) == 0 {
		return fmt.Errorf("at least one source path is required")
	}

	force := ctx.Bool(forceFlag.Name)
	if !force {
		magicString := "I know what I am doing"
		logger.Warnf("About to delete LittDB lock files. This is potentially dangerous. "+
			"Type \"%s\" to continue, or use "+
			"the --force flag.", magicString)
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

	err = disktable.Unlock(logger, sources)
	if err != nil {
		return fmt.Errorf("failed to unlock LittDB files: %w", err)
	}
	return nil
}
