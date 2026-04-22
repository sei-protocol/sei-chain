package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

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

	sink, err := consumer.NewCockroachSink(cfg.Cockroach)
	if err != nil {
		log.Fatalf("open cockroach sink: %v", err)
	}
	defer sink.Close()

	reader, err := consumer.NewKafkaReader(cfg.Kafka)
	if err != nil {
		log.Fatalf("open kafka reader: %v", err)
	}
	defer reader.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	c := consumer.New(reader, sink, consumer.Options{
		Logf: func(format string, args ...interface{}) { log.Printf(format, args...) },
	})
	if err := c.Run(ctx); err != nil {
		log.Fatalf("consumer: %v", err)
	}
}
