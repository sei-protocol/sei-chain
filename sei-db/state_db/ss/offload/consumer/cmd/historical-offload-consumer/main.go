package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	commonmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/offload/consumer"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <config.json>\n", os.Args[0])
		os.Exit(2)
	}

	cfg, err := consumer.LoadConfig(os.Args[1])
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Install the Prometheus MeterProvider before opening the sink so the
	// backend cost metrics it registers bind to a real exporter.
	if cfg.MetricsAddr != "" {
		reg, shutdown, err := commonmetrics.SetupOtelPrometheus()
		if err != nil {
			log.Fatalf("setup metrics: %v", err)
		}
		defer func() {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			_ = shutdown(shutdownCtx)
		}()
		commonmetrics.StartMetricsServer(ctx, reg, cfg.MetricsAddr)
		log.Printf("serving consumer metrics at %s/metrics", cfg.MetricsAddr)
	}

	sink, err := consumer.NewBigtableSink(cfg.Bigtable)
	if err != nil {
		log.Fatalf("open bigtable sink: %v", err)
	}
	defer func() { _ = sink.Close() }()

	reader, err := consumer.NewKafkaReader(cfg.Kafka)
	if err != nil {
		log.Fatalf("open kafka reader: %v", err)
	}
	defer func() { _ = reader.Close() }()

	c := consumer.New(reader, sink, consumer.Options{
		Logf:            func(format string, args ...interface{}) { log.Printf(format, args...) },
		Workers:         cfg.Workers,
		ShardBufferSize: cfg.ShardBufferSize,
		MaxBatchRecords: cfg.MaxBatchRecords,
		BatchMaxWait:    time.Duration(cfg.BatchMaxWaitMS) * time.Millisecond,
	})
	if err := c.Run(ctx); err != nil {
		log.Fatalf("consumer: %v", err)
	}
}
