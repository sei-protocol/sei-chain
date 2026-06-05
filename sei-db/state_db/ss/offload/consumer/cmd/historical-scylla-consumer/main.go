package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	sink, err := consumer.NewSinkFromConfig(*cfg)
	if err != nil {
		log.Fatalf("open %s sink: %v", cfg.BackendName(), err)
	}
	defer func() { _ = sink.Close() }()

	reader, err := consumer.NewKafkaReader(cfg.Kafka)
	if err != nil {
		log.Fatalf("open kafka reader: %v", err)
	}
	defer func() { _ = reader.Close() }()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

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
