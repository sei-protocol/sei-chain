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

	"github.com/sei-protocol/sei-chain/loadtest_v2/config"
	"github.com/sei-protocol/sei-chain/loadtest_v2/generator"
	"github.com/sei-protocol/sei-chain/loadtest_v2/sender"
)

var (
	configFile    string
	statsInterval time.Duration
	bufferSize    int
	rateLimit     time.Duration
)

var rootCmd = &cobra.Command{
	Use:   "seiload",
	Short: "Sei Chain Load Test v2",
	Long: `A flexible, modular load test scenario generator for Sei Chain.

Supports both contract and non-contract scenarios with robust factory 
and weighted scenario selection mechanisms. Features sharded sending 
to multiple endpoints with proper account pooling management.`,
	Run: runLoadTest,
}

func init() {
	rootCmd.Flags().StringVarP(&configFile, "config", "c", "", "Path to configuration file (required)")
	rootCmd.Flags().DurationVarP(&statsInterval, "stats-interval", "s", 10*time.Second, "Interval for logging statistics")
	rootCmd.Flags().IntVarP(&bufferSize, "buffer-size", "b", 100, "Buffer size per worker")
	rootCmd.Flags().DurationVarP(&rateLimit, "rate-limit", "r", 0, "Rate limit between transactions (0 = no limit)")

	rootCmd.MarkFlagRequired("config")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
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
	fmt.Printf("📊 Scenarios: %d\n", len(cfg.Scenarios))
	fmt.Printf("⏱️  Stats interval: %v\n", statsInterval)
	fmt.Printf("📦 Buffer size per worker: %d\n", bufferSize)
	if rateLimit > 0 {
		fmt.Printf("🐌 Rate limit: %v\n", rateLimit)
	}
	fmt.Println()

	// Create the generator from the config struct
	gen, err := generator.NewConfigBasedGenerator(cfg)
	if err != nil {
		log.Fatalf("Failed to create generator: %v", err)
	}

	// Create the sender from the config struct
	snd, err := sender.NewShardedSender(cfg, bufferSize)
	if err != nil {
		log.Fatalf("Failed to create sender: %v", err)
	}

	// Create dispatcher
	dispatcher := sender.NewDispatcher(gen, snd)
	if rateLimit > 0 {
		dispatcher.SetRateLimit(rateLimit)
	}

	// Start the sender (starts all workers)
	snd.Start()
	fmt.Printf("✅ Started %d workers\n", snd.GetNumShards())

	// Start the dispatcher
	dispatcher.Start()
	fmt.Printf("✅ Started transaction dispatcher\n")

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start periodic statistics logging
	statsTicker := time.NewTicker(statsInterval)
	defer statsTicker.Stop()

	fmt.Printf("📈 Logging statistics every %v (Press Ctrl+C to stop)\n", statsInterval)
	fmt.Println(strings.Repeat("=", 60))

	// Main loop - periodically log statistics until signal received
	for {
		select {
		case <-sigChan:
			fmt.Println("\n🛑 Received shutdown signal, stopping gracefully...")

			// Stop dispatcher first
			dispatcher.Stop()
			fmt.Println("✅ Stopped dispatcher")

			// Stop sender and all workers
			snd.Stop()
			fmt.Println("✅ Stopped sender and workers")

			// Print final statistics
			printFinalStats(dispatcher, snd)

			fmt.Println("👋 Shutdown complete")
			return

		case <-statsTicker.C:
			// Log statistics in a non-disruptive way
			logStatistics(dispatcher, snd)
		}
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

// logStatistics prints current statistics in a clean, non-disruptive format
func logStatistics(dispatcher *sender.Dispatcher, snd *sender.ShardedSender) {
	dispatcherStats := dispatcher.GetStats()
	workerStats := snd.GetWorkerStats()

	// Calculate total pending transactions across all workers
	totalPending := 0
	for _, stat := range workerStats {
		totalPending += stat.ChannelLength
	}

	// Print compact statistics line
	fmt.Printf("[%s] 📊 Sent: %d | Pending: %d | Workers: %d\n",
		time.Now().Format("15:04:05"),
		dispatcherStats.TotalSent,
		totalPending,
		len(workerStats))

	// Optionally show per-worker details if there are pending transactions
	if totalPending > 0 {
		fmt.Printf("         Worker details: ")
		for i, stat := range workerStats {
			if i > 0 {
				fmt.Printf(", ")
			}
			fmt.Printf("W%d:%d", stat.WorkerID, stat.ChannelLength)
		}
		fmt.Println()
	}
}

// printFinalStats shows detailed final statistics
func printFinalStats(dispatcher *sender.Dispatcher, snd *sender.ShardedSender) {
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("📈 FINAL STATISTICS")
	fmt.Println(strings.Repeat("=", 60))

	dispatcherStats := dispatcher.GetStats()
	workerStats := snd.GetWorkerStats()

	fmt.Printf("Total transactions sent: %d\n", dispatcherStats.TotalSent)
	fmt.Printf("Number of workers: %d\n", len(workerStats))

	fmt.Println("\nWorker details:")
	totalPending := 0
	for _, stat := range workerStats {
		fmt.Printf("  Worker %d: %d pending, endpoint: %s\n",
			stat.WorkerID, stat.ChannelLength, stat.Endpoint)
		totalPending += stat.ChannelLength
	}

	if totalPending > 0 {
		fmt.Printf("\n⚠️  Warning: %d transactions still pending in worker queues\n", totalPending)
	}

	fmt.Println(strings.Repeat("=", 60))
}
