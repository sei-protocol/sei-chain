package staking_test

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/staking"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authztypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/authz"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
)

func TestStakingAuthorizationFlow(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	blockTime := time.Unix(1_700_000_100, 0).UTC()
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2).WithBlockTime(blockTime)
	k := &testApp.EvmKeeper
	validatorSrc := setupValidator(t, ctx, testApp, stakingtypes.Bonded, testkeeper.MockPrivateKey().PubKey())
	validatorDst := setupValidator(t, ctx, testApp, stakingtypes.Bonded, testkeeper.MockPrivateKey().PubKey())

	granterSeiAddr, granterEVMAddr := testkeeper.MockAddressPair()
	granteeSeiAddr, granteeEVMAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, granterSeiAddr, granterEVMAddr)
	k.SetAddressMapping(ctx, granteeSeiAddr, granteeEVMAddr)

	p, err := staking.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{StateDB: statedb}
	call := func(caller common.Address, methodName string, value *big.Int, args ...interface{}) ([]byte, error) {
		t.Helper()
		input, packErr := p.ABI.Pack(methodName, args...)
		require.NoError(t, packErr)
		ret, _, callErr := p.RunAndCalculateGas(&evm, caller, caller, input, 10_000_000, value, nil, false, false)
		return ret, callErr
	}
	assertSuccess := func(methodName string, ret []byte) {
		t.Helper()
		outputs, unpackErr := p.ABI.Methods[methodName].Outputs.Unpack(ret)
		require.NoError(t, unpackErr)
		require.Equal(t, []interface{}{true}, outputs)
	}

	expiration := blockTime.Add(time.Hour)
	ret, err := call(granterEVMAddr, staking.GrantStakingMethod, nil, granteeEVMAddr, expiration.Unix())
	require.NoError(t, err)
	assertSuccess(staking.GrantStakingMethod, ret)

	for _, msg := range []sdk.Msg{
		&stakingtypes.MsgDelegate{},
		&stakingtypes.MsgBeginRedelegate{},
		&stakingtypes.MsgUndelegate{},
	} {
		authorization, storedExpiration := testApp.AuthzKeeper.GetCleanAuthorization(
			statedb.Ctx(),
			granteeSeiAddr,
			granterSeiAddr,
			sdk.MsgTypeURL(msg),
		)
		require.IsType(t, &authztypes.GenericAuthorization{}, authorization)
		require.Equal(t, expiration, storedExpiration)
	}

	delegateValue := big.NewInt(100_000_000_000_000)
	precompileAddr := k.GetSeiAddressOrDefault(statedb.Ctx(), common.HexToAddress(staking.StakingAddress))
	precompileFunds := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(statedb.Ctx()), sdk.NewInt(100)))
	require.NoError(t, testApp.BankKeeper.MintCoins(statedb.Ctx(), minttypes.ModuleName, precompileFunds))
	require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToAccount(statedb.Ctx(), minttypes.ModuleName, precompileAddr, precompileFunds))

	ret, err = call(
		granteeEVMAddr,
		staking.DelegateWithAuthzMethod,
		delegateValue,
		granterEVMAddr,
		validatorSrc.String(),
	)
	require.NoError(t, err)
	assertSuccess(staking.DelegateWithAuthzMethod, ret)
	delegation, found := testApp.StakingKeeper.GetDelegation(statedb.Ctx(), granterSeiAddr, validatorSrc)
	require.True(t, found)
	require.Equal(t, int64(100), delegation.Shares.RoundInt().Int64())

	ret, err = call(
		granteeEVMAddr,
		staking.RedelegateWithAuthzMethod,
		nil,
		granterEVMAddr,
		validatorSrc.String(),
		validatorDst.String(),
		big.NewInt(30),
	)
	require.NoError(t, err)
	assertSuccess(staking.RedelegateWithAuthzMethod, ret)
	delegation, found = testApp.StakingKeeper.GetDelegation(statedb.Ctx(), granterSeiAddr, validatorSrc)
	require.True(t, found)
	require.Equal(t, int64(70), delegation.Shares.RoundInt().Int64())

	ret, err = call(
		granteeEVMAddr,
		staking.UndelegateWithAuthzMethod,
		nil,
		granterEVMAddr,
		validatorSrc.String(),
		big.NewInt(20),
	)
	require.NoError(t, err)
	assertSuccess(staking.UndelegateWithAuthzMethod, ret)
	delegation, found = testApp.StakingKeeper.GetDelegation(statedb.Ctx(), granterSeiAddr, validatorSrc)
	require.True(t, found)
	require.Equal(t, int64(50), delegation.Shares.RoundInt().Int64())

	ret, err = call(granterEVMAddr, staking.RevokeStakingMethod, nil, granteeEVMAddr)
	require.NoError(t, err)
	assertSuccess(staking.RevokeStakingMethod, ret)
	for _, msg := range []sdk.Msg{
		&stakingtypes.MsgDelegate{},
		&stakingtypes.MsgBeginRedelegate{},
		&stakingtypes.MsgUndelegate{},
	} {
		authorization, _ := testApp.AuthzKeeper.GetCleanAuthorization(
			statedb.Ctx(),
			granteeSeiAddr,
			granterSeiAddr,
			sdk.MsgTypeURL(msg),
		)
		require.Nil(t, authorization)
	}

	_, err = call(
		granteeEVMAddr,
		staking.UndelegateWithAuthzMethod,
		nil,
		granterEVMAddr,
		validatorSrc.String(),
		big.NewInt(1),
	)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)

}
