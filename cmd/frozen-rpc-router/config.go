package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	defaultListenAddress      = "127.0.0.1:8545"
	defaultMaxRequestBodySize = int64(5 << 20)
	defaultShutdownTimeout    = 10 * time.Second
)

type config struct {
	listenAddress      string
	liveNode           string
	frozenNodes        frozenNodeFlags
	maxRequestBodySize int64
	shutdownTimeout    time.Duration
}

type frozenNodeConfig struct {
	freezeHeight uint64
	address      string
}

type frozenNodeFlags []string

func (f *frozenNodeFlags) String() string {
	return strings.Join(*f, ",")
}

func (f *frozenNodeFlags) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func parseConfig(args []string, output io.Writer) (config, error) {
	cfg := config{}
	flags := flag.NewFlagSet("frozen-rpc-router", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.StringVar(&cfg.listenAddress, "listen-address", defaultListenAddress, "address on which the router listens")
	flags.StringVar(&cfg.liveNode, "live-node", "", "HTTP RPC address of the live node (required)")
	flags.Var(&cfg.frozenNodes, "frozen-node", "freeze-height=ip:port pair; repeat once per frozen node")
	flags.Int64Var(&cfg.maxRequestBodySize, "max-request-body-bytes", defaultMaxRequestBodySize, "maximum JSON-RPC request body size")
	flags.DurationVar(&cfg.shutdownTimeout, "shutdown-timeout", defaultShutdownTimeout, "graceful shutdown timeout")
	if err := flags.Parse(args); err != nil {
		return config{}, err
	}
	if flags.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	if strings.TrimSpace(cfg.liveNode) == "" {
		return config{}, errors.New("--live-node is required")
	}
	if cfg.maxRequestBodySize <= 0 {
		return config{}, errors.New("--max-request-body-bytes must be positive")
	}
	if cfg.shutdownTimeout <= 0 {
		return config{}, errors.New("--shutdown-timeout must be positive")
	}
	return cfg, nil
}

func parseFrozenNodes(values frozenNodeFlags) ([]frozenNodeConfig, error) {
	nodes := make([]frozenNodeConfig, 0, len(values))
	for _, value := range values {
		heightText, address, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(heightText) == "" || strings.TrimSpace(address) == "" {
			return nil, fmt.Errorf("invalid frozen node %q: expected freeze-height=ip:port", value)
		}
		freezeHeight, err := strconv.ParseUint(strings.TrimSpace(heightText), 10, 64)
		if err != nil || freezeHeight == 0 || freezeHeight > math.MaxInt64 {
			return nil, fmt.Errorf("invalid freeze height %q", heightText)
		}
		nodes = append(nodes, frozenNodeConfig{
			freezeHeight: freezeHeight,
			address:      strings.TrimSpace(address),
		})
	}
	return nodes, nil
}
