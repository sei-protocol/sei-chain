package evm_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestInitGenesis(t *testing.T) {
	keeper := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	ctx = ctx.WithMultiStore(ctx.MultiStore().CacheMultiStore())
	seiAddr, evmAddr := testkeeper.MockAddressPair()
	_, codeAddr := testkeeper.MockAddressPair()
	slot, value := common.BytesToHash([]byte("123")), common.BytesToHash([]byte("456"))
	genesis := types.GenesisState{
		Params:              types.DefaultParams(),
		AddressAssociations: []*types.AddressAssociation{{SeiAddress: seiAddr.String(), EthAddress: evmAddr.Hex()}},
		Codes:               []*types.Code{{Address: codeAddr.Hex(), Code: []byte("abcde")}},
		States:              []*types.ContractState{{Address: codeAddr.Hex(), Key: slot.Bytes(), Value: value.Bytes()}},
		Nonces:              []*types.Nonce{{Address: evmAddr.Hex(), Nonce: 2}},
		Serialized: []*types.Serialized{
			{Prefix: []byte("prefix"), Key: []byte("key"), Value: []byte("prefixed")},
			{Prefix: []byte("unprefixed"), Value: []byte("whole key")},
		},
	}
	require.NoError(t, genesis.Validate())

	evm.InitGenesis(ctx, keeper, genesis)

	require.Equal(t, types.DefaultParams(), keeper.GetParams(ctx))
	require.Equal(t, evmAddr, keeper.GetEVMAddressOrDefault(ctx, seiAddr))
	require.Equal(t, []byte("abcde"), keeper.GetCode(ctx, codeAddr))
	require.Equal(t, value, keeper.GetState(ctx, codeAddr, slot))
	require.Equal(t, uint64(2), keeper.GetNonce(ctx, evmAddr))
	store := ctx.KVStore(keeper.GetStoreKey())
	require.Equal(t, []byte("prefixed"), store.Get([]byte("prefixkey")))
	require.Equal(t, []byte("whole key"), store.Get([]byte("unprefixed")))
}
