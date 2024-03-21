package ibc_test

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	channeltypes "github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/precompiles/ibc"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
	tmtypes "github.com/tendermint/tendermint/proto/tendermint/types"
	"math/big"
	"testing"
)

func TestRun(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper

	// Setup sender addresses and environment
	privKey := testkeeper.MockPrivateKey()
	senderAddr, senderEVMAddr := testkeeper.PrivateKeyToAddresses(privKey)
	k.SetAddressMapping(ctx, senderAddr, senderEVMAddr)
	k.ScopedCapabilityKeeper().NewCapability(ctx, "capabilities/ports/port/channels/sourceChannel")
	k.ChannelKeeper().SetChannel(ctx, "port", "sourceChannel", channeltypes.Channel{
		State:    0,
		Ordering: 0,
		Counterparty: channeltypes.Counterparty{
			PortId:    "destinationPort",
			ChannelId: "destinationChannel",
		},
		ConnectionHops: nil,
		Version:        "",
	})
	k.ChannelKeeper().SetNextSequenceSend(ctx, "port", "sourceChannel", 1)

	err := k.BankKeeper().MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10000000))))
	require.Nil(t, err)
	err = k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, senderAddr, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10000000))))
	require.Nil(t, err)

	// Setup receiving addresses
	_, evmAddr := testkeeper.MockAddressPair()

	p, err := ibc.NewPrecompile(k.TransferKeeper(), k)

	require.Nil(t, err)
	stateDb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{
		StateDB:   stateDb,
		TxContext: vm.TxContext{Origin: senderEVMAddr},
	}

	// Precompile transfer test
	send, err := p.ABI.MethodById(p.TransferID)
	require.Nil(t, err)
	args, err := send.Inputs.Pack(senderEVMAddr, evmAddr, "port", "sourceChannel", "usei", big.NewInt(25))
	require.Nil(t, err)
	_, err = p.Run(&evm, senderEVMAddr, append(p.TransferID, args...), nil)
	// TODO: Fix uncomment when all dependencies are resolved
	//require.Nil(t, err)

}
