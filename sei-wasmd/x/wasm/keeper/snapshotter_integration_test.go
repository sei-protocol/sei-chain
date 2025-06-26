package keeper_test

import (
	"context"
	"crypto/sha256"
	"io/ioutil"
	"testing"
	"time"

	"github.com/CosmWasm/wasmd/x/wasm/types"

	"github.com/stretchr/testify/assert"

	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmtypes "github.com/tendermint/tendermint/types"

	"github.com/CosmWasm/wasmd/app"
	"github.com/CosmWasm/wasmd/x/wasm/keeper"
)

func TestSnapshotter(t *testing.T) {
	specs := map[string]struct {
		wasmFiles []string
	}{
		"single contract": {
			wasmFiles: []string{"./testdata/reflect.wasm"},
		},
		"multiple contract": {
			wasmFiles: []string{"./testdata/reflect.wasm", "./testdata/burner.wasm", "./testdata/reflect.wasm"},
		},
		"duplicate contracts": {
			wasmFiles: []string{"./testdata/reflect.wasm", "./testdata/reflect.wasm"},
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			// setup source app
			srcWasmApp, genesisAddr := newWasmExampleApp(t)

			// store wasm codes on chain
			ctx := srcWasmApp.NewUncachedContext(false, tmproto.Header{
				ChainID: "foo",
				Height:  srcWasmApp.LastBlockHeight() + 1,
				Time:    time.Now(),
			})
			wasmKeeper := app.NewTestSupport(t, srcWasmApp).WasmKeeper()
			contractKeeper := keeper.NewDefaultPermissionKeeper(&wasmKeeper)

			srcCodeIDToChecksum := make(map[uint64][]byte, len(spec.wasmFiles))
			for i, v := range spec.wasmFiles {
				wasmCode, err := ioutil.ReadFile(v)
				require.NoError(t, err)
				codeID, err := contractKeeper.Create(ctx, genesisAddr, wasmCode, nil)
				require.NoError(t, err)
				require.Equal(t, uint64(i+1), codeID)
				hash := sha256.Sum256(wasmCode)
				srcCodeIDToChecksum[codeID] = hash[:]
			}
			// create snapshot
			srcWasmApp.SetDeliverStateToCommit()
			srcWasmApp.Commit(context.Background())
			snapshotHeight := uint64(srcWasmApp.LastBlockHeight())
			snapshot, err := srcWasmApp.SnapshotManager().Create(snapshotHeight)
			require.NoError(t, err)
			assert.NotNil(t, snapshot)

			// when snapshot imported into dest app instance
			destWasmApp := app.SetupWithEmptyStore(t)
			require.NoError(t, destWasmApp.SnapshotManager().Restore(*snapshot))
			for i := uint32(0); i < snapshot.Chunks; i++ {
				chunkBz, err := srcWasmApp.SnapshotManager().LoadChunk(snapshot.Height, snapshot.Format, i)
				require.NoError(t, err)
				end, err := destWasmApp.SnapshotManager().RestoreChunk(chunkBz)
				require.NoError(t, err)
				if end {
					break
				}
			}

			// then all wasm contracts are imported
			wasmKeeper = app.NewTestSupport(t, destWasmApp).WasmKeeper()
			ctx = destWasmApp.NewUncachedContext(false, tmproto.Header{
				ChainID: "foo",
				Height:  destWasmApp.LastBlockHeight() + 1,
				Time:    time.Now(),
			})

			destCodeIDToChecksum := make(map[uint64][]byte, len(spec.wasmFiles))
			wasmKeeper.IterateCodeInfos(ctx, func(id uint64, info types.CodeInfo) bool {
				bz, err := wasmKeeper.GetByteCode(ctx, id)
				require.NoError(t, err)
				hash := sha256.Sum256(bz)
				destCodeIDToChecksum[id] = hash[:]
				assert.Equal(t, hash[:], info.CodeHash)
				return false
			})
			assert.Equal(t, srcCodeIDToChecksum, destCodeIDToChecksum)
		})
	}
}

func newWasmExampleApp(t *testing.T) (*app.WasmApp, sdk.AccAddress) {
	senderPrivKey := ed25519.GenPrivKey()
	pubKey, err := cryptocodec.ToTmPubKeyInterface(senderPrivKey.PubKey())
	require.NoError(t, err)

	senderAddr := senderPrivKey.PubKey().Address().Bytes()
	acc := authtypes.NewBaseAccount(senderAddr, senderPrivKey.PubKey(), 0, 0)
	amount, ok := sdk.NewIntFromString("10000000000000000000")
	require.True(t, ok)

	balance := banktypes.Balance{
		Address: acc.GetAddress().String(),
		Coins:   sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, amount)),
	}
	validator := tmtypes.NewValidator(pubKey, 1)
	valSet := tmtypes.NewValidatorSet([]*tmtypes.Validator{validator})
	wasmApp := app.SetupWithGenesisValSet(t, "testing", valSet, []authtypes.GenesisAccount{acc}, nil, balance)

	return wasmApp, senderAddr
}
