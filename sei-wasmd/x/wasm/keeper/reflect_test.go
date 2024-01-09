package keeper

import (
	"encoding/json"
	"io/ioutil"
	"strings"
	"testing"

	"github.com/golang/protobuf/proto"

	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/CosmWasm/wasmd/x/wasm/types"
)

// ReflectInitMsg is {}

// ReflectHandleMsg is used to encode handle messages
type ReflectHandleMsg struct {
	Reflect       *reflectPayload    `json:"reflect_msg,omitempty"`
	ReflectSubMsg *reflectSubPayload `json:"reflect_sub_msg,omitempty"`
	Change        *ownerPayload      `json:"change_owner,omitempty"`
}

type ownerPayload struct {
	Owner sdk.Address `json:"owner"`
}

type reflectPayload struct {
	Msgs []wasmvmtypes.CosmosMsg `json:"msgs"`
}

type reflectSubPayload struct {
	Msgs []wasmvmtypes.SubMsg `json:"msgs"`
}

// ReflectQueryMsg is used to encode query messages
type ReflectQueryMsg struct {
	Owner        *struct{}   `json:"owner,omitempty"`
	Capitalized  *Text       `json:"capitalized,omitempty"`
	Chain        *ChainQuery `json:"chain,omitempty"`
	SubMsgResult *SubCall    `json:"sub_msg_result,omitempty"`
}

type ChainQuery struct {
	Request *wasmvmtypes.QueryRequest `json:"request,omitempty"`
}

type Text struct {
	Text string `json:"text"`
}

type SubCall struct {
	ID uint64 `json:"id"`
}

type OwnerResponse struct {
	Owner string `json:"owner,omitempty"`
}

type ChainResponse struct {
	Data []byte `json:"data,omitempty"`
}

func buildReflectQuery(t *testing.T, query *ReflectQueryMsg) []byte {
	bz, err := json.Marshal(query)
	require.NoError(t, err)
	return bz
}

func mustParse(t *testing.T, data []byte, res interface{}) {
	err := json.Unmarshal(data, res)
	require.NoError(t, err)
}

const ReflectFeatures = "staking,mask,stargate"

