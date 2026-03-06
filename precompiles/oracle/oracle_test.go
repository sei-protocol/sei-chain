package oracle_test

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/precompiles/oracle"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
)

func TestGetExchangeRateReturnsRetiredError(t *testing.T) {
	testApp := app.Setup(t, false, true, false)
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockTime(time.Unix(5400, 0)).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	// Setup sender addresses and environment
	privKey := testkeeper.MockPrivateKey()
	senderAddr, senderEVMAddr := testkeeper.PrivateKeyToAddresses(privKey)
	k.SetAddressMapping(ctx, senderAddr, senderEVMAddr)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB:   statedb,
		TxContext: vm.TxContext{Origin: senderEVMAddr},
	}

	p, err := oracle.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)
	ret, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, p.GetExecutor().(*oracle.PrecompileExecutor).GetExchangeRatesId, 100000, nil, nil, true, false)
	require.Error(t, err)
	require.Equal(t, vm.ErrExecutionReverted, err)
	reason, unpackErr := abi.UnpackRevert(ret)
	require.NoError(t, unpackErr)
	require.Equal(t, oracle.ErrOraclePrecompileRetired.Error(), reason)
}

func TestGetOracleTwapsReturnsRetiredError(t *testing.T) {
	testApp := app.Setup(t, false, true, false)
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockTime(time.Unix(5400, 0)).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	// Setup sender addresses and environment
	privKey := testkeeper.MockPrivateKey()
	senderAddr, senderEVMAddr := testkeeper.PrivateKeyToAddresses(privKey)
	k.SetAddressMapping(ctx, senderAddr, senderEVMAddr)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB:   statedb,
		TxContext: vm.TxContext{Origin: senderEVMAddr},
	}

	p, err := oracle.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)

	query, err := p.ABI.MethodById(p.GetExecutor().(*oracle.PrecompileExecutor).GetOracleTwapsId)
	require.NoError(t, err)
	args, err := query.Inputs.Pack(uint64(3600))
	require.NoError(t, err)

	ret, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(p.GetExecutor().(*oracle.PrecompileExecutor).GetOracleTwapsId, args...), 100000, nil, nil, true, false)
	require.Error(t, err)
	require.Equal(t, vm.ErrExecutionReverted, err)
	reason, unpackErr := abi.UnpackRevert(ret)
	require.NoError(t, unpackErr)
	require.Equal(t, oracle.ErrOraclePrecompileRetired.Error(), reason)
}
