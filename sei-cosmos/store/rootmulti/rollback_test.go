package rootmulti_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/legacyabci"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

func setup(t *testing.T, withGenesis bool, invCheckPeriod uint, db dbm.DB) (*app.App, map[string]json.RawMessage) {
	a := app.SetupWithDB(t, db, false, false, false)
	if withGenesis {
		return a, app.ModuleBasics.DefaultGenesis(a.AppCodec())
	}
	return a, map[string]json.RawMessage{}
}

// Setup initializes a new SimApp. A Nop logger is set in SimApp.
func SetupWithDB(t *testing.T, isCheckTx bool, db dbm.DB) *app.App {
	a, genesisState := setup(t, !isCheckTx, 5, db)
	if !isCheckTx {
		// init chain must be called to stop deliverState from being nil
		stateBytes, err := json.MarshalIndent(genesisState, "", " ")
		if err != nil {
			panic(err)
		}

		// Initialize the chain
		a.InitChain(
			context.Background(), &abci.RequestInitChain{
				Validators:      []abci.ValidatorUpdate{},
				ConsensusParams: app.DefaultConsensusParams,
				AppStateBytes:   stateBytes,
			},
		)
	}

	return a
}

func TestRollback(t *testing.T) {
	t.Skip()
	db := dbm.NewMemDB()
	a := SetupWithDB(t, false, db)
	a.SetDeliverStateToCommit()
	a.Commit(context.Background())
	ver0 := a.LastBlockHeight()
	// commit 10 blocks
	for i := int64(1); i <= 10; i++ {
		header := tmproto.Header{
			Height:  ver0 + i,
			AppHash: a.LastCommitID().Hash,
		}
		legacyabci.BeginBlock(sdk.Context{}, header.Height, []abci.VoteInfo{}, []abci.Misbehavior{}, a.BeginBlockKeepers)
		ctx := a.NewContext(false, header)
		store := ctx.KVStore(a.GetKey("bank"))
		store.Set([]byte("key"), []byte(fmt.Sprintf("value%d", i)))
		a.Commit(context.Background())
	}

	require.Equal(t, ver0+10, a.LastBlockHeight())
	store := a.NewContext(true, tmproto.Header{}).KVStore(a.GetKey("bank"))
	require.Equal(t, []byte("value10"), store.Get([]byte("key")))

	// rollback 5 blocks
	target := ver0 + 5
	require.NoError(t, a.CommitMultiStore().RollbackToVersion(target))
	require.Equal(t, target, a.LastBlockHeight())

	// recreate app to have clean check state
	a = SetupWithDB(t, false, db)
	store = a.NewContext(true, tmproto.Header{}).KVStore(a.GetKey("bank"))
	require.Equal(t, []byte("value5"), store.Get([]byte("key")))

	// commit another 5 blocks with different values
	for i := int64(6); i <= 10; i++ {
		header := tmproto.Header{
			Height:  ver0 + i,
			AppHash: a.LastCommitID().Hash,
		}
		legacyabci.BeginBlock(sdk.Context{}, header.Height, []abci.VoteInfo{}, []abci.Misbehavior{}, a.BeginBlockKeepers)
		ctx := a.NewContext(false, header)
		store := ctx.KVStore(a.GetKey("bank"))
		store.Set([]byte("key"), []byte(fmt.Sprintf("VALUE%d", i)))
		a.Commit(context.Background())
	}

	require.Equal(t, ver0+10, a.LastBlockHeight())
	store = a.NewContext(true, tmproto.Header{}).KVStore(a.GetKey("bank"))
	require.Equal(t, []byte("VALUE10"), store.Get([]byte("key")))
}
