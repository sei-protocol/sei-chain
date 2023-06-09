package aclwasmmapping

import (
	"testing"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/accesscontrol"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/bank/types"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestWasmDependencyGenerator(t *testing.T) {
	wasmDependencyGenerator := NewWasmDependencyGenerator().GetWasmDependencyGenerators()
	// verify that there's one entry, for bank send
	require.Equal(t, 1, len(wasmDependencyGenerator))
	// check that bank send generator is in the map
	_, ok := wasmDependencyGenerator[acltypes.GenerateMessageKey(&wasmtypes.MsgExecuteContract{})]
	require.True(t, ok)
}

func TestGeneratorInvalidMessageTypes(t *testing.T) {
	accs := authtypes.GenesisAccounts{}
	balances := []types.Balance{}

	app := simapp.SetupWithGenesisAccounts(accs, balances...)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	oracleVote := oracletypes.MsgAggregateExchangeRateVote{
		ExchangeRates: "1usei",
		Feeder:        "test",
		Validator:     "validator",
	}

	_, err := NewWasmDependencyGenerator().WasmExecuteContractGenerator(app.AccessControlKeeper, ctx, &oracleVote)
	require.Error(t, err)
}

func TestMsgBeginWasmExecuteGenerator(t *testing.T) {
	priv1 := secp256k1.GenPrivKey()
	addr1 := sdk.AccAddress(priv1.PubKey().Address())
	priv2 := secp256k1.GenPrivKey()
	addr2 := sdk.AccAddress(priv2.PubKey().Address())
	coins := sdk.Coins{sdk.NewInt64Coin("foocoin", 10)}

	acc1 := &authtypes.BaseAccount{
		Address: addr1.String(),
	}
	acc2 := &authtypes.BaseAccount{
		Address: addr2.String(),
	}
	accs := authtypes.GenesisAccounts{acc1, acc2}
	balances := []types.Balance{
		{
			Address: addr1.String(),
			Coins:   coins,
		},
		{
			Address: addr2.String(),
			Coins:   coins,
		},
	}

	app := simapp.SetupWithGenesisAccounts(accs, balances...)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	execMsg := wasmtypes.MsgExecuteContract{
		Sender:   addr1.String(),
		Contract: addr2.String(),
		Msg:      wasmtypes.RawContractMessage([]byte("{\"test\":{}}")),
		Funds:    coins,
	}

	accessOps, err := NewWasmDependencyGenerator().WasmExecuteContractGenerator(app.AccessControlKeeper, ctx, &execMsg)
	require.NoError(t, err)
	err = acltypes.ValidateAccessOps(accessOps)
	require.NoError(t, err)
}

func TestGeneratorInvalidMsgFormat(t *testing.T) {
	priv1 := secp256k1.GenPrivKey()
	addr1 := sdk.AccAddress(priv1.PubKey().Address())
	coins := sdk.Coins{sdk.NewInt64Coin("foocoin", 10)}

	acc1 := &authtypes.BaseAccount{
		Address: addr1.String(),
	}
	accs := authtypes.GenesisAccounts{acc1}
	balances := []types.Balance{
		{
			Address: addr1.String(),
			Coins:   coins,
		},
	}

	app := simapp.SetupWithGenesisAccounts(accs, balances...)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	execMsg := wasmtypes.MsgExecuteContract{
		Sender:   addr1.String(),
		Contract: addr1.String(),
		Msg:      wasmtypes.RawContractMessage([]byte("InvalidMsgFormat")), // Invalid Msg format
		Funds:    coins,
	}

	_, err := NewWasmDependencyGenerator().WasmExecuteContractGenerator(app.AccessControlKeeper, ctx, &execMsg)
	require.Error(t, err)
}

func TestGeneratorInvalidContractAddrFormat(t *testing.T) {
	priv1 := secp256k1.GenPrivKey()
	addr1 := sdk.AccAddress(priv1.PubKey().Address())
	coins := sdk.Coins{sdk.NewInt64Coin("foocoin", 10)}

	acc1 := &authtypes.BaseAccount{
		Address: addr1.String(),
	}
	accs := authtypes.GenesisAccounts{acc1}
	balances := []types.Balance{
		{
			Address: addr1.String(),
			Coins:   coins,
		},
	}

	app := simapp.SetupWithGenesisAccounts(accs, balances...)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	execMsg := wasmtypes.MsgExecuteContract{
		Sender:   addr1.String(),
		Contract: "invalidAddrFormat", // Invalid contract address format
		Msg:      wasmtypes.RawContractMessage([]byte("{\"test\":{}}")),
		Funds:    coins,
	}

	_, err := NewWasmDependencyGenerator().WasmExecuteContractGenerator(app.AccessControlKeeper, ctx, &execMsg)
	require.Error(t, err)
}

func TestGeneratorGetRawWasmDependencyMappingError(t *testing.T) {
	// Setup
	priv1 := secp256k1.GenPrivKey()
	addr1 := sdk.AccAddress(priv1.PubKey().Address())
	coins := sdk.Coins{sdk.NewInt64Coin("foocoin", 10)}
	msg := wasmtypes.RawContractMessage([]byte("{\"test\":{}}"))

	priv2 := secp256k1.GenPrivKey()
	contractAddr := sdk.AccAddress(priv2.PubKey().Address())

	acc1 := &authtypes.BaseAccount{
		Address: addr1.String(),
	}
	accs := authtypes.GenesisAccounts{acc1}
	balances := []types.Balance{
		{
			Address: addr1.String(),
			Coins:   coins,
		},
	}
	app := simapp.SetupWithGenesisAccounts(accs, balances...)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	execMsg := wasmtypes.MsgExecuteContract{
		Sender:   addr1.String(),
		Contract: contractAddr.String(),
		Msg:      msg,
		Funds:    coins,
	}

	// Expect an error from the generator
	app.AccessControlKeeper.SetWasmDependencyMapping(ctx, accesscontrol.WasmDependencyMapping{
		ContractAddress: contractAddr.String(),
		BaseAccessOps: []*accesscontrol.WasmAccessOperation{
			{
				Operation: &accesscontrol.AccessOperation{
					AccessType:         accesscontrol.AccessType_WRITE,
					ResourceType:       accesscontrol.ResourceType_KV,
					IdentifierTemplate: "ACBC",
				},
				SelectorType: accesscontrol.AccessOperationSelectorType_JQ_LENGTH_PREFIXED_ADDRESS},
			{
				Operation: acltypes.CommitAccessOp(),
			},
		},
		ExecuteAccessOps: []*accesscontrol.WasmAccessOperations{
			{
				MessageName: "abc",
				WasmOperations: []*accesscontrol.WasmAccessOperation{
					{
						Operation: &accesscontrol.AccessOperation{
							AccessType:         accesscontrol.AccessType_WRITE,
							ResourceType:       accesscontrol.ResourceType_KV,
							IdentifierTemplate: "ACCXD",
						},
						SelectorType: accesscontrol.AccessOperationSelectorType_SENDER_LENGTH_PREFIXED_ADDRESS,
					},
				},
			},
		},
	})

	_, err := NewWasmDependencyGenerator().WasmExecuteContractGenerator(app.AccessControlKeeper, ctx, &execMsg)
	require.Error(t, err)
}