func TestReflectContractSend(t *testing.T) {
	cdc := MakeEncodingConfig(t).Marshaler
	ctx, keepers := CreateTestInput(t, false, ReflectFeatures, WithMessageEncoders(reflectEncoders(cdc)))
	accKeeper, keeper, bankKeeper := keepers.AccountKeeper, keepers.ContractKeeper, keepers.BankKeeper

	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
	creator := keepers.Faucet.NewFundedAccount(ctx, deposit...)
	_, _, bob := keyPubAddr()

	// upload reflect code
	reflectCode, err := ioutil.ReadFile("./testdata/reflect.wasm")
	require.NoError(t, err)
	reflectID, err := keeper.Create(ctx, creator, reflectCode, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(1), reflectID)

	// upload hackatom escrow code
	escrowCode, err := ioutil.ReadFile("./testdata/hackatom.wasm")
	require.NoError(t, err)
	escrowID, err := keeper.Create(ctx, creator, escrowCode, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(2), escrowID)

	// creator instantiates a contract and gives it tokens
	reflectStart := sdk.NewCoins(sdk.NewInt64Coin("denom", 40000))
	reflectAddr, _, err := keeper.Instantiate(ctx, reflectID, creator, nil, []byte("{}"), "reflect contract 2", reflectStart)
	require.NoError(t, err)
	require.NotEmpty(t, reflectAddr)

	// now we set contract as verifier of an escrow
	initMsg := HackatomExampleInitMsg{
		Verifier:    reflectAddr,
		Beneficiary: bob,
	}
	initMsgBz, err := json.Marshal(initMsg)
	require.NoError(t, err)
	escrowStart := sdk.NewCoins(sdk.NewInt64Coin("denom", 25000))
	escrowAddr, _, err := keeper.Instantiate(ctx, escrowID, creator, nil, initMsgBz, "escrow contract 2", escrowStart)
	require.NoError(t, err)
	require.NotEmpty(t, escrowAddr)

	// let's make sure all balances make sense
	checkAccount(t, ctx, accKeeper, bankKeeper, creator, sdk.NewCoins(sdk.NewInt64Coin("denom", 35000))) // 100k - 40k - 25k
	checkAccount(t, ctx, accKeeper, bankKeeper, reflectAddr, reflectStart)
	checkAccount(t, ctx, accKeeper, bankKeeper, escrowAddr, escrowStart)
	checkAccount(t, ctx, accKeeper, bankKeeper, bob, nil)

	// now for the trick.... we reflect a message through the reflect to call the escrow
	// we also send an additional 14k tokens there.
	// this should reduce the reflect balance by 14k (to 26k)
	// this 14k is added to the escrow, then the entire balance is sent to bob (total: 39k)
	approveMsg := []byte(`{"release":{}}`)
	msgs := []wasmvmtypes.CosmosMsg{{
		Wasm: &wasmvmtypes.WasmMsg{
			Execute: &wasmvmtypes.ExecuteMsg{
				ContractAddr: escrowAddr.String(),
				Msg:          approveMsg,
				Funds: []wasmvmtypes.Coin{{
					Denom:  "denom",
					Amount: "14000",
				}},
			},
		},
	}}
	reflectSend := ReflectHandleMsg{
		Reflect: &reflectPayload{
			Msgs: msgs,
		},
	}
	reflectSendBz, err := json.Marshal(reflectSend)
	require.NoError(t, err)
	_, err = keeper.Execute(ctx, reflectAddr, creator, reflectSendBz, nil)
	require.NoError(t, err)

	// did this work???
	checkAccount(t, ctx, accKeeper, bankKeeper, creator, sdk.NewCoins(sdk.NewInt64Coin("denom", 35000)))     // same as before
	checkAccount(t, ctx, accKeeper, bankKeeper, reflectAddr, sdk.NewCoins(sdk.NewInt64Coin("denom", 26000))) // 40k - 14k (from send)
	checkAccount(t, ctx, accKeeper, bankKeeper, escrowAddr, sdk.Coins{})                                     // emptied reserved
	checkAccount(t, ctx, accKeeper, bankKeeper, bob, sdk.NewCoins(sdk.NewInt64Coin("denom", 39000)))         // all escrow of 25k + 14k
}

