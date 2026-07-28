package authz_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/authz"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
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
