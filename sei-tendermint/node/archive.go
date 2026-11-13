package node

import (
	"context"
	"fmt"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/sei-db/archive"
	"github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// openArchive starts the history archive exporter configured for an Autobahn node. It returns
// None when no archive is configured.
func openArchive(ctx context.Context, conf *config.Config, cfg gigaconfig.ArchiveConfig) (utils.Option[*archive.Exporter], error) {
	if !cfg.Enabled() || conf.AutobahnConfigFile == "" {
		return utils.None[*archive.Exporter](), nil
	}
	exporter, err := archive.StartExporter(ctx, archive.Config{
		Addr:     cfg.ClickHouseAddr,
		Database: cfg.Database,
		User:     cfg.User,
		Password: cfg.Password,
	})
	if err != nil {
		return utils.None[*archive.Exporter](), fmt.Errorf("start archive exporter: %w", err)
	}
	return utils.Some(exporter), nil
}

// archiveExecutorOptions makes the EVM-only executor hand every block it executes to exporter.
func archiveExecutorOptions(exporter utils.Option[*archive.Exporter]) []evmonly.Option {
	if e, ok := exporter.Get(); ok {
		return []evmonly.Option{evmonly.WithResultSink(e)}
	}
	return nil
}

// closeArchive writes the blocks the exporter still holds and closes it.
func closeArchive(exporter utils.Option[*archive.Exporter]) {
	if e, ok := exporter.Get(); ok {
		e.Close()
	}
}

// withArchive hands the node the exporter it closes on stop.
func withArchive(exporter utils.Option[*archive.Exporter]) Option {
	return func(opts *options) {
		opts.archive = exporter
	}
}
