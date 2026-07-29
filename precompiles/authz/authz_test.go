package authz_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/authz"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authztypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/authz"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestGrants(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper
	granterSeiAddr, granterEvmAddr := testkeeper.MockAddressPair()
	granteeSeiAddr, granteeEvmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, granterSeiAddr, granterEvmAddr)
	k.SetAddressMapping(ctx, granteeSeiAddr, granteeEvmAddr)

	expiration := time.Unix(1893456000, 0).UTC()
	authorization := banktypes.NewSendAuthorization(sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000))))
	require.Nil(t, testApp.AuthzKeeper.SaveGrant(ctx, granteeSeiAddr, granterSeiAddr, authorization, expiration))

	p, err := authz.NewPrecompile(testApp.GetPrecompileKeepers())
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{StateDB: statedb, TxContext: vm.TxContext{Origin: granterEvmAddr}}
	executor := p.GetExecutor().(*authz.PrecompileExecutor)

	method, err := p.ABI.MethodById(executor.GrantsID)
	require.Nil(t, err)
	args, err := method.Inputs.Pack(granterEvmAddr, granteeEvmAddr, "", []byte{})
	require.Nil(t, err)
	ret, _, err := p.RunAndCalculateGas(&evm, granterEvmAddr, granterEvmAddr, append(executor.GrantsID, args...), 1000000, nil, nil, true, false)
	require.Nil(t, err)

	outputs, err := method.Outputs.Unpack(ret)
	require.Nil(t, err)
	require.Len(t, outputs, 1)

	response := reflect.ValueOf(outputs[0])
	grants := response.FieldByName("Grants")
	require.Equal(t, 1, grants.Len())
	grant := grants.Index(0)
	authorizationJSON := string(grant.FieldByName("Authorization").Bytes())
	require.Contains(t, authorizationJSON, "@type")
	require.Contains(t, authorizationJSON, "SendAuthorization")
	require.Equal(t, expiration.Unix(), grant.FieldByName("Expiration").Int())

	// unassociated granter should error
	_, unassociatedEvmAddr := testkeeper.MockAddressPair()
	args, err = method.Inputs.Pack(unassociatedEvmAddr, granteeEvmAddr, "", []byte{})
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, granterEvmAddr, granterEvmAddr, append(executor.GrantsID, args...), 1000000, nil, nil, true, false)
	require.NotNil(t, err)
}

func TestGranterGrants(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper
	granterSeiAddr, granterEvmAddr := testkeeper.MockAddressPair()
	granteeSeiAddr, granteeEvmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, granterSeiAddr, granterEvmAddr)
	k.SetAddressMapping(ctx, granteeSeiAddr, granteeEvmAddr)

	expiration := time.Unix(1893456000, 0).UTC()
	authorization := banktypes.NewSendAuthorization(sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(2000))))
	require.Nil(t, testApp.AuthzKeeper.SaveGrant(ctx, granteeSeiAddr, granterSeiAddr, authorization, expiration))

	p, err := authz.NewPrecompile(testApp.GetPrecompileKeepers())
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{StateDB: statedb, TxContext: vm.TxContext{Origin: granterEvmAddr}}
	executor := p.GetExecutor().(*authz.PrecompileExecutor)

	method, err := p.ABI.MethodById(executor.GranterGrantsID)
	require.Nil(t, err)
	args, err := method.Inputs.Pack(granterEvmAddr, []byte{})
	require.Nil(t, err)
	ret, _, err := p.RunAndCalculateGas(&evm, granterEvmAddr, granterEvmAddr, append(executor.GranterGrantsID, args...), 1000000, nil, nil, true, false)
	require.Nil(t, err)

	outputs, err := method.Outputs.Unpack(ret)
	require.Nil(t, err)
	require.Len(t, outputs, 1)

	response := reflect.ValueOf(outputs[0])
	grants := response.FieldByName("Grants")
	require.Equal(t, 1, grants.Len())
	grant := grants.Index(0)
	require.Equal(t, granterSeiAddr.String(), grant.FieldByName("Granter").String())
	require.Equal(t, granteeSeiAddr.String(), grant.FieldByName("Grantee").String())
	authorizationJSON := string(grant.FieldByName("Authorization").Bytes())
	require.Contains(t, authorizationJSON, "@type")
	require.Contains(t, authorizationJSON, "SendAuthorization")
	require.Equal(t, expiration.Unix(), grant.FieldByName("Expiration").Int())
}

