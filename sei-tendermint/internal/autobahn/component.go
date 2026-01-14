package stream

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"

	"github.com/sei-protocol/sei-stream/config"
	"github.com/sei-protocol/sei-stream/data"
	"github.com/sei-protocol/sei-stream/execute"
	"github.com/sei-protocol/sei-stream/pkg/grpcutils"
	"github.com/sei-protocol/sei-stream/pkg/service"
	"github.com/sei-protocol/sei-stream/pkg/utils"
	"github.com/sei-protocol/sei-stream/storage/stores/ledger"
	"github.com/tendermint/tendermint/internal/autobahn/consensus"
	"github.com/tendermint/tendermint/internal/autobahn/producer"
	"github.com/tendermint/tendermint/internal/autobahn/types"
)

// Run runs the stream node component.
func Run(
	ctx context.Context,
	cfg *config.Config,
	key types.SecretKey,
	viewTimeout consensus.ViewTimeoutFunc,
	reg *prometheus.Registry,
) error {
	committee, err := cfg.StreamConfig.Committee()
	if err != nil {
		return fmt.Errorf("cfg.StreamConfig.Committee(): %w", err)
	}

	return service.Run(ctx, func(ctx context.Context, s service.Scope) error {
		var blockStore utils.Option[data.BlockStore]
		if cfg.ExecuteConfig != nil {
			if dbPath, ok := cfg.ExecuteConfig.BlockDBPath.Get(); ok {
				bs, err := ledger.NewBlockStore(&ledger.BlockStoreConfig{
					BlockDBPath:     dbPath,
					BlockDBTTL:      cfg.ExecuteConfig.BlockDBTTL,
					DisableBlockWAL: cfg.ExecuteConfig.DisableBlockWAL,
					DisableBlockDB:  cfg.ExecuteConfig.DisableBlockDB,
				})
				if err != nil {
					return fmt.Errorf("stores.NewBlockStore(): %w", err)
				}
				blockStore = utils.Some[data.BlockStore](bs)
			}
		}
		dataState := data.NewState(&data.Config{
			Committee: committee,
			PruneAfter: utils.MapOpt(
				cfg.StreamConfig.PruneAfter,
				func(d utils.Duration) time.Duration { return d.Duration() },
			),
		}, blockStore)
		if err := reg.Register(dataState); err != nil {
			return fmt.Errorf("reg.Register(): %w", err)
		}

		if txsPerSec, ok := cfg.StreamConfig.MockExecutorTxsPerSecond.Get(); ok {
			s.SpawnNamed("MockExecutorComponent", func() error {
				limiter := utils.NewLimiter(txsPerSec, txsPerSec)
				s.SpawnBg(func() error { return limiter.Run(ctx) })
				var hash utils.Hash
				for n := types.GlobalBlockNumber(0); ; n += 1 {
					b, err := dataState.GlobalBlock(ctx, n)
					if err != nil {
						return fmt.Errorf("dataState.GlobalBlock(): %w", err)
					}
					if err := limiter.Acquire(ctx, uint64(len(b.Payload.Txs()))); err != nil {
						return err
					}
					// TODO(gprusak): for now we just hash the payloads. We should hash the whole global block.
					ph := b.Payload.Hash()
					hash = sha256.Sum256(append(hash[:], ph[:]...))
					if err := dataState.PushAppHash(b.GlobalNumber, types.AppHash(slices.Clone(hash[:]))); err != nil {
						return fmt.Errorf("dataState.PushAppProposal(): %w", err)
					}
				}
			})
		} else {
			// start internal executor and subscription
			s.SpawnNamed("ExecutorComponent", func() error {
				execComponent, err := execute.NewComponent(ctx, cfg.ChainID, cfg.ExecuteConfig, nil, nil, dataState, true)
				if err != nil {
					return fmt.Errorf("execute.NewComponent(): %w", err)
				}
				if err := reg.Register(execComponent); err != nil {
					return fmt.Errorf("reg.Register(): %w", err)
				}
				return execComponent.Run(ctx)
			})
		}

		s.SpawnNamed("dataState", func() error { return dataState.Run(ctx) })
		s.SpawnNamed("dataClient", func() error { return dataState.RunClientPool(ctx, cfg.StreamConfig.Peers) })
		server := grpcutils.NewServer()
		dataState.Register(server)

		// The consensus will be running iff the node is supposed to be online.
		if online, err := cfg.StreamConfig.IsOnline(key.Public()); err != nil {
			return fmt.Errorf("cfg.StreamConfig.IsOnline(): %w", err)
		} else if online {
			consensusState := consensus.NewState(&consensus.Config{
				Key:         key,
				ViewTimeout: viewTimeout,
			}, dataState)
			consensusState.Register(server)
			s.SpawnNamed("consensusState", func() error { return consensusState.Run(ctx) })
			s.SpawnNamed("consensusClient", func() error { return consensusState.RunClientPool(ctx, cfg.StreamConfig.Peers) })
			if err := reg.Register(consensusState); err != nil {
				return fmt.Errorf("reg.Register(): %w", err)
			}

			producerState := producer.NewState(&producer.Config{
				MaxGasPerBlock: 350_000_000,
				MaxTxsPerBlock: 3000,
				MaxTxsPerSecond: utils.MapOpt(
					cfg.StreamConfig.TxsPerSecondCap,
					func(c uint64) uint64 {
						// Scale the global cap by the number of peers.
						return c / uint64(len(cfg.StreamConfig.Peers))
					},
				),
				//TODO: figure out how deep these really need to be
				MempoolSize:      100000,
				BlockInterval:    200 * time.Millisecond,
				AllowEmptyBlocks: false,
			}, consensusState)
			s.SpawnNamed("producerState", func() error { return producerState.Run(ctx) })
			producerState.Register(server)
		} else {
			log.Info().Msg("Consensus compontent is not running, because the node is configured to be offline")
		}
		s.SpawnNamed("server", func() error {
			return grpcutils.RunServer(ctx, server, cfg.StreamConfig.ServerAddr)
		})
		return nil
	})
}
