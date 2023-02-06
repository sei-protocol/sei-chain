package keeper_test

import (
	"testing"
	"time"

	abci "github.com/tendermint/tendermint/abci/types"

	"github.com/CosmWasm/wasmd/x/wasm/keeper"

	"github.com/cosmos/cosmos-sdk/store"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	dbm "github.com/tendermint/tm-db"

	"github.com/CosmWasm/wasmd/x/wasm/types"
)

func TestCountTxDecorator(t *testing.T) {
	keyWasm := sdk.NewKVStoreKey(types.StoreKey)
	db := dbm.NewMemDB()
	ms := store.NewCommitMultiStore(db)
	ms.MountStoreWithDB(keyWasm, sdk.StoreTypeIAVL, db)
	require.NoError(t, ms.LoadLatestVersion())
	const myCurrentBlockHeight = 100

	specs := map[string]struct {
		setupDB        func(t *testing.T, ctx sdk.Context)
		simulate       bool
		nextAssertAnte func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error)
		expErr         bool
	}{
		"no initial counter set": {
			setupDB: func(t *testing.T, ctx sdk.Context) {},
			nextAssertAnte: func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
				gotCounter, ok := types.TXCounter(ctx)
				require.True(t, ok)
				assert.Equal(t, uint32(0), gotCounter)
				// and stored +1
				bz := ctx.MultiStore().GetKVStore(keyWasm).Get(types.TXCounterPrefix)
				assert.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, myCurrentBlockHeight, 0, 0, 0, 1}, bz)
				return ctx, nil
			},
		},
		"persistent counter incremented - big endian": {
			setupDB: func(t *testing.T, ctx sdk.Context) {
				bz := []byte{0, 0, 0, 0, 0, 0, 0, myCurrentBlockHeight, 1, 0, 0, 2}
				ctx.MultiStore().GetKVStore(keyWasm).Set(types.TXCounterPrefix, bz)
			},
			nextAssertAnte: func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
				gotCounter, ok := types.TXCounter(ctx)
				require.True(t, ok)
				assert.Equal(t, uint32(1<<24+2), gotCounter)
				// and stored +1
				bz := ctx.MultiStore().GetKVStore(keyWasm).Get(types.TXCounterPrefix)
				assert.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, myCurrentBlockHeight, 1, 0, 0, 3}, bz)
				return ctx, nil
			},
		},
		"old height counter replaced": {
			setupDB: func(t *testing.T, ctx sdk.Context) {
				previousHeight := byte(myCurrentBlockHeight - 1)
				bz := []byte{0, 0, 0, 0, 0, 0, 0, previousHeight, 0, 0, 0, 1}
				ctx.MultiStore().GetKVStore(keyWasm).Set(types.TXCounterPrefix, bz)
			},
			nextAssertAnte: func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
				gotCounter, ok := types.TXCounter(ctx)
				require.True(t, ok)
				assert.Equal(t, uint32(0), gotCounter)
				// and stored +1
				bz := ctx.MultiStore().GetKVStore(keyWasm).Get(types.TXCounterPrefix)
				assert.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, myCurrentBlockHeight, 0, 0, 0, 1}, bz)
				return ctx, nil
			},
		},
		"simulation not persisted": {
			setupDB: func(t *testing.T, ctx sdk.Context) {
			},
			simulate: true,
			nextAssertAnte: func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
				_, ok := types.TXCounter(ctx)
				assert.False(t, ok)
				require.True(t, simulate)
				// and not stored
				assert.False(t, ctx.MultiStore().GetKVStore(keyWasm).Has(types.TXCounterPrefix))
				return ctx, nil
			},
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			ctx := sdk.NewContext(ms.CacheMultiStore(), tmproto.Header{
				Height: myCurrentBlockHeight,
				Time:   time.Date(2021, time.September, 27, 12, 0, 0, 0, time.UTC),
			}, false, log.NewNopLogger())

			spec.setupDB(t, ctx)
			var anyTx sdk.Tx

			// when
			ante := keeper.NewCountTXDecorator(keyWasm)
			_, gotErr := ante.AnteHandle(ctx, anyTx, spec.simulate, spec.nextAssertAnte)
			if spec.expErr {
				require.Error(t, gotErr)
				return
			}
			require.NoError(t, gotErr)
		})
	}
}

func TestLimitSimulationGasDecorator(t *testing.T) {
	var (
		hundred sdk.Gas = 100
		zero    sdk.Gas = 0
	)
	specs := map[string]struct {
		customLimit *sdk.Gas
		consumeGas  sdk.Gas
		maxBlockGas int64
		simulation  bool
		expErr      interface{}
	}{
		"custom limit set": {
			customLimit: &hundred,
			consumeGas:  hundred + 1,
			maxBlockGas: -1,
			simulation:  true,
			expErr:      sdk.ErrorOutOfGas{Descriptor: "testing"},
		},
		"block limit set": {
			maxBlockGas: 100,
			consumeGas:  hundred + 1,
			simulation:  true,
			expErr:      sdk.ErrorOutOfGas{Descriptor: "testing"},
		},
		"no limits set": {
			maxBlockGas: -1,
			consumeGas:  hundred + 1,
			simulation:  true,
		},
		"both limits set, custom applies": {
			customLimit: &hundred,
			consumeGas:  hundred - 1,
			maxBlockGas: 10,
			simulation:  true,
		},
		"not a simulation": {
			customLimit: &hundred,
			consumeGas:  hundred + 1,
			simulation:  false,
		},
		"zero custom limit": {
			customLimit: &zero,
			simulation:  true,
			expErr:      "gas limit must not be zero",
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			nextAnte := consumeGasAnteHandler(spec.consumeGas)
			ctx := sdk.Context{}.
				WithGasMeter(sdk.NewInfiniteGasMeter()).
				WithConsensusParams(&abci.ConsensusParams{
					Block: &abci.BlockParams{MaxGas: spec.maxBlockGas},
				})
			// when
			if spec.expErr != nil {
				require.PanicsWithValue(t, spec.expErr, func() {
					ante := keeper.NewLimitSimulationGasDecorator(spec.customLimit)
					ante.AnteHandle(ctx, nil, spec.simulation, nextAnte)
				})
				return
			}
			ante := keeper.NewLimitSimulationGasDecorator(spec.customLimit)
			ante.AnteHandle(ctx, nil, spec.simulation, nextAnte)
		})
	}
}

func consumeGasAnteHandler(gasToConsume sdk.Gas) sdk.AnteHandler {
	return func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		ctx.GasMeter().ConsumeGas(gasToConsume, "testing")
		return ctx, nil
	}
}
