package auth_test

import (
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/auth"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/stretchr/testify/require"
)

func TestAccount(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper
	seiAddr, evmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, seiAddr, evmAddr)

	p, err := auth.NewPrecompile(testApp.GetPrecompileKeepers())
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{StateDB: statedb, TxContext: vm.TxContext{Origin: evmAddr}}
	executor := p.GetExecutor().(*auth.PrecompileExecutor)

	method, err := p.ABI.MethodById(executor.AccountID)
	require.Nil(t, err)
	args, err := method.Inputs.Pack(evmAddr)
	require.Nil(t, err)
	ret, _, err := p.RunAndCalculateGas(&evm, evmAddr, evmAddr, append(executor.AccountID, args...), 1000000, nil, nil, true, false)
	require.Nil(t, err)

	acc := testApp.AccountKeeper.GetAccount(ctx, seiAddr)
	require.NotNil(t, acc)
	expected, err := method.Outputs.Pack(auth.Account{
		AccountAddress: seiAddr.String(),
		AccountNumber:  acc.GetAccountNumber(),
		Sequence:       acc.GetSequence(),
	})
	require.Nil(t, err)
	require.Equal(t, expected, ret)

	// unassociated address should error
	_, unassociatedEvmAddr := testkeeper.MockAddressPair()
	args, err = method.Inputs.Pack(unassociatedEvmAddr)
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, evmAddr, evmAddr, append(executor.AccountID, args...), 1000000, nil, nil, true, false)
	require.NotNil(t, err)

	// associated address without an account should error with "account not found"
	missingSeiAddr, missingEvmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, missingSeiAddr, missingEvmAddr)
	missingAcc := testApp.AccountKeeper.GetAccount(ctx, missingSeiAddr)
	require.NotNil(t, missingAcc)
	testApp.AccountKeeper.RemoveAccount(ctx, missingAcc)
	args, err = method.Inputs.Pack(missingEvmAddr)
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, evmAddr, evmAddr, append(executor.AccountID, args...), 1000000, nil, nil, true, false)
	require.NotNil(t, err)
}

func TestAccounts(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper
	seiAddr, evmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, seiAddr, evmAddr)

	p, err := auth.NewPrecompile(testApp.GetPrecompileKeepers())
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{StateDB: statedb, TxContext: vm.TxContext{Origin: evmAddr}}
	executor := p.GetExecutor().(*auth.PrecompileExecutor)

	method, err := p.ABI.MethodById(executor.AccountsID)
	require.Nil(t, err)
	args, err := method.Inputs.Pack([]byte{})
	require.Nil(t, err)
	ret, _, err := p.RunAndCalculateGas(&evm, evmAddr, evmAddr, append(executor.AccountsID, args...), 1000000, nil, nil, true, false)
	require.Nil(t, err)

	outputs, err := method.Outputs.Unpack(ret)
	require.Nil(t, err)
	require.Len(t, outputs, 1)

	response := reflect.ValueOf(outputs[0])
	accounts := response.FieldByName("Accounts")
	require.True(t, accounts.Len() > 0)
	found := false
	for i := 0; i < accounts.Len(); i++ {
		account := accounts.Index(i)
		if account.FieldByName("AccountAddress").String() == seiAddr.String() {
			found = true
			break
		}
	}
	require.True(t, found)
}

func TestParams(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper
	seiAddr, evmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, seiAddr, evmAddr)

	p, err := auth.NewPrecompile(testApp.GetPrecompileKeepers())
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{StateDB: statedb, TxContext: vm.TxContext{Origin: evmAddr}}
	executor := p.GetExecutor().(*auth.PrecompileExecutor)

	method, err := p.ABI.MethodById(executor.ParamsID)
	require.Nil(t, err)
	ret, _, err := p.RunAndCalculateGas(&evm, evmAddr, evmAddr, executor.ParamsID, 1000000, nil, nil, true, false)
	require.Nil(t, err)

	params := testApp.AccountKeeper.GetParams(ctx)
	expected, err := method.Outputs.Pack(auth.AuthParams{
		MaxMemoCharacters:      params.MaxMemoCharacters,
		TxSigLimit:             params.TxSigLimit,
		TxSizeCostPerByte:      params.TxSizeCostPerByte,
		SigVerifyCostEd25519:   params.SigVerifyCostED25519,
		SigVerifyCostSecp256k1: params.SigVerifyCostSecp256k1,
		DisableSeqnoCheck:      params.DisableSeqnoCheck,
	})
	require.Nil(t, err)
	require.Equal(t, expected, ret)
}

func TestNextAccountNumber(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper
	seiAddr, evmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, seiAddr, evmAddr)

	p, err := auth.NewPrecompile(testApp.GetPrecompileKeepers())
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{StateDB: statedb, TxContext: vm.TxContext{Origin: evmAddr}}
	executor := p.GetExecutor().(*auth.PrecompileExecutor)

	method, err := p.ABI.MethodById(executor.NextAccountNumberID)
	require.Nil(t, err)
	queryCount := func() uint64 {
		ret, _, err := p.RunAndCalculateGas(&evm, evmAddr, evmAddr, executor.NextAccountNumberID, 1000000, nil, nil, true, false)
		require.Nil(t, err)
		outputs, err := method.Outputs.Unpack(ret)
		require.Nil(t, err)
		require.Len(t, outputs, 1)
		count, ok := outputs[0].(uint64)
		require.True(t, ok)
		return count
	}

	count := queryCount()
	require.True(t, count > 0)
	// The query is a view: it must not increment the persisted counter, so
	// repeated calls return the same value.
	require.Equal(t, count, queryCount())
	// The keeper hands out exactly the number the query reported.
	require.Equal(t, count, testApp.AccountKeeper.GetNextAccountNumber(ctx))
}
