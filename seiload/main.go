package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"seiload/config"
	"seiload/generator"
	"seiload/sender"
	"seiload/stats"
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
	Run: runLoadTest,
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

func runLoadTest(cmd *cobra.Command, args []string) {
	// Parse the config file into a config.LoadConfig struct
	cfg, err := loadConfig(configFile)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	fmt.Printf("🚀 Starting Sei Chain Load Test v2\n")
	fmt.Printf("📁 Config file: %s\n", configFile)
	fmt.Printf("🎯 Endpoints: %d\n", len(cfg.Endpoints))
	fmt.Printf("👥 Workers per endpoint: %d\n", workers)
	fmt.Printf("🔧 Total workers: %d\n", len(cfg.Endpoints)*workers)
	fmt.Printf("📊 Scenarios: %d\n", len(cfg.Scenarios))
	fmt.Printf("⏱️  Stats interval: %v\n", statsInterval)
	fmt.Printf("📦 Buffer size per worker: %d\n", bufferSize)
	if tps > 0 {
		fmt.Printf("📈 Transactions per second: %.2f\n", tps)
	}
	if dryRun {
		fmt.Printf("📝 Dry run: enabled\n")
	}
	if trackReceipts {
		fmt.Printf("📝 Track receipts: enabled\n")
	}
	if trackBlocks {
		fmt.Printf("📝 Track blocks: enabled\n")
	}
	fmt.Println()

	// Enable mock deployment in dry-run mode
	if dryRun {
		cfg.MockDeploy = true
	}

	// Create the generator from the config struct
	gen, err := generator.NewConfigBasedGenerator(cfg)
	if err != nil {
		log.Fatalf("Failed to create generator: %v", err)
	}

	// Create the sender from the config struct
	snd, err := sender.NewShardedSender(cfg, bufferSize, workers)
	if err != nil {
		log.Fatalf("Failed to create sender: %v", err)
	}

	// Create statistics collector and logger
	collector := stats.NewCollector()
	logger := stats.NewLogger(collector, statsInterval, debug)

	// Create and start block collector if endpoints are available
	var blockCollector *stats.BlockCollector
	if len(cfg.Endpoints) > 0 && trackBlocks {
		blockCollector = stats.NewBlockCollector(cfg.Endpoints[0])
		collector.SetBlockCollector(blockCollector)
		
		// Start block collector
		if err := blockCollector.Start(); err != nil {
			log.Printf("⚠️  Failed to start block collector: %v", err)
		}
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

	// Start the sender (starts all workers)
	snd.Start()
	fmt.Printf("✅ Connected to %d endpoints\n", snd.GetNumShards())

	// Start the dispatcher
	dispatcher.Start()
	fmt.Printf("✅ Started transaction dispatcher\n")

	// Start statistics logger
	logger.Start()
	fmt.Printf("✅ Started statistics logger\n")

	// Show block collector status
	if trackBlocks && blockCollector != nil {
		fmt.Printf("✅ Started block collector\n")
	}

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	fmt.Printf("📈 Logging statistics every %v (Press Ctrl+C to stop)\n", statsInterval)
	if dryRun {
		fmt.Printf("📝 Dry-run mode: Simulating requests without sending\n")
	}
	if debug {
		fmt.Printf("🐛 Debug mode: Each transaction will be logged\n")
	}
	if trackReceipts {
		fmt.Printf("📝 Track receipts mode: Receipts will be tracked\n")
	}
	if trackBlocks {
		fmt.Printf("📝 Track blocks mode: Block data will be collected\n")
	}
	fmt.Println(strings.Repeat("=", 60))

	// Main loop - wait for shutdown signal
	select {
	case <-sigChan:
		fmt.Println("\n🛑 Received shutdown signal, stopping gracefully...")

		// Stop block collector first
		if blockCollector != nil {
			blockCollector.Stop()
			fmt.Println("✅ Stopped block collector")
		}

		// Stop statistics logger first
		logger.Stop()
		fmt.Println("✅ Stopped statistics logger")

		// Stop dispatcher
		dispatcher.Stop()
		fmt.Println("✅ Stopped dispatcher")

		// Stop sender and all workers
		snd.Stop()
		fmt.Println("✅ Stopped sender and workers")

		// Print final statistics
		logger.LogFinalStats()

		fmt.Println("👋 Shutdown complete")
		return
	}
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