func TestReflectCustomMsg(t *testing.T) {
	cdc := MakeEncodingConfig(t).Marshaler
	ctx, keepers := CreateTestInput(t, false, ReflectFeatures, WithMessageEncoders(reflectEncoders(cdc)), WithQueryPlugins(reflectPlugins()))
	accKeeper, keeper, bankKeeper := keepers.AccountKeeper, keepers.ContractKeeper, keepers.BankKeeper

	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
	creator := keepers.Faucet.NewFundedAccount(ctx, deposit...)
	bob := keepers.Faucet.NewFundedAccount(ctx, deposit...)
	_, _, fred := keyPubAddr()

	// upload code
	reflectCode, err := ioutil.ReadFile("./testdata/reflect.wasm")
	require.NoError(t, err)
	codeID, err := keeper.Create(ctx, creator, reflectCode, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(1), codeID)

	// creator instantiates a contract and gives it tokens
	contractStart := sdk.NewCoins(sdk.NewInt64Coin("denom", 40000))
	contractAddr, _, err := keeper.Instantiate(ctx, codeID, creator, nil, []byte("{}"), "reflect contract 1", contractStart)
	require.NoError(t, err)
	require.NotEmpty(t, contractAddr)

	// set owner to bob
	transfer := ReflectHandleMsg{
		Change: &ownerPayload{
			Owner: bob,
		},
	}
	transferBz, err := json.Marshal(transfer)
	require.NoError(t, err)
	_, err = keeper.Execute(ctx, contractAddr, creator, transferBz, nil)
	require.NoError(t, err)

	// check some account values
	checkAccount(t, ctx, accKeeper, bankKeeper, contractAddr, contractStart)
	checkAccount(t, ctx, accKeeper, bankKeeper, bob, deposit)
	checkAccount(t, ctx, accKeeper, bankKeeper, fred, nil)

	// bob can send contract's tokens to fred (using SendMsg)
	msgs := []wasmvmtypes.CosmosMsg{{
		Bank: &wasmvmtypes.BankMsg{
			Send: &wasmvmtypes.SendMsg{
				ToAddress: fred.String(),
				Amount: []wasmvmtypes.Coin{{
					Denom:  "denom",
					Amount: "15000",
				}},
			},
		},
	}}
	reflectSend := ReflectHandleMsg{
		Reflect: &reflectPayload{
			Msgs: msgs,
		},
	}
	reflectSendBz, err := json.Marshal(reflectSend)
	require.NoError(t, err)
	_, err = keeper.Execute(ctx, contractAddr, bob, reflectSendBz, nil)
	require.NoError(t, err)

	// fred got coins
	checkAccount(t, ctx, accKeeper, bankKeeper, fred, sdk.NewCoins(sdk.NewInt64Coin("denom", 15000)))
	// contract lost them
	checkAccount(t, ctx, accKeeper, bankKeeper, contractAddr, sdk.NewCoins(sdk.NewInt64Coin("denom", 25000)))
	checkAccount(t, ctx, accKeeper, bankKeeper, bob, deposit)

	// construct an opaque message
	var sdkSendMsg sdk.Msg = &banktypes.MsgSend{
		FromAddress: contractAddr.String(),
		ToAddress:   fred.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("denom", 23000)),
	}
	opaque, err := toReflectRawMsg(cdc, sdkSendMsg)
	require.NoError(t, err)
	reflectOpaque := ReflectHandleMsg{
		Reflect: &reflectPayload{
			Msgs: []wasmvmtypes.CosmosMsg{opaque},
		},
	}
	reflectOpaqueBz, err := json.Marshal(reflectOpaque)
	require.NoError(t, err)

	_, err = keeper.Execute(ctx, contractAddr, bob, reflectOpaqueBz, nil)
	require.NoError(t, err)

	// fred got more coins
	checkAccount(t, ctx, accKeeper, bankKeeper, fred, sdk.NewCoins(sdk.NewInt64Coin("denom", 38000)))
	// contract lost them
	checkAccount(t, ctx, accKeeper, bankKeeper, contractAddr, sdk.NewCoins(sdk.NewInt64Coin("denom", 2000)))
	checkAccount(t, ctx, accKeeper, bankKeeper, bob, deposit)
}

func TestMaskReflectCustomQuery(t *testing.T) {
	cdc := MakeEncodingConfig(t).Marshaler
	ctx, keepers := CreateTestInput(t, false, ReflectFeatures, WithMessageEncoders(reflectEncoders(cdc)), WithQueryPlugins(reflectPlugins()))
	keeper := keepers.WasmKeeper

	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
	creator := keepers.Faucet.NewFundedAccount(ctx, deposit...)

	// upload code
	reflectCode, err := ioutil.ReadFile("./testdata/reflect.wasm")
	require.NoError(t, err)
	codeID, err := keepers.ContractKeeper.Create(ctx, creator, reflectCode, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(1), codeID)

	// creator instantiates a contract and gives it tokens
	contractStart := sdk.NewCoins(sdk.NewInt64Coin("denom", 40000))
	contractAddr, _, err := keepers.ContractKeeper.Instantiate(ctx, codeID, creator, nil, []byte("{}"), "reflect contract 1", contractStart)
	require.NoError(t, err)
	require.NotEmpty(t, contractAddr)

	// let's perform a normal query of state
	ownerQuery := ReflectQueryMsg{
		Owner: &struct{}{},
	}
	ownerQueryBz, err := json.Marshal(ownerQuery)
	require.NoError(t, err)
	ownerRes, err := keeper.QuerySmart(ctx, contractAddr, ownerQueryBz)
	require.NoError(t, err)
	var res OwnerResponse
	err = json.Unmarshal(ownerRes, &res)
	require.NoError(t, err)
	assert.Equal(t, res.Owner, creator.String())

	// and now making use of the custom querier callbacks
	customQuery := ReflectQueryMsg{
		Capitalized: &Text{
			Text: "all Caps noW",
		},
	}
	customQueryBz, err := json.Marshal(customQuery)
	require.NoError(t, err)
	custom, err := keeper.QuerySmart(ctx, contractAddr, customQueryBz)
	require.NoError(t, err)
	var resp capitalizedResponse
	err = json.Unmarshal(custom, &resp)
	require.NoError(t, err)
	assert.Equal(t, resp.Text, "ALL CAPS NOW")
}

