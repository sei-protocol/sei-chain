package producer

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

func TestProducer_LeaveCancelsAndRejoinStartsNewLane(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 2)
	a, b := keys[0], keys[1]

	db := memblock.NewBlockDB()
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	ds, err := data.NewState(&data.Config{Registry: registry}, db)
	require.NoError(t, err)
	consensusState, err := consensus.NewState(&consensus.Config{
		Key:                a,
		ViewTimeout:        func(types.View) time.Duration { return time.Hour },
		PersistentStateDir: utils.None[string](),
	}, ds)
	require.NoError(t, err)

	app := newTestApp()
	cfg := app.Cfg()
	cfg.AllowEmptyBlocks = true
	cfg.BlockInterval = 10 * time.Millisecond
	prod := NewState(cfg, consensusState, app.Proxy())
	availState := consensusState.Avail()

	lane0 := types.NewLaneID(a.Public(), 0)
	require.Equal(t, lane0, availState.LocalLane().OrPanic("genesis"))

	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("avail", func() error {
			return utils.IgnoreCancel(availState.Run(ctx))
		})
		s.SpawnBgNamed("producer", func() error {
			return utils.IgnoreCancel(prod.Run(ctx))
		})

		if _, err := availState.Block(ctx, lane0, 0); err != nil {
			return err
		}

		epLeave, err := registry.ActivateEpoch(
			map[types.PublicKey]uint64{b.Public(): 1},
			types.OpenRoadRange(), time.Time{}, registry.FirstBlock(),
		)
		if err != nil {
			return err
		}
		if err := availState.ApplyEpoch(epLeave); err != nil {
			return err
		}
		if _, err := availState.LocalLaneUpdates().Wait(ctx, func(opt utils.Option[types.LaneID]) bool {
			return !opt.IsPresent()
		}); err != nil {
			return err
		}

		epJoin, err := registry.ActivateEpoch(
			map[types.PublicKey]uint64{a.Public(): 1, b.Public(): 1},
			types.OpenRoadRange(), time.Time{}, registry.FirstBlock(),
		)
		if err != nil {
			return err
		}
		if err := availState.ApplyEpoch(epJoin); err != nil {
			return err
		}
		lane2, err := availState.LocalLaneUpdates().Wait(ctx, func(opt utils.Option[types.LaneID]) bool {
			got, ok := opt.Get()
			return ok && got != lane0
		})
		if err != nil {
			return err
		}
		got, ok := lane2.Get()
		if !ok {
			return fmt.Errorf("expected rejoined LocalLane")
		}
		_, err = availState.Block(ctx, got, 0)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
