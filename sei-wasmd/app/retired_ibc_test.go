package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
)

func TestRetiredIBCStateRemainsMountedWithoutModuleWiring(t *testing.T) {
	testApp, _ := setup(t, false, 0)

	for _, name := range []string{
		retiredIBCStoreName,
		retiredTransferName,
		capabilityStoreName,
		feegrantStoreKeyName,
	} {
		require.NotNil(t, testApp.keys[name])
	}

	_, basicWired := ModuleBasics[retiredIBCStoreName]
	_, moduleWired := testApp.mm.Modules[retiredIBCStoreName]
	require.False(t, basicWired)
	require.False(t, moduleWired)
	require.NotContains(t, testApp.mm.OrderInitGenesis, retiredIBCStoreName)
	for _, typeURL := range testApp.interfaceRegistry.ListImplementations("cosmos.base.v1beta1.Msg") {
		require.Falsef(t, strings.HasPrefix(typeURL, "/ibc."), "IBC message type remains registered: %s", typeURL)
	}
}

func TestRetiredIBCStoreQueriesAreUnavailable(t *testing.T) {
	testApp, _ := setup(t, false, 0)

	for _, storeName := range []string{retiredIBCStoreName, retiredTransferName, capabilityStoreName} {
		response, err := testApp.Query(context.Background(), &abci.RequestQuery{
			Path: "/store/" + storeName + "/key",
		})
		require.NoError(t, err)
		require.Equal(t, "ibc", response.Codespace)
		require.Equal(t, uint32(103), response.Code)
		require.Equal(t, "ibc module is deprecated", response.Log)
	}
}

func TestRetiredTransferModuleAccountIsMaterialized(t *testing.T) {
	testApp, genesisState := setup(t, true, 0)
	state, err := json.Marshal(genesisState)
	require.NoError(t, err)

	_, err = testApp.InitChain(&abci.RequestInitChain{AppStateBytes: state})
	require.NoError(t, err)
	ctx := testApp.GetContextForDeliverTx(nil).WithBlockHeader(tmproto.Header{})

	account := testApp.accountKeeper.GetAccount(ctx, authtypes.NewModuleAddress(retiredTransferName))
	moduleAccount, ok := account.(authtypes.ModuleAccountI)
	require.True(t, ok)
	require.Equal(t, retiredTransferName, moduleAccount.GetName())
	require.ElementsMatch(t, []string{authtypes.Minter, authtypes.Burner}, moduleAccount.GetPermissions())
}