func TestReflectStargateQuery(t *testing.T) {
	cdc := MakeEncodingConfig(t).Marshaler
	ctx, keepers := CreateTestInput(t, false, ReflectFeatures, WithMessageEncoders(reflectEncoders(cdc)), WithQueryPlugins(reflectPlugins()))
	keeper := keepers.WasmKeeper

	funds := sdk.NewCoins(sdk.NewInt64Coin("denom", 320000))
	contractStart := sdk.NewCoins(sdk.NewInt64Coin("denom", 40000))
	expectedBalance := funds.Sub(contractStart)
	creator := keepers.Faucet.NewFundedAccount(ctx, funds...)

	// upload code
	reflectCode, err := ioutil.ReadFile("./testdata/reflect.wasm")
	require.NoError(t, err)
	codeID, err := keepers.ContractKeeper.Create(ctx, creator, reflectCode, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(1), codeID)

	// creator instantiates a contract and gives it tokens
	contractAddr, _, err := keepers.ContractKeeper.Instantiate(ctx, codeID, creator, nil, []byte("{}"), "reflect contract 1", contractStart)
	require.NoError(t, err)
	require.NotEmpty(t, contractAddr)

	// first, normal query for the bank balance (to make sure our query is proper)
	bankQuery := wasmvmtypes.QueryRequest{
		Bank: &wasmvmtypes.BankQuery{
			AllBalances: &wasmvmtypes.AllBalancesQuery{
				Address: creator.String(),
			},
		},
	}
	simpleQueryBz, err := json.Marshal(ReflectQueryMsg{
		Chain: &ChainQuery{Request: &bankQuery},
	})
	require.NoError(t, err)
	simpleRes, err := keeper.QuerySmart(ctx, contractAddr, simpleQueryBz)
	require.NoError(t, err)
	var simpleChain ChainResponse
	mustParse(t, simpleRes, &simpleChain)
	var simpleBalance wasmvmtypes.AllBalancesResponse
	mustParse(t, simpleChain.Data, &simpleBalance)
	require.Equal(t, len(expectedBalance), len(simpleBalance.Amount))
	assert.Equal(t, simpleBalance.Amount[0].Amount, expectedBalance[0].Amount.String())
	assert.Equal(t, simpleBalance.Amount[0].Denom, expectedBalance[0].Denom)
}

func TestReflectInvalidStargateQuery(t *testing.T) {
	cdc := MakeEncodingConfig(t).Marshaler
	ctx, keepers := CreateTestInput(t, false, ReflectFeatures, WithMessageEncoders(reflectEncoders(cdc)), WithQueryPlugins(reflectPlugins()))
	keeper := keepers.WasmKeeper

	funds := sdk.NewCoins(sdk.NewInt64Coin("denom", 320000))
	contractStart := sdk.NewCoins(sdk.NewInt64Coin("denom", 40000))
	creator := keepers.Faucet.NewFundedAccount(ctx, funds...)

	// upload code
	reflectCode, err := ioutil.ReadFile("./testdata/reflect.wasm")
	require.NoError(t, err)
	codeID, err := keepers.ContractKeeper.Create(ctx, creator, reflectCode, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(1), codeID)

	// creator instantiates a contract and gives it tokens
	contractAddr, _, err := keepers.ContractKeeper.Instantiate(ctx, codeID, creator, nil, []byte("{}"), "reflect contract 1", contractStart)
	require.NoError(t, err)
	require.NotEmpty(t, contractAddr)

	// now, try to build a protobuf query
	protoQuery := banktypes.QueryAllBalancesRequest{
		Address: creator.String(),
	}
	protoQueryBin, err := proto.Marshal(&protoQuery)
	protoRequest := wasmvmtypes.QueryRequest{
		Stargate: &wasmvmtypes.StargateQuery{
			Path: "/cosmos.bank.v1beta1.Query/AllBalances",
			Data: protoQueryBin,
		},
	}
	protoQueryBz, err := json.Marshal(ReflectQueryMsg{
		Chain: &ChainQuery{Request: &protoRequest},
	})
	require.NoError(t, err)

	// make a query on the chain, should be blacklisted
	_, err = keeper.QuerySmart(ctx, contractAddr, protoQueryBz)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Stargate queries are disabled")

	// now, try to build a protobuf query
	protoRequest = wasmvmtypes.QueryRequest{
		Stargate: &wasmvmtypes.StargateQuery{
			Path: "/cosmos.tx.v1beta1.Service/GetTx",
			Data: []byte{},
		},
	}
	protoQueryBz, err = json.Marshal(ReflectQueryMsg{
		Chain: &ChainQuery{Request: &protoRequest},
	})
	require.NoError(t, err)

	// make a query on the chain, should be blacklisted
	_, err = keeper.QuerySmart(ctx, contractAddr, protoQueryBz)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Stargate queries are disabled")

	// and another one
	protoRequest = wasmvmtypes.QueryRequest{
		Stargate: &wasmvmtypes.StargateQuery{
			Path: "/cosmos.base.tendermint.v1beta1.Service/GetNodeInfo",
			Data: []byte{},
		},
	}
	protoQueryBz, err = json.Marshal(ReflectQueryMsg{
		Chain: &ChainQuery{Request: &protoRequest},
	})
	require.NoError(t, err)

	// make a query on the chain, should be blacklisted
	_, err = keeper.QuerySmart(ctx, contractAddr, protoQueryBz)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Stargate queries are disabled")
}

