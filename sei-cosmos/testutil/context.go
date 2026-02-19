package testutil

import (
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/log"
	dbm "github.com/tendermint/tm-db"

	"github.com/cosmos/cosmos-sdk/store"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultContext creates a sdk.Context with a fresh MemDB that can be used in tests.
func DefaultContext(key sdk.StoreKey, tkey sdk.StoreKey) sdk.Context {
	db := dbm.NewMemDB()
	cms := store.NewCommitMultiStore(db)
	cms.MountStoreWithDB(key, sdk.StoreTypeIAVL, db)
	cms.MountStoreWithDB(tkey, sdk.StoreTypeTransient, db)
	err := cms.LoadLatestVersion()
	if err != nil {
		panic(err)
	}
	ctx := sdk.NewContext(cms, sdk.Header{}, false, log.NewNopLogger())

	return ctx
}
