package authz_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"
	tmdb "github.com/tendermint/tm-db"

	"github.com/sei-protocol/sei-chain/precompiles/authz"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/x/evm/state"
)

func TestRetiredPrecompileRejectsCalls(t *testing.T) {
	keepers := testKeepers{EVMKeeper: testEVMKeeper{}}
	ctx := sdk.NewContext(store.NewCommitMultiStore(tmdb.NewMemDB()), tmproto.Header{}, false)
	evm := vm.EVM{
		StateDB: state.NewDBImpl(ctx, testStateKeeper{}, true),
	}

	precompile, err := authz.NewPrecompile(keepers)
	require.NoError(t, err)

	granter := common.HexToAddress("0x0000000000000000000000000000000000000001")
	grantee := common.HexToAddress("0x0000000000000000000000000000000000000002")
	tests := map[string][]interface{}{
		"grants":        {granter, grantee, "/cosmos.bank.v1beta1.MsgSend", []byte{}},
		"granterGrants": {granter, []byte{}},
		"granteeGrants": {grantee, []byte{}},
	}
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			method := precompile.ABI.Methods[name]
			inputs, packErr := method.Inputs.Pack(args...)
			require.NoError(t, packErr)

			ret, remainingGas, runErr := precompile.RunAndCalculateGas(
				&evm,
				common.Address{},
				common.Address{},
				append(method.ID, inputs...),
				1_000_000,
				nil,
				nil,
				true,
				false,
			)
			require.ErrorIs(t, runErr, vm.ErrExecutionReverted)
			require.Positive(t, remainingGas, "the retired precompile consumed every supplied unit of gas")
			reason, unpackErr := abi.UnpackRevert(ret)
			require.NoError(t, unpackErr)
			require.Equal(t, authz.ErrAuthzPrecompileRetired.Error(), reason)
		})
	}
}

type testKeepers struct {
	utils.Keepers
	EVMKeeper utils.EVMKeeper
}

func (k testKeepers) EVMK() utils.EVMKeeper {
	return k.EVMKeeper
}

type testEVMKeeper struct {
	utils.EVMKeeper
}

func (testEVMKeeper) GetCosmosGasLimitFromEVMGas(_ sdk.Context, evmGas uint64) uint64 {
	return evmGas
}

func (testEVMKeeper) GetEVMGasLimitFromCtx(_ sdk.Context) uint64 {
	return 1_000_000
}

type testStateKeeper struct {
	state.EVMKeeper
}

func (testStateKeeper) GetFeeCollectorAddress(sdk.Context) (common.Address, error) {
	return common.Address{}, nil
}
