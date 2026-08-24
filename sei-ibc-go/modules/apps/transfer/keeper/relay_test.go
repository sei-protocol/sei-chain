package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"
	tmdb "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"

	clienttypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/02-client/types"
	porttypes "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/05-port/types"
)

func TestSendTransferRejectsForeignPort(t *testing.T) {
	storeKey := sdk.NewKVStoreKey("transfer")
	db := tmdb.NewMemDB()
	stateStore := store.NewCommitMultiStore(db)
	stateStore.MountStoreWithDB(storeKey, sdk.StoreTypeIAVL, db)
	require.NoError(t, stateStore.LoadLatestVersion())

	ctx := sdk.NewContext(stateStore, tmproto.Header{}, false)
	k := Keeper{storeKey: storeKey}
	k.SetPort(ctx, "transfer")

	_, err := k.sendTransfer(
		ctx,
		"wasm.contract",
		"channel-0",
		sdk.Coin{},
		nil,
		"receiver",
		clienttypes.Height{},
		0,
		"",
	)
	require.ErrorIs(t, err, porttypes.ErrInvalidPort)
}
