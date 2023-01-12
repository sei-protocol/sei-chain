package migrations_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/migrations"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestMigrate8to9(t *testing.T) {
	dexkeeper, ctx := keepertest.DexKeeper(t)
	// write old contract
	store := prefix.NewStore(
		ctx.KVStore(dexkeeper.GetStoreKey()),
		[]byte(keeper.ContractPrefixKey),
	)
	contract := types.ContractInfo{
		CodeId:            1,
		ContractAddr:      keepertest.TestContract,
		NeedOrderMatching: true,
	}
	contractBytes, _ := contract.Marshal()
	store.Set(types.ContractKey(contract.ContractAddr), contractBytes)

	err := migrations.V8ToV9(ctx, *dexkeeper)
	require.NoError(t, err)

	contractV2, err := dexkeeper.GetContract(ctx, keepertest.TestContract)
	require.NoError(t, err)
	require.Equal(t, types.ContractInfoV2{
		CodeId:            1,
		ContractAddr:      keepertest.TestContract,
		NeedOrderMatching: true,
	}, contractV2)

	moduleAccount := dexkeeper.AccountKeeper.GetModuleAccount(ctx, types.ModuleName)
	require.NotNil(t, moduleAccount)
}
