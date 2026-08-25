package ibc_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"

	"github.com/sei-protocol/sei-chain/precompiles/ibc"
	"github.com/sei-protocol/sei-chain/precompiles/utils"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
	tmdb "github.com/tendermint/tm-db"
)

func TestRetiredPrecompileRejectsCalls(t *testing.T) {
	keepers := testKeepers{EVMKeeper: testEVMKeeper{}}
	ctx := sdk.NewContext(store.NewCommitMultiStore(tmdb.NewMemDB()), tmproto.Header{}, false)
	evm := vm.EVM{
		StateDB: state.NewDBImpl(ctx, testStateKeeper{}, true),
	}

	precompile, err := ibc.NewPrecompile(keepers)
	require.NoError(t, err)

	tests := map[string][]interface{}{
		"transfer": {
			"receiver", "transfer", "channel-0", "usei", big.NewInt(1),
			uint64(1), uint64(1), uint64(1), "",
		},
		"transferWithDefaultTimeout": {
			"receiver", "transfer", "channel-0", "usei", big.NewInt(1), "",
		},
	}
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			method := precompile.ABI.Methods[name]
			inputs, packErr := method.Inputs.Pack(args...)
			require.NoError(t, packErr)

			ret, _, runErr := precompile.RunAndCalculateGas(
				&evm,
				common.Address{},
				common.Address{},
				append(method.ID, inputs...),
				1_000_000,
				nil,
				nil,
				false,
				false,
			)
			require.ErrorIs(t, runErr, vm.ErrExecutionReverted)
			reason, unpackErr := abi.UnpackRevert(ret)
			require.NoError(t, unpackErr)
			require.Equal(t, ibc.ErrIBCPrecompileRetired.Error(), reason)
		})
	}
}

func TestVersionedPrecompilesAreAllTombstones(t *testing.T) {
	versioned := ibc.GetVersioned("v6.8", testKeepers{EVMKeeper: testEVMKeeper{}})
	versions := []string{
		"v5.5.2",
		"v5.5.5",
		"v5.6.2",
		"v5.8.0",
		"v6.0.1",
		"v6.0.3",
		"v6.0.5",
		"v6.0.6",
		"v6.1.0",
		"v6.1.4",
		"v6.2.0",
		"v6.3.0",
		"v6.4.0",
		"v6.5",
		"v6.6",
		"v6.7",
		"v6.8",
	}

	require.Len(t, versioned, len(versions))
	tombstone := versioned["v6.8"]
	for _, version := range versions {
		require.Contains(t, versioned, version)
		require.Same(t, tombstone, versioned[version])
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