type reflectState struct {
	Owner string `json:"owner"`
}

func TestMaskReflectWasmQueries(t *testing.T) {
	cdc := MakeEncodingConfig(t).Marshaler
	ctx, keepers := CreateTestInput(t, false, ReflectFeatures, WithMessageEncoders(reflectEncoders(cdc)), WithQueryPlugins(reflectPlugins()))
	keeper := keepers.WasmKeeper

	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
	creator := keepers.Faucet.NewFundedAccount(ctx, deposit...)

	// upload reflect code
	reflectCode, err := ioutil.ReadFile("./testdata/reflect.wasm")
	require.NoError(t, err)
	reflectID, err := keepers.ContractKeeper.Create(ctx, creator, reflectCode, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(1), reflectID)

	// creator instantiates a contract and gives it tokens
	reflectStart := sdk.NewCoins(sdk.NewInt64Coin("denom", 40000))
	reflectAddr, _, err := keepers.ContractKeeper.Instantiate(ctx, reflectID, creator, nil, []byte("{}"), "reflect contract 2", reflectStart)
	require.NoError(t, err)
	require.NotEmpty(t, reflectAddr)

	// for control, let's make some queries directly on the reflect
	ownerQuery := buildReflectQuery(t, &ReflectQueryMsg{Owner: &struct{}{}})
	res, err := keeper.QuerySmart(ctx, reflectAddr, ownerQuery)
	require.NoError(t, err)
	var ownerRes OwnerResponse
	mustParse(t, res, &ownerRes)
	require.Equal(t, ownerRes.Owner, creator.String())

	// and a raw query: cosmwasm_storage::Singleton uses 2 byte big-endian length-prefixed to store data
	configKey := append([]byte{0, 6}, []byte("config")...)
	raw := keeper.QueryRaw(ctx, reflectAddr, configKey)
	var stateRes reflectState
	mustParse(t, raw, &stateRes)
	require.Equal(t, stateRes.Owner, creator.String())

	// now, let's reflect a smart query into the x/wasm handlers and see if we get the same result
	reflectOwnerQuery := ReflectQueryMsg{Chain: &ChainQuery{Request: &wasmvmtypes.QueryRequest{Wasm: &wasmvmtypes.WasmQuery{
		Smart: &wasmvmtypes.SmartQuery{
			ContractAddr: reflectAddr.String(),
			Msg:          ownerQuery,
		},
	}}}}
	reflectOwnerBin := buildReflectQuery(t, &reflectOwnerQuery)
	res, err = keeper.QuerySmart(ctx, reflectAddr, reflectOwnerBin)
	require.NoError(t, err)
	// first we pull out the data from chain response, before parsing the original response
	var reflectRes ChainResponse
	mustParse(t, res, &reflectRes)
	var reflectOwnerRes OwnerResponse
	mustParse(t, reflectRes.Data, &reflectOwnerRes)
	require.Equal(t, reflectOwnerRes.Owner, creator.String())

	// and with queryRaw
	reflectStateQuery := ReflectQueryMsg{Chain: &ChainQuery{Request: &wasmvmtypes.QueryRequest{Wasm: &wasmvmtypes.WasmQuery{
		Raw: &wasmvmtypes.RawQuery{
			ContractAddr: reflectAddr.String(),
			Key:          configKey,
		},
	}}}}
	reflectStateBin := buildReflectQuery(t, &reflectStateQuery)
	res, err = keeper.QuerySmart(ctx, reflectAddr, reflectStateBin)
	require.NoError(t, err)
	// first we pull out the data from chain response, before parsing the original response
	var reflectRawRes ChainResponse
	mustParse(t, res, &reflectRawRes)
	// now, with the raw data, we can parse it into state
	var reflectStateRes reflectState
	mustParse(t, reflectRawRes.Data, &reflectStateRes)
	require.Equal(t, reflectStateRes.Owner, creator.String())
}

