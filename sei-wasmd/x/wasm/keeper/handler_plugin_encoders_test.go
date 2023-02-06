package keeper

import (
	"testing"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v3/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v3/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v3/modules/core/04-channel/types"
	"github.com/golang/protobuf/proto"
	"github.com/stretchr/testify/assert"

	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	"github.com/CosmWasm/wasmd/x/wasm/keeper/wasmtesting"
	"github.com/CosmWasm/wasmd/x/wasm/types"
)

func TestEncoding(t *testing.T) {
	var (
		addr1       = RandomAccountAddress(t)
		addr2       = RandomAccountAddress(t)
		addr3       = RandomAccountAddress(t)
		invalidAddr = "xrnd1d02kd90n38qvr3qb9qof83fn2d2"
	)
	valAddr := make(sdk.ValAddress, types.SDKAddrLen)
	valAddr[0] = 12
	valAddr2 := make(sdk.ValAddress, types.SDKAddrLen)
	valAddr2[1] = 123

	jsonMsg := types.RawContractMessage(`{"foo": 123}`)

	bankMsg := &banktypes.MsgSend{
		FromAddress: addr2.String(),
		ToAddress:   addr1.String(),
		Amount: sdk.Coins{
			sdk.NewInt64Coin("uatom", 12345),
			sdk.NewInt64Coin("utgd", 54321),
		},
	}
	bankMsgBin, err := proto.Marshal(bankMsg)
	require.NoError(t, err)

	content, err := codectypes.NewAnyWithValue(types.StoreCodeProposalFixture())
	require.NoError(t, err)

	proposalMsg := &govtypes.MsgSubmitProposal{
		Proposer:       addr1.String(),
		InitialDeposit: sdk.NewCoins(sdk.NewInt64Coin("uatom", 12345)),
		Content:        content,
	}
	proposalMsgBin, err := proto.Marshal(proposalMsg)
	require.NoError(t, err)

	cases := map[string]struct {
		sender             sdk.AccAddress
		srcMsg             wasmvmtypes.CosmosMsg
		srcContractIBCPort string
		transferPortSource types.ICS20TransferPortSource
		// set if valid
		output []sdk.Msg
		// set if invalid
		isError bool
	}{
		"simple send": {
			sender: addr1,
			srcMsg: wasmvmtypes.CosmosMsg{
				Bank: &wasmvmtypes.BankMsg{
					Send: &wasmvmtypes.SendMsg{
						ToAddress: addr2.String(),
						Amount: []wasmvmtypes.Coin{
							{
								Denom:  "uatom",
								Amount: "12345",
							},
							{
								Denom:  "usdt",
								Amount: "54321",
							},
						},
					},
				},
			},
			output: []sdk.Msg{
				&banktypes.MsgSend{
					FromAddress: addr1.String(),
					ToAddress:   addr2.String(),
					Amount: sdk.Coins{
						sdk.NewInt64Coin("uatom", 12345),
						sdk.NewInt64Coin("usdt", 54321),
					},
				},
			},
		},
		"invalid send amount": {
			sender: addr1,
			srcMsg: wasmvmtypes.CosmosMsg{
				Bank: &wasmvmtypes.BankMsg{
					Send: &wasmvmtypes.SendMsg{
						ToAddress: addr2.String(),
						Amount: []wasmvmtypes.Coin{
							{
								Denom:  "uatom",
								Amount: "123.456",
							},
						},
					},
				},
			},
			isError: true,
		},
		"invalid address": {
			sender: addr1,
			srcMsg: wasmvmtypes.CosmosMsg{
				Bank: &wasmvmtypes.BankMsg{
					Send: &wasmvmtypes.SendMsg{
						ToAddress: invalidAddr,
						Amount: []wasmvmtypes.Coin{
							{
								Denom:  "uatom",
								Amount: "7890",
							},
						},
					},
				},
			},
			isError: false, // addresses are checked in the handler
			output: []sdk.Msg{
				&banktypes.MsgSend{
					FromAddress: addr1.String(),
					ToAddress:   invalidAddr,
					Amount: sdk.Coins{
						sdk.NewInt64Coin("uatom", 7890),
					},
				},
			},
		},
		"wasm execute": {
			sender: addr1,
			srcMsg: wasmvmtypes.CosmosMsg{
				Wasm: &wasmvmtypes.WasmMsg{
					Execute: &wasmvmtypes.ExecuteMsg{
						ContractAddr: addr2.String(),
						Msg:          jsonMsg,
						Funds: []wasmvmtypes.Coin{
							wasmvmtypes.NewCoin(12, "eth"),
						},
					},
				},
			},
			output: []sdk.Msg{
				&types.MsgExecuteContract{
					Sender:   addr1.String(),
					Contract: addr2.String(),
					Msg:      jsonMsg,
					Funds:    sdk.NewCoins(sdk.NewInt64Coin("eth", 12)),
				},
			},
		},
		"wasm instantiate": {
			sender: addr1,
			srcMsg: wasmvmtypes.CosmosMsg{
				Wasm: &wasmvmtypes.WasmMsg{
					Instantiate: &wasmvmtypes.InstantiateMsg{
						CodeID: 7,
						Msg:    jsonMsg,
						Funds: []wasmvmtypes.Coin{
							wasmvmtypes.NewCoin(123, "eth"),
						},
						Label: "myLabel",
						Admin: addr2.String(),
					},
				},
			},
			output: []sdk.Msg{
				&types.MsgInstantiateContract{
					Sender: addr1.String(),
					CodeID: 7,
					Label:  "myLabel",
					Msg:    jsonMsg,
					Funds:  sdk.NewCoins(sdk.NewInt64Coin("eth", 123)),
					Admin:  addr2.String(),
				},
			},
		},
		"wasm migrate": {
			sender: addr2,
			srcMsg: wasmvmtypes.CosmosMsg{
				Wasm: &wasmvmtypes.WasmMsg{
					Migrate: &wasmvmtypes.MigrateMsg{
						ContractAddr: addr1.String(),
						NewCodeID:    12,
						Msg:          jsonMsg,
					},
				},
			},
			output: []sdk.Msg{
				&types.MsgMigrateContract{
					Sender:   addr2.String(),
					Contract: addr1.String(),
					CodeID:   12,
					Msg:      jsonMsg,
				},
			},
		},
		"wasm update admin": {
			sender: addr2,
			srcMsg: wasmvmtypes.CosmosMsg{
				Wasm: &wasmvmtypes.WasmMsg{
					UpdateAdmin: &wasmvmtypes.UpdateAdminMsg{
						ContractAddr: addr1.String(),
						Admin:        addr3.String(),
					},
				},
			},
			output: []sdk.Msg{
				&types.MsgUpdateAdmin{
					Sender:   addr2.String(),
					Contract: addr1.String(),
					NewAdmin: addr3.String(),
				},
			},
		},
		"wasm clear admin": {
			sender: addr2,
			srcMsg: wasmvmtypes.CosmosMsg{
				Wasm: &wasmvmtypes.WasmMsg{
					ClearAdmin: &wasmvmtypes.ClearAdminMsg{
						ContractAddr: addr1.String(),
					},
				},
			},
			output: []sdk.Msg{
				&types.MsgClearAdmin{
					Sender:   addr2.String(),
					Contract: addr1.String(),
				},
			},
		},
		"staking delegate": {
			sender: addr1,
			srcMsg: wasmvmtypes.CosmosMsg{
				Staking: &wasmvmtypes.StakingMsg{
					Delegate: &wasmvmtypes.DelegateMsg{
						Validator: valAddr.String(),
						Amount:    wasmvmtypes.NewCoin(777, "stake"),
					},
				},
			},
			output: []sdk.Msg{
				&stakingtypes.MsgDelegate{
					DelegatorAddress: addr1.String(),
					ValidatorAddress: valAddr.String(),
					Amount:           sdk.NewInt64Coin("stake", 777),
				},
			},
		},
		"staking delegate to non-validator": {
			sender: addr1,
			srcMsg: wasmvmtypes.CosmosMsg{
				Staking: &wasmvmtypes.StakingMsg{
					Delegate: &wasmvmtypes.DelegateMsg{
						Validator: addr2.String(),
						Amount:    wasmvmtypes.NewCoin(777, "stake"),
					},
				},
			},
			isError: false, // fails in the handler
			output: []sdk.Msg{
				&stakingtypes.MsgDelegate{
					DelegatorAddress: addr1.String(),
					ValidatorAddress: addr2.String(),
					Amount:           sdk.NewInt64Coin("stake", 777),
				},
			},
		},
		"staking undelegate": {
			sender: addr1,
			srcMsg: wasmvmtypes.CosmosMsg{
				Staking: &wasmvmtypes.StakingMsg{
					Undelegate: &wasmvmtypes.UndelegateMsg{
						Validator: valAddr.String(),
						Amount:    wasmvmtypes.NewCoin(555, "stake"),
					},
				},
			},
			output: []sdk.Msg{
				&stakingtypes.MsgUndelegate{
					DelegatorAddress: addr1.String(),
					ValidatorAddress: valAddr.String(),
					Amount:           sdk.NewInt64Coin("stake", 555),
				},
			},
		},
		"staking redelegate": {
			sender: addr1,
			srcMsg: wasmvmtypes.CosmosMsg{
				Staking: &wasmvmtypes.StakingMsg{
					Redelegate: &wasmvmtypes.RedelegateMsg{
						SrcValidator: valAddr.String(),
						DstValidator: valAddr2.String(),
						Amount:       wasmvmtypes.NewCoin(222, "stake"),
					},
				},
			},
			output: []sdk.Msg{
				&stakingtypes.MsgBeginRedelegate{
					DelegatorAddress:    addr1.String(),
					ValidatorSrcAddress: valAddr.String(),
					ValidatorDstAddress: valAddr2.String(),
					Amount:              sdk.NewInt64Coin("stake", 222),
				},
			},
		},
		"staking withdraw (explicit recipient)": {
			sender: addr1,
			srcMsg: wasmvmtypes.CosmosMsg{
				Distribution: &wasmvmtypes.DistributionMsg{
					WithdrawDelegatorReward: &wasmvmtypes.WithdrawDelegatorRewardMsg{
						Validator: valAddr2.String(),
					},
				},
			},
			output: []sdk.Msg{
				&distributiontypes.MsgWithdrawDelegatorReward{
					DelegatorAddress: addr1.String(),
					ValidatorAddress: valAddr2.String(),
				},
			},
		},
		"staking set withdraw address": {
			sender: addr1,
			srcMsg: wasmvmtypes.CosmosMsg{
				Distribution: &wasmvmtypes.DistributionMsg{
					SetWithdrawAddress: &wasmvmtypes.SetWithdrawAddressMsg{
						Address: addr2.String(),
					},
				},
			},
			output: []sdk.Msg{
				&distributiontypes.MsgSetWithdrawAddress{
					DelegatorAddress: addr1.String(),
					WithdrawAddress:  addr2.String(),
				},
			},
		},
		"stargate encoded bank msg": {
			sender: addr2,
			srcMsg: wasmvmtypes.CosmosMsg{
				Stargate: &wasmvmtypes.StargateMsg{
					TypeURL: "/cosmos.bank.v1beta1.MsgSend",
					Value:   bankMsgBin,
				},
			},
			output: []sdk.Msg{bankMsg},
		},
		"stargate encoded msg with any type": {
			sender: addr2,
			srcMsg: wasmvmtypes.CosmosMsg{
				Stargate: &wasmvmtypes.StargateMsg{
					TypeURL: "/cosmos.gov.v1beta1.MsgSubmitProposal",
					Value:   proposalMsgBin,
				},
			},
			output: []sdk.Msg{proposalMsg},
		},
		"stargate encoded invalid typeUrl": {
			sender: addr2,
			srcMsg: wasmvmtypes.CosmosMsg{
				Stargate: &wasmvmtypes.StargateMsg{
					TypeURL: "/cosmos.bank.v2.MsgSend",
					Value:   bankMsgBin,
				},
			},
			isError: true,
		},
		"IBC transfer with block timeout": {
			sender:             addr1,
			srcContractIBCPort: "myIBCPort",
			srcMsg: wasmvmtypes.CosmosMsg{
				IBC: &wasmvmtypes.IBCMsg{
					Transfer: &wasmvmtypes.TransferMsg{
						ChannelID: "myChanID",
						ToAddress: addr2.String(),
						Amount: wasmvmtypes.Coin{
							Denom:  "ALX",
							Amount: "1",
						},
						Timeout: wasmvmtypes.IBCTimeout{
							Block: &wasmvmtypes.IBCTimeoutBlock{Revision: 1, Height: 2},
						},
					},
				},
			},
			transferPortSource: wasmtesting.MockIBCTransferKeeper{GetPortFn: func(ctx sdk.Context) string {
				return "myTransferPort"
			}},
			output: []sdk.Msg{
				&ibctransfertypes.MsgTransfer{
					SourcePort:    "myTransferPort",
					SourceChannel: "myChanID",
					Token: sdk.Coin{
						Denom:  "ALX",
						Amount: sdk.NewInt(1),
					},
					Sender:        addr1.String(),
					Receiver:      addr2.String(),
					TimeoutHeight: clienttypes.Height{RevisionNumber: 1, RevisionHeight: 2},
				},
			},
		},
		"IBC transfer with time timeout": {
			sender:             addr1,
			srcContractIBCPort: "myIBCPort",
			srcMsg: wasmvmtypes.CosmosMsg{
				IBC: &wasmvmtypes.IBCMsg{
					Transfer: &wasmvmtypes.TransferMsg{
						ChannelID: "myChanID",
						ToAddress: addr2.String(),
						Amount: wasmvmtypes.Coin{
							Denom:  "ALX",
							Amount: "1",
						},
						Timeout: wasmvmtypes.IBCTimeout{Timestamp: 100},
					},
				},
			},
			transferPortSource: wasmtesting.MockIBCTransferKeeper{GetPortFn: func(ctx sdk.Context) string {
				return "transfer"
			}},
			output: []sdk.Msg{
				&ibctransfertypes.MsgTransfer{
					SourcePort:    "transfer",
					SourceChannel: "myChanID",
					Token: sdk.Coin{
						Denom:  "ALX",
						Amount: sdk.NewInt(1),
					},
					Sender:           addr1.String(),
					Receiver:         addr2.String(),
					TimeoutTimestamp: 100,
				},
			},
		},
		"IBC transfer with time and height timeout": {
			sender:             addr1,
			srcContractIBCPort: "myIBCPort",
			srcMsg: wasmvmtypes.CosmosMsg{
				IBC: &wasmvmtypes.IBCMsg{
					Transfer: &wasmvmtypes.TransferMsg{
						ChannelID: "myChanID",
						ToAddress: addr2.String(),
						Amount: wasmvmtypes.Coin{
							Denom:  "ALX",
							Amount: "1",
						},
						Timeout: wasmvmtypes.IBCTimeout{Timestamp: 100, Block: &wasmvmtypes.IBCTimeoutBlock{Height: 1, Revision: 2}},
					},
				},
			},
			transferPortSource: wasmtesting.MockIBCTransferKeeper{GetPortFn: func(ctx sdk.Context) string {
				return "transfer"
			}},
			output: []sdk.Msg{
				&ibctransfertypes.MsgTransfer{
					SourcePort:    "transfer",
					SourceChannel: "myChanID",
					Token: sdk.Coin{
						Denom:  "ALX",
						Amount: sdk.NewInt(1),
					},
					Sender:           addr1.String(),
					Receiver:         addr2.String(),
					TimeoutTimestamp: 100,
					TimeoutHeight:    clienttypes.NewHeight(2, 1),
				},
			},
		},
		"IBC close channel": {
			sender:             addr1,
			srcContractIBCPort: "myIBCPort",
			srcMsg: wasmvmtypes.CosmosMsg{
				IBC: &wasmvmtypes.IBCMsg{
					CloseChannel: &wasmvmtypes.CloseChannelMsg{
						ChannelID: "channel-1",
					},
				},
			},
			output: []sdk.Msg{
				&channeltypes.MsgChannelCloseInit{
					PortId:    "wasm." + addr1.String(),
					ChannelId: "channel-1",
					Signer:    addr1.String(),
				},
			},
		},
		"Gov vote: yes": {
			sender:             addr1,
			srcContractIBCPort: "myIBCPort",
			srcMsg: wasmvmtypes.CosmosMsg{
				Gov: &wasmvmtypes.GovMsg{
					Vote: &wasmvmtypes.VoteMsg{ProposalId: 1, Vote: wasmvmtypes.Yes},
				},
			},
			output: []sdk.Msg{
				&govtypes.MsgVote{
					ProposalId: 1,
					Voter:      addr1.String(),
					Option:     govtypes.OptionYes,
				},
			},
		},
		"Gov vote: No": {
			sender:             addr1,
			srcContractIBCPort: "myIBCPort",
			srcMsg: wasmvmtypes.CosmosMsg{
				Gov: &wasmvmtypes.GovMsg{
					Vote: &wasmvmtypes.VoteMsg{ProposalId: 1, Vote: wasmvmtypes.No},
				},
			},
			output: []sdk.Msg{
				&govtypes.MsgVote{
					ProposalId: 1,
					Voter:      addr1.String(),
					Option:     govtypes.OptionNo,
				},
			},
		},
		"Gov vote: Abstain": {
			sender:             addr1,
			srcContractIBCPort: "myIBCPort",
			srcMsg: wasmvmtypes.CosmosMsg{
				Gov: &wasmvmtypes.GovMsg{
					Vote: &wasmvmtypes.VoteMsg{ProposalId: 10, Vote: wasmvmtypes.Abstain},
				},
			},
			output: []sdk.Msg{
				&govtypes.MsgVote{
					ProposalId: 10,
					Voter:      addr1.String(),
					Option:     govtypes.OptionAbstain,
				},
			},
		},
		"Gov vote: No with veto": {
			sender:             addr1,
			srcContractIBCPort: "myIBCPort",
			srcMsg: wasmvmtypes.CosmosMsg{
				Gov: &wasmvmtypes.GovMsg{
					Vote: &wasmvmtypes.VoteMsg{ProposalId: 1, Vote: wasmvmtypes.NoWithVeto},
				},
			},
			output: []sdk.Msg{
				&govtypes.MsgVote{
					ProposalId: 1,
					Voter:      addr1.String(),
					Option:     govtypes.OptionNoWithVeto,
				},
			},
		},
	}
	encodingConfig := MakeEncodingConfig(t)
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var ctx sdk.Context
			encoder := DefaultEncoders(encodingConfig.Marshaler, tc.transferPortSource)
			res, err := encoder.Encode(ctx, tc.sender, tc.srcContractIBCPort, tc.srcMsg)
			if tc.isError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.output, res)
			}
		})
	}
}