func TestGranteeGrants(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper
	granterSeiAddr, granterEvmAddr := testkeeper.MockAddressPair()
	granteeSeiAddr, granteeEvmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, granterSeiAddr, granterEvmAddr)
	k.SetAddressMapping(ctx, granteeSeiAddr, granteeEvmAddr)

	expiration := time.Unix(1893456000, 0).UTC()
	authorization := banktypes.NewSendAuthorization(sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(3000))))
	require.Nil(t, testApp.AuthzKeeper.SaveGrant(ctx, granteeSeiAddr, granterSeiAddr, authorization, expiration))

	p, err := authz.NewPrecompile(testApp.GetPrecompileKeepers())
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{StateDB: statedb, TxContext: vm.TxContext{Origin: granteeEvmAddr}}
	executor := p.GetExecutor().(*authz.PrecompileExecutor)

	method, err := p.ABI.MethodById(executor.GranteeGrantsID)
	require.Nil(t, err)
	args, err := method.Inputs.Pack(granteeEvmAddr, []byte{})
	require.Nil(t, err)
	ret, _, err := p.RunAndCalculateGas(&evm, granteeEvmAddr, granteeEvmAddr, append(executor.GranteeGrantsID, args...), 1000000, nil, nil, true, false)
	require.Nil(t, err)

	outputs, err := method.Outputs.Unpack(ret)
	require.Nil(t, err)
	require.Len(t, outputs, 1)

	response := reflect.ValueOf(outputs[0])
	grants := response.FieldByName("Grants")
	require.Equal(t, 1, grants.Len())
	grant := grants.Index(0)
	require.Equal(t, granterSeiAddr.String(), grant.FieldByName("Granter").String())
	require.Equal(t, granteeSeiAddr.String(), grant.FieldByName("Grantee").String())
	authorizationJSON := string(grant.FieldByName("Authorization").Bytes())
	require.Contains(t, authorizationJSON, "@type")
	require.Contains(t, authorizationJSON, "SendAuthorization")
	require.Equal(t, expiration.Unix(), grant.FieldByName("Expiration").Int())
}