func TestWasmRawQueryWithNil(t *testing.T) {
	cdc := MakeEncodingConfig(t).Marshaler
	ctx, keepers := CreateTestInput(t, false, ReflectFeatures, WithMessageEncoders(reflectEncoders(cdc)), WithQueryPlugins(reflectPlugins()))
	keeper := keepers.WasmKeeper

	deposit := sdk.NewCoins(sdk.NewInt64Coin("denom", 100000))
	creator := keepers.Faucet.NewFundedAccount(ctx, deposit...)

	// upload reflect code
	reflectCode, err := ioutil.ReadFile("./testdata/reflect.wasm")
	require.NoError(t, err)
	reflectID, err := keepers.ContractKeeper.Create(ctx, creator, reflectCode, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(1), reflectID)

	// creator instantiates a contract and gives it tokens
	reflectStart := sdk.NewCoins(sdk.NewInt64Coin("denom", 40000))
	reflectAddr, _, err := keepers.ContractKeeper.Instantiate(ctx, reflectID, creator, nil, []byte("{}"), "reflect contract 2", reflectStart)
	require.NoError(t, err)
	require.NotEmpty(t, reflectAddr)

	// control: query directly
	missingKey := []byte{0, 1, 2, 3, 4}
	raw := keeper.QueryRaw(ctx, reflectAddr, missingKey)
	require.Nil(t, raw)

	// and with queryRaw
	reflectQuery := ReflectQueryMsg{Chain: &ChainQuery{Request: &wasmvmtypes.QueryRequest{Wasm: &wasmvmtypes.WasmQuery{
		Raw: &wasmvmtypes.RawQuery{
			ContractAddr: reflectAddr.String(),
			Key:          missingKey,
		},
	}}}}
	reflectStateBin := buildReflectQuery(t, &reflectQuery)
	res, err := keeper.QuerySmart(ctx, reflectAddr, reflectStateBin)
	require.NoError(t, err)

	// first we pull out the data from chain response, before parsing the original response
	var reflectRawRes ChainResponse
	mustParse(t, res, &reflectRawRes)
	// and make sure there is no data
	require.Empty(t, reflectRawRes.Data)
	// we get an empty byte slice not nil (if anyone care in go-land)
	require.Equal(t, []byte{}, reflectRawRes.Data)
}

func checkAccount(t *testing.T, ctx sdk.Context, accKeeper authkeeper.AccountKeeper, bankKeeper bankkeeper.Keeper, addr sdk.AccAddress, expected sdk.Coins) {
	acct := accKeeper.GetAccount(ctx, addr)
	if expected == nil {
		assert.Nil(t, acct)
	} else {
		assert.NotNil(t, acct)
		if expected.Empty() {
			// there is confusion between nil and empty slice... let's just treat them the same
			assert.True(t, bankKeeper.GetAllBalances(ctx, acct.GetAddress()).Empty())
		} else {
			assert.Equal(t, bankKeeper.GetAllBalances(ctx, acct.GetAddress()), expected)
		}
	}
}

