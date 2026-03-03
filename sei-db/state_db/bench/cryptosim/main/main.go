package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/cryptosim"
)

// Run the cryptosim benchmark.
func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <config-file>\n", os.Args[0])
		os.Exit(1)
	}
	config, err := cryptosim.LoadConfigFromFile(os.Args[1])
	if err != nil {
		return err
	}

	configString, err := config.StringifiedConfig()
	if err != nil {
		return fmt.Errorf("failed to stringify config: %w", err)
	}
	fmt.Printf("%s\n", configString)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cs, err := cryptosim.NewCryptoSim(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create cryptosim: %w", err)
	}
	defer func() {
		err := cs.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error closing cryptosim: %v\n", err)
		}
	}()

	// Toggle suspend/resume on Enter when enabled
	if config.EnableSuspension {
		go func() {
			scanner := bufio.NewScanner(os.Stdin)
			suspended := false
			for scanner.Scan() {
				if suspended {
					cs.Resume()
					suspended = false
				} else {
					cs.Suspend()
					suspended = true
				}
			}
		}()
	}

	cs.BlockUntilHalted()

	return nil
}