func TestGrantExecRevoke(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper
	cdc := testApp.AppCodec()

	granterSeiAddr, granterEvmAddr := testkeeper.MockAddressPair()
	granteeSeiAddr, granteeEvmAddr := testkeeper.MockAddressPair()
	thirdSeiAddr, thirdEvmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, granterSeiAddr, granterEvmAddr)
	k.SetAddressMapping(ctx, granteeSeiAddr, granteeEvmAddr)
	k.SetAddressMapping(ctx, thirdSeiAddr, thirdEvmAddr)

	amt := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000)))
	require.Nil(t, testApp.BankKeeper.MintCoins(ctx, evmtypes.ModuleName, amt))
	require.Nil(t, testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, granterSeiAddr, amt))

	p, err := authz.NewPrecompile(testApp.GetPrecompileKeepers())
	require.Nil(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{StateDB: statedb, TxContext: vm.TxContext{Origin: granterEvmAddr}}
	executor := p.GetExecutor().(*authz.PrecompileExecutor)
	sendMsgTypeURL := sdk.MsgTypeURL(&banktypes.MsgSend{})

	// grant a send authorization to the grantee
	authorizationJSON, err := cdc.MarshalInterfaceJSON(banktypes.NewSendAuthorization(sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(500)))))
	require.Nil(t, err)
	expiration := time.Unix(1893456000, 0).UTC()

	grantMethod, err := p.ABI.MethodById(executor.GrantID)
	require.Nil(t, err)
	grantArgs, err := grantMethod.Inputs.Pack(granteeEvmAddr, authorizationJSON, expiration.Unix())
	require.Nil(t, err)

	// should error because of read only call
	_, _, err = p.RunAndCalculateGas(&evm, granterEvmAddr, granterEvmAddr, append(executor.GrantID, grantArgs...), 1000000, nil, nil, true, false)
	require.NotNil(t, err)
	// should error because of delegatecall
	_, _, err = p.RunAndCalculateGas(&evm, granterEvmAddr, granterEvmAddr, append(executor.GrantID, grantArgs...), 1000000, nil, nil, false, true)
	require.NotNil(t, err)
	// should error because caller is not associated
	_, unassociatedEvmAddr := testkeeper.MockAddressPair()
	_, _, err = p.RunAndCalculateGas(&evm, unassociatedEvmAddr, unassociatedEvmAddr, append(executor.GrantID, grantArgs...), 1000000, nil, nil, false, false)
	require.NotNil(t, err)
	// should error because the authorization JSON is invalid
	badAuthorizationArgs, err := grantMethod.Inputs.Pack(granteeEvmAddr, []byte("{"), expiration.Unix())
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, granterEvmAddr, granterEvmAddr, append(executor.GrantID, badAuthorizationArgs...), 1000000, nil, nil, false, false)
	require.NotNil(t, err)

	ret, _, err := p.RunAndCalculateGas(&evm, granterEvmAddr, granterEvmAddr, append(executor.GrantID, grantArgs...), 1000000, nil, nil, false, false)
	require.Nil(t, err)
	outputs, err := grantMethod.Outputs.Unpack(ret)
	require.Nil(t, err)
	require.True(t, outputs[0].(bool))
	authorization, _ := testApp.AuthzKeeper.GetCleanAuthorization(statedb.Ctx(), granteeSeiAddr, granterSeiAddr, sendMsgTypeURL)
	require.NotNil(t, authorization)

	// exec a send on behalf of the granter as the grantee
	sendJSON, err := cdc.MarshalInterfaceJSON(&banktypes.MsgSend{
		FromAddress: granterSeiAddr.String(),
		ToAddress:   granteeSeiAddr.String(),
		Amount:      sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(300))),
	})
	require.Nil(t, err)
	execMethod, err := p.ABI.MethodById(executor.ExecID)
	require.Nil(t, err)
	execArgs, err := execMethod.Inputs.Pack([][]byte{sendJSON})
	require.Nil(t, err)

	// should error because a third party has no grant for the granter's send
	_, _, err = p.RunAndCalculateGas(&evm, thirdEvmAddr, thirdEvmAddr, append(executor.ExecID, execArgs...), 1000000, nil, nil, false, false)
	require.NotNil(t, err)

	ret, _, err = p.RunAndCalculateGas(&evm, granteeEvmAddr, granteeEvmAddr, append(executor.ExecID, execArgs...), 1000000, nil, nil, false, false)
	require.Nil(t, err)
	execOutputs, err := execMethod.Outputs.Unpack(ret)
	require.Nil(t, err)
	require.Len(t, execOutputs, 1)
	require.Equal(t, int64(300), testApp.BankKeeper.GetBalance(statedb.Ctx(), granteeSeiAddr, "usei").Amount.Int64())
	require.Equal(t, int64(700), testApp.BankKeeper.GetBalance(statedb.Ctx(), granterSeiAddr, "usei").Amount.Int64())

	// exec of an EVM message is rejected
	evmMsgJSON, err := cdc.MarshalInterfaceJSON(&evmtypes.MsgEVMTransaction{})
	require.Nil(t, err)
	evmExecArgs, err := execMethod.Inputs.Pack([][]byte{evmMsgJSON})
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, granteeEvmAddr, granteeEvmAddr, append(executor.ExecID, evmExecArgs...), 1000000, nil, nil, false, false)
	require.NotNil(t, err)

	// exec of a nested MsgExec containing an EVM message is rejected
	nestedExec := authztypes.NewMsgExec(granteeSeiAddr, []sdk.Msg{&evmtypes.MsgEVMTransaction{}})
	nestedExecJSON, err := cdc.MarshalInterfaceJSON(&nestedExec)
	require.Nil(t, err)
	nestedExecArgs, err := execMethod.Inputs.Pack([][]byte{nestedExecJSON})
	require.Nil(t, err)
	_, _, err = p.RunAndCalculateGas(&evm, granteeEvmAddr, granteeEvmAddr, append(executor.ExecID, nestedExecArgs...), 1000000, nil, nil, false, false)
	require.NotNil(t, err)

	// revoke the grant
	revokeMethod, err := p.ABI.MethodById(executor.RevokeID)
	require.Nil(t, err)
	revokeArgs, err := revokeMethod.Inputs.Pack(granteeEvmAddr, sendMsgTypeURL)
	require.Nil(t, err)
	ret, _, err = p.RunAndCalculateGas(&evm, granterEvmAddr, granterEvmAddr, append(executor.RevokeID, revokeArgs...), 1000000, nil, nil, false, false)
	require.Nil(t, err)
	outputs, err = revokeMethod.Outputs.Unpack(ret)
	require.Nil(t, err)
	require.True(t, outputs[0].(bool))
	authorization, _ = testApp.AuthzKeeper.GetCleanAuthorization(statedb.Ctx(), granteeSeiAddr, granterSeiAddr, sendMsgTypeURL)
	require.Nil(t, authorization)
}