/**** Code to support custom messages *****/

type reflectCustomMsg struct {
	Debug string `json:"debug,omitempty"`
	Raw   []byte `json:"raw,omitempty"`
}

// toReflectRawMsg encodes an sdk msg using any type with json encoding.
// Then wraps it as an opaque message
func toReflectRawMsg(cdc codec.Codec, msg sdk.Msg) (wasmvmtypes.CosmosMsg, error) {
	any, err := codectypes.NewAnyWithValue(msg)
	if err != nil {
		return wasmvmtypes.CosmosMsg{}, err
	}
	rawBz, err := cdc.MarshalJSON(any)
	if err != nil {
		return wasmvmtypes.CosmosMsg{}, sdkerrors.Wrap(sdkerrors.ErrJSONMarshal, err.Error())
	}
	customMsg, err := json.Marshal(reflectCustomMsg{
		Raw: rawBz,
	})
	res := wasmvmtypes.CosmosMsg{
		Custom: customMsg,
	}
	return res, nil
}

// reflectEncoders needs to be registered in test setup to handle custom message callbacks
func reflectEncoders(cdc codec.Codec) *MessageEncoders {
	return &MessageEncoders{
		Custom: fromReflectRawMsg(cdc),
	}
}

// fromReflectRawMsg decodes msg.Data to an sdk.Msg using proto Any and json encoding.
// this needs to be registered on the Encoders
func fromReflectRawMsg(cdc codec.Codec) CustomEncoder {
	return func(_sender sdk.AccAddress, msg json.RawMessage, info wasmvmtypes.MessageInfo) ([]sdk.Msg, error) {
		var custom reflectCustomMsg
		err := json.Unmarshal(msg, &custom)
		if err != nil {
			return nil, sdkerrors.Wrap(sdkerrors.ErrJSONUnmarshal, err.Error())
		}
		if custom.Raw != nil {
			var any codectypes.Any
			if err := cdc.UnmarshalJSON(custom.Raw, &any); err != nil {
				return nil, sdkerrors.Wrap(sdkerrors.ErrJSONUnmarshal, err.Error())
			}
			var msg sdk.Msg
			if err := cdc.UnpackAny(&any, &msg); err != nil {
				return nil, err
			}
			return []sdk.Msg{msg}, nil
		}
		if custom.Debug != "" {
			return nil, sdkerrors.Wrapf(types.ErrInvalidMsg, "Custom Debug: %s", custom.Debug)
		}
		return nil, sdkerrors.Wrap(types.ErrInvalidMsg, "Unknown Custom message variant")
	}
}

type reflectCustomQuery struct {
	Ping        *struct{} `json:"ping,omitempty"`
	Capitalized *Text     `json:"capitalized,omitempty"`
}

// this is from the go code back to the contract (capitalized or ping)
type customQueryResponse struct {
	Msg string `json:"msg"`
}

// these are the return values from contract -> go depending on type of query
type ownerResponse struct {
	Owner string `json:"owner"`
}

type capitalizedResponse struct {
	Text string `json:"text"`
}

type chainResponse struct {
	Data []byte `json:"data"`
}

// reflectPlugins needs to be registered in test setup to handle custom query callbacks
func reflectPlugins() *QueryPlugins {
	return &QueryPlugins{
		Custom: performCustomQuery,
	}
}

func performCustomQuery(_ sdk.Context, request json.RawMessage) ([]byte, error) {
	var custom reflectCustomQuery
	err := json.Unmarshal(request, &custom)
	if err != nil {
		return nil, sdkerrors.Wrap(sdkerrors.ErrJSONUnmarshal, err.Error())
	}
	if custom.Capitalized != nil {
		msg := strings.ToUpper(custom.Capitalized.Text)
		return json.Marshal(customQueryResponse{Msg: msg})
	}
	if custom.Ping != nil {
		return json.Marshal(customQueryResponse{Msg: "pong"})
	}
	return nil, sdkerrors.Wrap(types.ErrInvalidMsg, "Unknown Custom query variant")
}
