package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Layr-Labs/eigenda/common"
	"github.com/Layr-Labs/eigenda/litt/util"
	"github.com/Layr-Labs/eigensdk-go/logging"
	"github.com/urfave/cli/v2"
)

func syncCommand(ctx *cli.Context) error {
	if ctx.NArg() < 1 {
		return fmt.Errorf("not enough arguments provided, must provide USER@HOST")
	}

	logger, err := common.NewLogger(common.DefaultConsoleLoggerConfig())
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}

	sources := ctx.StringSlice("src")
	if len(sources) == 0 {
		return fmt.Errorf("no sources provided")
	}
	for i, src := range sources {
		var err error
		sources[i], err = util.SanitizePath(src)
		if err != nil {
			return fmt.Errorf("invalid source path: %s", src)
		}
	}

	destinations := ctx.StringSlice("dest")
	if len(destinations) == 0 {
		return fmt.Errorf("no destinations provided")
	}

	userHost := ctx.Args().First()
	parts := strings.Split(userHost, "@")
	if len(parts) != 2 {
		return fmt.Errorf("invalid USER@HOST format: %s", userHost)
	}
	user := parts[0]
	host := parts[1]

	port := ctx.Uint64("port")

	keyPath := ctx.String("key")
	keyPath, err = util.SanitizePath(keyPath)
	if err != nil {
		return fmt.Errorf("invalid key path: %s", keyPath)
	}

	deleteAfterTransfer := !ctx.Bool("no-gc")
	threads := ctx.Uint64("threads")
	verbose := !ctx.Bool("quiet")
	throttleMB := ctx.Float64("throttle")
	periodSeconds := ctx.Int64("period")
	period := time.Duration(periodSeconds) * time.Second

	maxAgeSeconds := ctx.Uint64("max-age")
	remoteLittBinary := ctx.String("litt-binary")

	knownHostsFile := ctx.String(knownHostsFileFlag.Name)
	knownHostsFile, err = util.SanitizePath(knownHostsFile)
	if err != nil {
		return fmt.Errorf("invalid known hosts path: %s", knownHostsFileFlag.Name)
	}

	return newSyncEngine(
		context.Background(),
		logger,
		sources,
		destinations,
		user,
		host,
		port,
		keyPath,
		knownHostsFile,
		deleteAfterTransfer,
		true,
		threads,
		throttleMB,
		period,
		maxAgeSeconds,
		remoteLittBinary,
		verbose).run()
}

// A utility that periodically transfers data from a local database to a remote backup using rsync.
type syncEngine struct {
	ctx                 context.Context
	cancel              context.CancelFunc
	logger              logging.Logger
	sources             []string
	destinations        []string
	user                string
	host                string
	port                uint64
	keyPath             string
	knownHostsFile      string
	deleteAfterTransfer bool
	fsync               bool
	threads             uint64
	throttleMB          float64
	period              time.Duration
	maxAgeSeconds       uint64
	remoteLittBinary    string
	verbose             bool
}

// newSyncEngine creates a new syncEngine instance with the provided parameters.
func newSyncEngine(
	ctx context.Context,
	logger logging.Logger,
	sources []string,
	destinations []string,
	user string,
	host string,
	port uint64,
	keyPath string,
	knownHostsFile string,
	deleteAfterTransfer bool,
	fsync bool,
	threads uint64,
	throttleMB float64,
	period time.Duration,
	maxAgeSeconds uint64,
	remoteLittBinary string,
	verbose bool,
) *syncEngine {

	ctx, cancel := context.WithCancel(ctx)

	return &syncEngine{
		ctx:                 ctx,
		cancel:              cancel,
		logger:              logger,
		sources:             sources,
		destinations:        destinations,
		user:                user,
		host:                host,
		port:                port,
		keyPath:             keyPath,
		knownHostsFile:      knownHostsFile,
		deleteAfterTransfer: deleteAfterTransfer,
		fsync:               fsync,
		threads:             threads,
		throttleMB:          throttleMB,
		period:              period,
		maxAgeSeconds:       maxAgeSeconds,
		remoteLittBinary:    remoteLittBinary,
		verbose:             verbose,
	}
}

// run the sync engine. This method blocks until the context is cancelled or an unrecoverable error occurs.
func (s *syncEngine) run() error {
	go s.syncLoop()

	// Create a channel to listen for OS signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Wait for signal
	select {
	case <-s.ctx.Done():
		s.logger.Infof("Received shutdown signal, stopping")
	case <-sigChan:
		// Cancel the context when signal is received
		s.cancel()
	}

	return nil
}

// syncLoop is the main loop of the sync engine. It runs indefinitely until the context is cancelled.
func (s *syncEngine) syncLoop() {

	ticker := time.NewTicker(s.period)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.sync()
		}
	}
}

func (s *syncEngine) sync() {
	s.logger.Info("Pushing data to remote.")

	err := push(
		s.logger,
		s.sources,
		s.destinations,
		s.user,
		s.host,
		s.port,
		s.keyPath,
		s.knownHostsFile,
		s.deleteAfterTransfer,
		s.fsync,
		s.threads,
		s.throttleMB,
		s.verbose)

	if err != nil {
		s.logger.Errorf("Push failed: %v", err)
		return
	} else {
		s.logger.Info("Push completed successfully.")
	}

	if s.maxAgeSeconds == 0 {
		s.logger.Info("No max age configured, remote data will not be automatically pruned.")
		return
	}

	s.logger.Infof("Pruning remote data older than %d seconds.", s.maxAgeSeconds)

	command := fmt.Sprintf("%s prune --max-age %d", s.remoteLittBinary, s.maxAgeSeconds)
	sshSession, err := util.NewSSHSession(
		s.logger,
		s.user,
		s.host,
		s.port,
		s.keyPath,
		s.knownHostsFile,
		s.verbose)
	if err != nil {
		s.logger.Errorf("Failed to create SSH session to %s@%s port %d: %v", s.user, s.host, s.port, err)
		return
	}
	defer func() {
		err = sshSession.Close()
		if err != nil {
			s.logger.Errorf("Failed to close SSH session: %v", err)
		}
	}()
	stdout, stderr, err := sshSession.Exec(command)
	if s.verbose {
		s.logger.Infof("prune stdout: %s", stdout)
	}
	if stderr != "" {
		s.logger.Errorf("prune stderr: %s", stderr)
	}

	if err != nil {
		s.logger.Errorf("failed to execute command '%s': %v", command, err)
	}
}

// Stop stops the sync engine by cancelling the context.
func (s *syncEngine) Stop() {
	s.cancel()
}