func TestConvertWasmCoinToSdkCoin(t *testing.T) {
	specs := map[string]struct {
		src    wasmvmtypes.Coin
		expErr bool
		expVal sdk.Coin
	}{
		"all good": {
			src: wasmvmtypes.Coin{
				Denom:  "foo",
				Amount: "1",
			},
			expVal: sdk.NewCoin("foo", sdk.NewIntFromUint64(1)),
		},
		"negative amount": {
			src: wasmvmtypes.Coin{
				Denom:  "foo",
				Amount: "-1",
			},
			expErr: true,
		},
		"denom to short": {
			src: wasmvmtypes.Coin{
				Denom:  "f",
				Amount: "1",
			},
			expErr: true,
		},
		"invalid demum char": {
			src: wasmvmtypes.Coin{
				Denom:  "&fff",
				Amount: "1",
			},
			expErr: true,
		},
		"not a number amount": {
			src: wasmvmtypes.Coin{
				Denom:  "foo",
				Amount: "bar",
			},
			expErr: true,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			gotVal, gotErr := ConvertWasmCoinToSdkCoin(spec.src)
			if spec.expErr {
				require.Error(t, gotErr)
				return
			}
			require.NoError(t, gotErr)
			assert.Equal(t, spec.expVal, gotVal)
		})
	}
}
