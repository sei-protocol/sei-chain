package keeper_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/store/prefix"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestChargeRentForGas(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	keeper.SetParams(ctx, types.Params{SudoCallGasPrice: sdk.NewDecWithPrec(1, 1), PriceSnapshotRetention: 1})
	err := keeper.SetContract(ctx, &types.ContractInfoV2{
		Creator:      keepertest.TestAccount,
		ContractAddr: keepertest.TestContract,
		CodeId:       1,
		RentBalance:  1000000,
	})
	require.Nil(t, err)
	err = keeper.ChargeRentForGas(ctx, keepertest.TestContract, 5000000, 0)
	require.Nil(t, err)
	contract, err := keeper.GetContract(ctx, keepertest.TestContract)
	require.Nil(t, err)
	require.Equal(t, uint64(500000), contract.RentBalance)
	err = keeper.ChargeRentForGas(ctx, keepertest.TestContract, 6000000, 0)
	require.NotNil(t, err)
	contract, err = keeper.GetContract(ctx, keepertest.TestContract)
	require.Nil(t, err)
	require.Equal(t, uint64(0), contract.RentBalance)
	err = keeper.SetContract(ctx, &types.ContractInfoV2{
		Creator:      keepertest.TestAccount,
		ContractAddr: keepertest.TestContract,
		CodeId:       1,
		RentBalance:  1000000,
	})
	require.Nil(t, err)
	err = keeper.ChargeRentForGas(ctx, keepertest.TestContract, 5000000, 4000000)
	require.Nil(t, err)
	contract, err = keeper.GetContract(ctx, keepertest.TestContract)
	require.Nil(t, err)
	require.Equal(t, uint64(900000), contract.RentBalance)
	err = keeper.ChargeRentForGas(ctx, keepertest.TestContract, 5000000, 6000000)
	require.Nil(t, err)
	contract, err = keeper.GetContract(ctx, keepertest.TestContract)
	require.Nil(t, err)
	require.Equal(t, uint64(900000), contract.RentBalance)

	// delete contract
	keeper.DeleteContract(ctx, keepertest.TestContract)
	_, err = keeper.GetContract(ctx, keepertest.TestContract)
	require.NotNil(t, err)
}

func TestGetContractInfo(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	keeper.SetParams(ctx, types.Params{SudoCallGasPrice: sdk.NewDecWithPrec(1, 1), PriceSnapshotRetention: 1})
	keeper.SetContract(ctx, &types.ContractInfoV2{
		Creator:      keepertest.TestAccount,
		ContractAddr: keepertest.TestContract,
		CodeId:       1,
		RentBalance:  1000000,
	})
	// Successfully get a contract
	contract, err := keeper.GetContract(ctx, keepertest.TestContract)
	require.Equal(t, uint64(1000000), contract.RentBalance)
	require.Equal(t, uint64(1), contract.CodeId)
	require.Equal(t, keepertest.TestAccount, contract.Creator)

	// Getting a non exist contract should throw error for contract not exist
	_, err = keeper.GetContract(ctx, keepertest.TestContract2)
	require.Error(t, err)
	require.Equal(t, err, types.ErrContractNotExists)

	// Getting a corrupted record should throw error for unable to parse
	store := prefix.NewStore(
		ctx.KVStore(keeper.GetStoreKey()),
		[]byte("x-wasm-contract"),
	)
	bz := []byte("bad_contract")
	store.Set(types.ContractKey(keepertest.TestContract), bz)
	_, err = keeper.GetContract(ctx, keepertest.TestContract)
	require.Error(t, err)
	require.Equal(t, err, types.ErrParsingContractInfo)
}

func TestGetAllContractInfo(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	keeper.SetParams(ctx, types.Params{SudoCallGasPrice: sdk.NewDecWithPrec(1, 1), PriceSnapshotRetention: 1})
	keeper.SetContract(ctx, &types.ContractInfoV2{
		Creator:      keepertest.TestAccount,
		ContractAddr: keepertest.TestContract,
		CodeId:       1,
		RentBalance:  1000000,
	})
	keeper.SetContract(ctx, &types.ContractInfoV2{
		Creator:      keepertest.TestAccount2,
		ContractAddr: keepertest.TestContract2,
		CodeId:       2,
		RentBalance:  1000000,
	})
	contracts := keeper.GetAllContractInfo(ctx)
	require.Equal(t, uint64(1000000), contracts[0].RentBalance)
	require.Equal(t, uint64(1000000), contracts[1].RentBalance)
	require.Equal(t, uint64(1), contracts[0].CodeId)
	require.Equal(t, uint64(2), contracts[1].CodeId)
	require.Equal(t, keepertest.TestAccount, contracts[0].Creator)
	require.Equal(t, keepertest.TestContract, contracts[0].ContractAddr)
	require.Equal(t, keepertest.TestAccount2, contracts[1].Creator)
	require.Equal(t, keepertest.TestContract2, contracts[1].ContractAddr)
}

func TestGetContractGasLimit(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	contractAddr := sdk.MustAccAddressFromBech32("sei1suhgf5svhu4usrurvxzlgn54ksxmn8gljarjtxqnapv8kjnp4nrsgshtdj")
	keeper.SetParams(ctx, types.Params{SudoCallGasPrice: sdk.NewDecWithPrec(1, 1), PriceSnapshotRetention: 1})
	keeper.SetContract(ctx, &types.ContractInfoV2{
		Creator:      keepertest.TestAccount,
		ContractAddr: "sei1suhgf5svhu4usrurvxzlgn54ksxmn8gljarjtxqnapv8kjnp4nrsgshtdj",
		CodeId:       1,
		RentBalance:  1000000,
	})
	gasLimit, err := keeper.GetContractGasLimit(ctx, contractAddr)
	require.Nil(t, err)
	require.Equal(t, uint64(10000000), gasLimit)
}

func TestGetRentsForContracts(t *testing.T) {
	keeper, ctx := keepertest.DexKeeper(t)
	addr := "sei1suhgf5svhu4usrurvxzlgn54ksxmn8gljarjtxqnapv8kjnp4nrsgshtdj"
	require.Equal(t, 0, len(keeper.GetRentsForContracts(ctx, []string{addr})))

	keeper.SetContract(ctx, &types.ContractInfoV2{
		Creator:      keepertest.TestAccount,
		ContractAddr: addr,
		CodeId:       1,
		RentBalance:  100,
	})
	require.Equal(t, map[string]uint64{addr: uint64(100)}, keeper.GetRentsForContracts(ctx, []string{addr}))
}
