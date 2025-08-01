package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sei-protocol/sei-chain/utils2"
	"github.com/spf13/cobra"

	"github.com/sei-protocol/sei-chain/seiload/config"
	"github.com/sei-protocol/sei-chain/seiload/generator"
	"github.com/sei-protocol/sei-chain/seiload/sender"
	"github.com/sei-protocol/sei-chain/seiload/stats"
	"github.com/sei-protocol/sei-chain/utils2/service"
)

var (
	configFile    string
	statsInterval time.Duration
	bufferSize    int
	tps           float64
	dryRun        bool
	debug         bool
	workers       int
	trackReceipts bool
	trackBlocks   bool
	prewarm       bool
)

var rootCmd = &cobra.Command{
	Use:   "seiload",
	Short: "Sei Chain Load Test v2",
	Long: `A load test generator for Sei Chain.

Supports both contract and non-contract scenarios with factory 
and weighted scenario selection mechanisms. Features sharded sending 
to multiple endpoints with account pooling management.

Use --dry-run to test configuration and view transaction details 
without actually sending requests or deploying contracts.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err:=runLoadTest(context.Background(),cmd,args); err != nil {
			log.Fatal(err)
		}
	},
}

func init() {
	rootCmd.Flags().StringVarP(&configFile, "config", "c", "", "Path to configuration file (required)")
	rootCmd.Flags().DurationVarP(&statsInterval, "stats-interval", "s", 10*time.Second, "Interval for logging statistics")
	rootCmd.Flags().IntVarP(&bufferSize, "buffer-size", "b", 1000, "Buffer size per worker")
	rootCmd.Flags().Float64VarP(&tps, "tps", "t", 0, "Transactions per second (0 = no limit)")
	rootCmd.Flags().BoolVarP(&dryRun, "dry-run", "", false, "Mock deployment and requests")
	rootCmd.Flags().BoolVarP(&debug, "debug", "", false, "Log each request")
	rootCmd.Flags().BoolVarP(&trackReceipts, "track-receipts", "", false, "Track receipts")
	rootCmd.Flags().BoolVarP(&trackBlocks, "track-blocks", "", false, "Track blocks")
	rootCmd.Flags().BoolVarP(&prewarm, "prewarm", "", false, "Prewarm accounts with self-transactions")
	rootCmd.Flags().IntVarP(&workers, "workers", "w", 1, "Number of workers")

	if err := rootCmd.MarkFlagRequired("config"); err != nil {
		log.Fatal(err)
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		_, err := fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		if err != nil {
			log.Fatal(err)
		}
		os.Exit(1)
	}
}

func runLoadTest(ctx context.Context, cmd *cobra.Command, args []string) error {
	// Parse the config file into a config.LoadConfig struct
	cfg, err := loadConfig(configFile)
	if err != nil {
		return fmt.Errorf("Failed to load config: %w", err)
	}

	log.Printf("🚀 Starting Sei Chain Load Test v2\n")
	log.Printf("📁 Config file: %s", configFile)
	log.Printf("🎯 Endpoints: %d", len(cfg.Endpoints))
	log.Printf("👥 Workers per endpoint: %d", workers)
	log.Printf("🔧 Total workers: %d", len(cfg.Endpoints)*workers)
	log.Printf("📊 Scenarios: %d", len(cfg.Scenarios))
	log.Printf("⏱️  Stats interval: %v", statsInterval)
	log.Printf("📦 Buffer size per worker: %d", bufferSize)
	if tps > 0 {
		log.Printf("📈 Transactions per second: %.2f", tps)
	}
	if dryRun {
		log.Printf("📝 Dry run: enabled")
	}
	if trackReceipts {
		log.Printf("📝 Track receipts: enabled")
	}
	if trackBlocks {
		log.Printf("📝 Track blocks: enabled")
	}
	if prewarm {
		log.Printf("📝 Prewarm: enabled")
	}

	// Enable mock deployment in dry-run mode
	if dryRun {
		cfg.MockDeploy = true
	}

	// Create statistics collector and logger
	collector := stats.NewCollector()
	logger := stats.NewLogger(collector, statsInterval, debug)

	err = service.Run(ctx, func(ctx context.Context, s service.Scope) error {
		// Create the generator from the config struct
		gen, err := generator.NewConfigBasedGenerator(cfg)
		if err != nil {
			return fmt.Errorf("Failed to create generator: %w", err)
		}

		// Create the sender from the config struct
		snd, err := sender.NewShardedSender(cfg, bufferSize, workers)
		if err != nil {
			return fmt.Errorf("Failed to create sender: %w", err)
		}

		// Create and start block collector if endpoints are available
		var blockCollector *stats.BlockCollector
		if len(cfg.Endpoints) > 0 && trackBlocks {
			blockCollector = stats.NewBlockCollector(cfg.Endpoints[0])
			collector.SetBlockCollector(blockCollector)
			s.SpawnBgNamed("block collector", func() error {
				return blockCollector.Run(ctx)
			})
		}

		// Enable dry-run mode in sender if specified
		if dryRun {
			snd.SetDryRun(true)
		}
		if debug {
			snd.SetDebug(true)
		}
		if trackReceipts {
			snd.SetTrackReceipts(true)
		}
		if trackBlocks {
			snd.SetTrackBlocks(true)
		}

		// Set statistics collector for sender and its workers
		snd.SetStatsCollector(collector, logger)

		// Create dispatcher
		dispatcher := sender.NewDispatcher(gen, snd)
		if tps > 0 {
			// Convert TPS to interval: 1/tps seconds = (1/tps) * 1e9 nanoseconds
			intervalNs := int64((1.0 / tps) * 1e9)
			dispatcher.SetRateLimit(time.Duration(intervalNs))
		}

		// Set statistics collector for dispatcher
		dispatcher.SetStatsCollector(collector, logger)

		// Set up prewarming if enabled
		if prewarm {
			log.Printf("🔥 Creating prewarm generator...")
			prewarmGen := generator.NewPrewarmGenerator(cfg, gen)
			dispatcher.SetPrewarmGenerator(prewarmGen)
			log.Printf("✅ Prewarm generator ready")
			log.Printf("📝 Prewarm mode: Accounts will be prewarmed\n")
		}

		// Start the sender (starts all workers)
		s.SpawnBgNamed("sender", func() error { return snd.Run(ctx) })
		log.Printf("✅ Connected to %d endpoints", snd.GetNumShards())

		// Start block collector if enabled
		if trackBlocks {
			blockCollector = stats.NewBlockCollector(cfg.Endpoints[0])
			collector.SetBlockCollector(blockCollector)
			s.SpawnBgNamed("block collector", func() error { return blockCollector.Run(ctx) })
			log.Printf("✅ Started block collector")
		}

		// Perform prewarming if enabled (before starting logger to avoid logging prewarm transactions)
		if prewarm {
			if err := dispatcher.Prewarm(ctx); err != nil {
				return fmt.Errorf("Failed to prewarm accounts: %w", err)
			}
		}

		// Start logger (after prewarming to capture only main load test metrics)
		s.SpawnBgNamed("logger", func() error { return logger.Run(ctx) })
		log.Printf("✅ Started statistics logger")

		// Start dispatcher for main load test
		s.SpawnBgNamed("dispatcher", func() error { return dispatcher.Run(ctx) })
		log.Printf("✅ Started dispatcher")

		// Set up signal handling for graceful shutdown
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		log.Printf("📈 Logging statistics every %v (Press Ctrl+C to stop)", statsInterval)
		if dryRun {
			log.Printf("📝 Dry-run mode: Simulating requests without sending")
		}
		if debug {
			log.Printf("🐛 Debug mode: Each transaction will be logged")
		}
		if trackReceipts {
			log.Printf("📝 Track receipts mode: Receipts will be tracked")
		}
		if trackBlocks {
			log.Printf("📝 Track blocks mode: Block data will be collected")
		}
		log.Printf(strings.Repeat("=", 60))

		// Main loop - wait for shutdown signal
		if _,err:=utils.Recv(ctx, sigChan); err!=nil {
			return err
		}
		log.Print("\n🛑 Received shutdown signal, stopping gracefully...")
		return nil
	})
	// Print final statistics
	logger.LogFinalStats()
	log.Printf("👋 Shutdown complete")
	return err
}

// loadConfig reads and parses the configuration file
func loadConfig(filename string) (*config.LoadConfig, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg config.LoadConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config JSON: %w", err)
	}

	// Validate configuration
	if len(cfg.Endpoints) == 0 {
		return nil, fmt.Errorf("no endpoints specified in config")
	}

	if len(cfg.Scenarios) == 0 {
		return nil, fmt.Errorf("no scenarios specified in config")
	}

	return &cfg, nil
}
