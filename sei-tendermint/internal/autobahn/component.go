package autobahn 

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"

	"github.com/tendermint/tendermint/internal/autobahn/data"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/scope"
	"github.com/tendermint/tendermint/internal/autobahn/consensus"
	"github.com/tendermint/tendermint/internal/autobahn/producer"
	"github.com/tendermint/tendermint/internal/autobahn/types"
	"golang.org/x/time/rate"
)

// Run runs the stream node component.
func Run(
	ctx context.Context,
	cfg *Config,
	key types.SecretKey,
	viewTimeout consensus.ViewTimeoutFunc,
	reg *prometheus.Registry,
) error {
	committee, err := cfg.Committee()
	if err != nil {
		return fmt.Errorf("cfg.Config.Committee(): %w", err)
	}

	return scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		var blockStore utils.Option[data.BlockStore]
		dataState := data.NewState(&data.Config{
			Committee: committee,
			PruneAfter: utils.MapOpt(
				cfg.PruneAfter,
				func(d utils.Duration) time.Duration { return d.Duration() },
			),
		}, blockStore)
		if err := reg.Register(dataState); err != nil {
			return fmt.Errorf("reg.Register(): %w", err)
		}

		s.SpawnNamed("MockExecutor", func() error {
			txsPerSec := cfg.MockExecutorTxsPerSecond
			limiter := rate.NewLimiter(rate.Limit(txsPerSec), int(txsPerSec))
			var hash [sha256.Size]byte
			for n := types.GlobalBlockNumber(0); ; n += 1 {
				b, err := dataState.GlobalBlock(ctx, n)
				if err != nil {
					return fmt.Errorf("dataState.GlobalBlock(): %w", err)
				}
				if err := limiter.WaitN(ctx, len(b.Payload.Txs())); err != nil {
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

		s.SpawnNamed("dataState", func() error { return dataState.Run(ctx) })
		s.SpawnNamed("dataClient", func() error { return dataState.RunClientPool(ctx, cfg.Peers) })
		server := grpcutils.NewServer()
		dataState.Register(server)

		// The consensus will be running iff the node is supposed to be online.
		if online, err := cfg.IsOnline(key.Public()); err != nil {
			return fmt.Errorf("cfg.Config.IsOnline(): %w", err)
		} else if online {
			consensusState := consensus.NewState(&consensus.Config{
				Key:         key,
				ViewTimeout: viewTimeout,
			}, dataState)
			consensusState.Register(server)
			s.SpawnNamed("consensusState", func() error { return consensusState.Run(ctx) })
			s.SpawnNamed("consensusClient", func() error { return consensusState.RunClientPool(ctx, cfg.Peers) })
			if err := reg.Register(consensusState); err != nil {
				return fmt.Errorf("reg.Register(): %w", err)
			}

			producerState := producer.NewState(&producer.Config{
				MaxGasPerBlock: 350_000_000,
				MaxTxsPerBlock: 3000,
				MaxTxsPerSecond: utils.MapOpt(
					cfg.TxsPerSecondCap,
					func(c uint64) uint64 {
						// Scale the global cap by the number of peers.
						return c / uint64(len(cfg.Peers))
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
			return grpcutils.RunServer(ctx, server, cfg.ServerAddr)
		})
		return nil
	})
}
