package gov_test

import (
	"embed"
	"encoding/hex"
	"math/big"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	tmtypes "github.com/tendermint/tendermint/proto/tendermint/types"

	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/gov"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
)

//go:embed abi.json
var f embed.FS

func TestGovPrecompile(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	content := govtypes.ContentFromProposalType("title", "description", govtypes.ProposalTypeText, false)
	proposal, err := testApp.GovKeeper.SubmitProposal(ctx, content)
	require.Nil(t, err)
	testApp.GovKeeper.ActivateVotingPeriod(ctx, proposal)

	proposal2, err := testApp.GovKeeper.SubmitProposal(ctx, content)
	require.Nil(t, err)
	testApp.GovKeeper.ActivateVotingPeriod(ctx, proposal2)

	k := &testApp.EvmKeeper
	abi := pcommon.MustGetABI(f, "abi.json")

	type args struct {
		method   string
		proposal uint64
		option   govtypes.VoteOption
		value    *big.Int
	}

	tests := []struct {
		name                string
		args                args
		setup               func(ctx sdk.Context, k *keeper.Keeper, evmAddr common.Address, seiAddr sdk.AccAddress)
		verify              func(t *testing.T, ctx sdk.Context, seiAddr sdk.AccAddress, proposalID uint64)
		wantErr             bool
		wantErrMsgToContain string
		avoidAssociate      bool
	}{
		{
			name: "successful deposit",
			args: args{
				method:   "deposit",
				proposal: proposal.ProposalId,
				value:    new(big.Int).Mul(big.NewInt(10000000), big.NewInt(1_000_000_000_000)),
			},
			setup: func(ctx sdk.Context, k *keeper.Keeper, evmAddr common.Address, seiAddr sdk.AccAddress) {
				amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(20000000000000000))) // Adjusted for large deposit
				require.Nil(t, k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, amt))
				require.Nil(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, seiAddr, amt))
			},
			verify: func(t *testing.T, ctx sdk.Context, seiAddr sdk.AccAddress, proposalID uint64) {
				proposal, _ := testApp.GovKeeper.GetProposal(ctx, proposalID)
				require.Equal(t, govtypes.StatusVotingPeriod, proposal.Status)
			},
			wantErr: false,
		},
		{
			name: "successful vote yes",
			args: args{
				method:   "vote",
				proposal: proposal.ProposalId,
				option:   govtypes.OptionYes,
				value:    big.NewInt(0),
			},
			setup: func(ctx sdk.Context, k *keeper.Keeper, evmAddr common.Address, seiAddr sdk.AccAddress) {
				amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(200000000)))
				require.Nil(t, k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, amt))
				require.Nil(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, seiAddr, amt))
			},
			verify: func(t *testing.T, ctx sdk.Context, seiAddr sdk.AccAddress, proposalID uint64) {
				v, found := testApp.GovKeeper.GetVote(ctx, proposalID, seiAddr)
				require.True(t, found)
				require.Equal(t, 1, len(v.Options))
				require.Equal(t, govtypes.OptionYes, v.Options[0].Option)
				require.Equal(t, sdk.OneDec(), v.Options[0].Weight)
			},
			wantErr: false,
		},
		{
			name: "successful weighted vote",
			args: args{
				method:   "voteWeighted",
				proposal: proposal.ProposalId,
				value:    big.NewInt(0),
			},
			setup: func(ctx sdk.Context, k *keeper.Keeper, evmAddr common.Address, seiAddr sdk.AccAddress) {
				amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(200000000)))
				require.Nil(t, k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, amt))
				require.Nil(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, seiAddr, amt))
			},
			verify: func(t *testing.T, ctx sdk.Context, seiAddr sdk.AccAddress, proposalID uint64) {
				v, found := testApp.GovKeeper.GetVote(ctx, proposalID, seiAddr)
				require.True(t, found)
				require.Equal(t, 2, len(v.Options))
				// Should have Yes with 0.7 weight and Abstain with 0.3 weight
				require.Equal(t, govtypes.OptionYes, v.Options[0].Option)
				require.Equal(t, sdk.MustNewDecFromStr("0.7"), v.Options[0].Weight)
				require.Equal(t, govtypes.OptionAbstain, v.Options[1].Option)
				require.Equal(t, sdk.MustNewDecFromStr("0.3"), v.Options[1].Weight)
			},
			wantErr: false,
		},
		{
			name: "too many weighted vote options",
			args: args{
				method:   "voteWeighted",
				proposal: proposal.ProposalId,
				value:    big.NewInt(0),
			},
			setup: func(ctx sdk.Context, k *keeper.Keeper, evmAddr common.Address, seiAddr sdk.AccAddress) {
				amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(200000000)))
				require.Nil(t, k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, amt))
				require.Nil(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, seiAddr, amt))
			},
			verify: func(t *testing.T, ctx sdk.Context, seiAddr sdk.AccAddress, proposalID uint64) {
				// No verification needed as we expect an error
			},
			wantErr:             true,
			wantErrMsgToContain: "too many vote options provided: maximum allowed is 4",
		},
		{
			name: "exactly max weighted vote options",
			args: args{
				method:   "voteWeighted",
				proposal: proposal.ProposalId,
				value:    big.NewInt(0),
			},
			setup: func(ctx sdk.Context, k *keeper.Keeper, evmAddr common.Address, seiAddr sdk.AccAddress) {
				amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(200000000)))
				require.Nil(t, k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, amt))
				require.Nil(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, seiAddr, amt))
			},
			verify: func(t *testing.T, ctx sdk.Context, seiAddr sdk.AccAddress, proposalID uint64) {
				v, found := testApp.GovKeeper.GetVote(ctx, proposalID, seiAddr)
				require.True(t, found)
				require.Equal(t, 4, len(v.Options))
				// Verify all options were correctly processed
				require.Equal(t, govtypes.OptionYes, v.Options[0].Option)
				require.Equal(t, govtypes.OptionNo, v.Options[1].Option)
				require.Equal(t, govtypes.OptionAbstain, v.Options[2].Option)
				require.Equal(t, govtypes.OptionNoWithVeto, v.Options[3].Option)
			},
			wantErr: false,
		},
		{
			name: "invalid weighted vote options",
			args: args{
				method:   "voteWeighted",
				proposal: proposal.ProposalId,
				value:    big.NewInt(0),
			},
			setup: func(ctx sdk.Context, k *keeper.Keeper, evmAddr common.Address, seiAddr sdk.AccAddress) {
				amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(200000000)))
				require.Nil(t, k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, amt))
				require.Nil(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, seiAddr, amt))
			},
			verify: func(t *testing.T, ctx sdk.Context, seiAddr sdk.AccAddress, proposalID uint64) {
				// No verification needed as we expect an error
			},
			wantErr:             true,
			wantErrMsgToContain: "Duplicated vote option: invalid vote option",
		},
		{
			name: "association missing for vote",
			args: args{
				method:   "vote",
				proposal: proposal.ProposalId,
				option:   govtypes.OptionNo,
				value:    big.NewInt(0),
			},
			setup: func(ctx sdk.Context, k *keeper.Keeper, evmAddr common.Address, seiAddr sdk.AccAddress) {},
			verify: func(t *testing.T, ctx sdk.Context, seiAddr sdk.AccAddress, proposalID uint64) {
				_, found := testApp.GovKeeper.GetVote(ctx, proposalID, seiAddr)
				require.False(t, found)
			},
			wantErr:             true,
			avoidAssociate:      true,
			wantErrMsgToContain: "is not linked",
		},
		{
			name: "association missing for deposit",
			args: args{
				method:   "deposit",
				proposal: proposal2.ProposalId,
				value:    new(big.Int).Mul(big.NewInt(10000000), big.NewInt(1_000_000_000_000)),
			},
			setup: func(ctx sdk.Context, k *keeper.Keeper, evmAddr common.Address, seiAddr sdk.AccAddress) {
				amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(20000000000000000))) // Adjusted for large deposit
				require.Nil(t, k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, amt))
				require.Nil(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, seiAddr, amt))

				// send to casted address so it has enough funds to avoid insufficient funds
				require.Nil(t, k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, amt))
				require.Nil(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, evmAddr.Bytes(), amt))
			},
			verify: func(t *testing.T, ctx sdk.Context, seiAddr sdk.AccAddress, proposalID uint64) {
				proposal, _ := testApp.GovKeeper.GetProposal(ctx, proposalID)
				require.Len(t, proposal.TotalDeposit, 0)
			},
			wantErr:             true,
			avoidAssociate:      true,
			wantErrMsgToContain: "is not linked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var args []byte
			var err error
			if tt.args.method == "deposit" {
				args, err = abi.Pack(tt.args.method, tt.args.proposal)
			} else if tt.args.method == "voteWeighted" {
				// Create weighted vote options for testing
				// Example: 70% Yes, 30% Abstain
				weightedOptions := []struct {
					Option int32  `json:"option"`
					Weight string `json:"weight"`
				}{
					{Option: int32(govtypes.OptionYes), Weight: "0.7"},
					{Option: int32(govtypes.OptionAbstain), Weight: "0.3"},
				}
				if tt.name == "too many weighted vote options" {
					weightedOptions = []struct {
						Option int32  `json:"option"`
						Weight string `json:"weight"`
					}{
						{Option: int32(govtypes.OptionYes), Weight: "0.7"},
						{Option: int32(govtypes.OptionAbstain), Weight: "0.2"},
						{Option: int32(govtypes.OptionNo), Weight: "0.1"},
						{Option: int32(govtypes.OptionNoWithVeto), Weight: "0.1"},
						{Option: int32(govtypes.OptionNoWithVeto), Weight: "0.1"},
					}
				} else if tt.name == "invalid weighted vote options" {
					weightedOptions = []struct {
						Option int32  `json:"option"`
						Weight string `json:"weight"`
					}{
						{Option: int32(govtypes.OptionYes), Weight: "0.7"},
						{Option: int32(govtypes.OptionYes), Weight: "0.3"},
					}
				} else if tt.name == "exactly max weighted vote options" {
					weightedOptions = []struct {
						Option int32  `json:"option"`
						Weight string `json:"weight"`
					}{
						{Option: int32(govtypes.OptionYes), Weight: "0.25"},
						{Option: int32(govtypes.OptionNo), Weight: "0.25"},
						{Option: int32(govtypes.OptionAbstain), Weight: "0.25"},
						{Option: int32(govtypes.OptionNoWithVeto), Weight: "0.25"},
					}
				}
				args, err = abi.Pack(tt.args.method, tt.args.proposal, weightedOptions)
			} else {
				args, err = abi.Pack(tt.args.method, tt.args.proposal, tt.args.option)
			}
			require.Nil(t, err)

			privKey := testkeeper.MockPrivateKey()
			testPrivHex := hex.EncodeToString(privKey.Bytes())
			key, _ := crypto.HexToECDSA(testPrivHex)
			addr := common.HexToAddress(gov.GovAddress)
			txData := ethtypes.LegacyTx{
				GasPrice: big.NewInt(1000000000000),
				Gas:      20000000,
				To:       &addr,
				Value:    tt.args.value,
				Data:     args,
				Nonce:    0,
			}
			chainID := k.ChainID(ctx)
			chainCfg := evmtypes.DefaultChainConfig()
			ethCfg := chainCfg.EthereumConfig(chainID)
			blockNum := big.NewInt(ctx.BlockHeight())
			signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
			tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
			require.Nil(t, err)
			txwrapper, err := ethtx.NewLegacyTx(tx)
			require.Nil(t, err)
			req, err := evmtypes.NewMsgEVMTransaction(txwrapper)
			require.Nil(t, err)

			seiAddr, evmAddr := testkeeper.PrivateKeyToAddresses(privKey)
			if !tt.avoidAssociate {
				k.SetAddressMapping(ctx, seiAddr, evmAddr)
			}

			tt.setup(ctx, k, evmAddr, seiAddr)

			msgServer := keeper.NewMsgServerImpl(k)
			ante.Preprocess(ctx, req)
			res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
			if tt.wantErr {
				require.NotEmpty(t, res.VmError)
				require.Contains(t, string(res.ReturnData), tt.wantErrMsgToContain)
			} else {
				require.Nil(t, err)
				require.Empty(t, res.VmError)
				tt.verify(t, ctx, seiAddr, tt.args.proposal)
			}
		})
	}
}

func TestPrecompileExecutor_submitProposal(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(3)
	privKey := testkeeper.MockPrivateKey()
	callerSeiAddress, callerEvmAddress := testkeeper.PrivateKeyToAddresses(privKey)
	recipientSeiAddress, recipientEvmAddress := testkeeper.MockAddressPair()

	// Dynamically determine the expected proposal ID
	proposals := testApp.GovKeeper.GetProposals(ctx)
	expectedProposalID := byte(len(proposals) + 1)

	type args struct {
		caller           common.Address
		callerSeiAddress sdk.AccAddress
		proposal         string
		value            *big.Int
	}
	tests := []struct {
		name       string
		args       args
		wantErr    bool
		wantErrMsg string
		wantRet    []byte
	}{
		{
			name: "returns proposal id on submit text proposal with valid content",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
                      "title":"Test Proposal",
                      "description":"My awesome proposal",
                      "is_expedited":false,
                      "type":"Text"
                 }`,
				value: big.NewInt(1_000_000_000_000_000_000),
			},
			wantErr: false,
			wantRet: []byte{31: expectedProposalID},
		},
		{
			name: "returns proposal id on submit text proposal with valid content and no deposit",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Gov Proposal",
					"description": "This is a gov proposal",
					"type": "Text"
				}`,
				value: big.NewInt(1_000_000_000_000_000_000),
			},
			wantErr: false,
			wantRet: []byte{31: expectedProposalID},
		},
		{
			name: "returns proposal id on submit parameter change proposal with multiple changes",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Gov Param Change",
					"description": "Update quorum to 0.45",
					"changes": [
						{
							"subspace": "gov",
							"key": "tallyparams",
							"value": {
								"quorum": "0.45"
							}
						}
					],
					"deposit": "10000000usei",
					"is_expedited": false
				}`,
				value: big.NewInt(1_000_000_000_000_000_000),
			},
			wantErr: false,
			wantRet: []byte{31: expectedProposalID},
		},
		{
			name: "returns proposal id on submit cancel software upgrade proposal",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Cancel Upgrade",
					"description": "Cancel the pending software upgrade",
					"type": "CancelSoftwareUpgrade",
					"deposit": "10000000usei"
				}`,
				value: big.NewInt(1_000_000_000_000_000_000),
			},
			wantErr: false,
			wantRet: []byte{31: expectedProposalID},
		},
		{
			name: "returns error on parameter change proposal with no changes",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Invalid proposal",
					"description": "This proposal has no changes",
					"type": "ParameterChange",
					"deposit": "10000000usei"
				}`,
			},
			wantErr:    true,
			wantErrMsg: "at least one parameter change must be specified",
		},
		{
			name: "returns error on parameter change proposal with invalid value type",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Invalid proposal",
					"description": "This proposal has invalid value",
					"type": "ParameterChange",
					"changes": [
						{
							"subspace": "ct",
							"key": "EnableCtModule",
							"value": {
								"complex": "object"
							}
						}
					],
					"deposit": "10000000usei"
				}`,
			},
			wantErr:    true,
			wantErrMsg: "parameter ct/EnableCtModule does not exist: invalid proposal content",
		},
		{
			name: "returns proposal id on submit software upgrade proposal with valid content",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Software Upgrade",
					"description": "Upgrade to v2.0.0",
					"type": "SoftwareUpgrade",
					"plan": {
						"name": "v2.0.0",
						"height": 1000,
						"info": "Upgrade to v2.0.0"
					},
					"deposit": "10000000usei"
				}`,
			},
			wantErr: false,
			wantRet: []byte{31: expectedProposalID},
		},
		{
			name: "returns error on software upgrade proposal with no plan",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Invalid upgrade",
					"description": "This proposal has no plan",
					"type": "SoftwareUpgrade",
					"deposit": "10000000usei"
				}`,
			},
			wantErr:    true,
			wantErrMsg: "upgrade plan must be specified",
		},
		{
			name: "returns error on software upgrade proposal with missing height",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Invalid upgrade",
					"description": "Missing height",
					"type": "SoftwareUpgrade",
					"plan": {
						"name": "v2.0.0"
					},
					"deposit": "10000000usei"
				}`,
			},
			wantErr:    true,
			wantErrMsg: "upgrade height must be specified",
		},
		{
			name: "returns error on software upgrade proposal with missing name",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Invalid upgrade",
					"description": "Missing name",
					"type": "SoftwareUpgrade",
					"plan": {
						"height": 1000
					},
					"deposit": "10000000usei"
				}`,
			},
			wantErr:    true,
			wantErrMsg: "upgrade name must be specified",
		},
		{
			name: "returns error on software upgrade proposal with invalid height type",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Invalid upgrade",
					"description": "Invalid height type",
					"type": "SoftwareUpgrade",
					"plan": {
						"name": "v2.0.0",
						"height": "1000"
					},
					"deposit": "10000000usei"
				}`,
			},
			wantErr:    true,
			wantErrMsg: "failed to parse proposal JSON: json: cannot unmarshal string into Go struct field SoftwareUpgradePlan.plan.height of type int64",
		},
		{
			name: "returns error on software upgrade proposal with invalid name type",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Invalid upgrade",
					"description": "Invalid name type",
					"type": "SoftwareUpgrade",
					"plan": {
						"name": 123,
						"height": 1000
					},
					"deposit": "10000000usei"
				}`,
			},
			wantErr:    true,
			wantErrMsg: "failed to parse proposal JSON: json: cannot unmarshal number into Go struct field SoftwareUpgradePlan.plan.name of type string",
		},
		{
			name: "returns error on software upgrade proposal with invalid info type",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Invalid upgrade",
					"description": "Invalid info type",
					"type": "SoftwareUpgrade",
					"plan": {
						"name": "v2.0.0",
						"height": 1000,
						"info": 123
					},
					"deposit": "10000000usei"
				}`,
			},
			wantErr:    true,
			wantErrMsg: "failed to parse proposal JSON: json: cannot unmarshal number into Go struct field SoftwareUpgradePlan.plan.info of type string",
		},
		{
			name: "returns proposal id on submit community pool spend proposal with valid content",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Community Pool Spend",
					"description": "Spend from community pool",
					"type": "CommunityPoolSpend",
					"community_pool_spend": {
						"recipient": "` + recipientEvmAddress.String() + `",
						"amount": "1000000usei"
					},
					"deposit": "10000000usei"
				}`,
				value: big.NewInt(1_000_000_000_000_000_000),
			},
			wantErr: false,
			wantRet: []byte{31: expectedProposalID},
		},
		{
			name: "returns error on community pool spend proposal with no parameters",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Invalid spend",
					"description": "This proposal has no parameters",
					"type": "CommunityPoolSpend",
					"deposit": "10000000usei"
				}`,
			},
			wantErr:    true,
			wantErrMsg: "community pool spend parameters must be specified",
		},
		{
			name: "returns error on community pool spend proposal with missing recipient",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Invalid spend",
					"description": "Missing recipient",
					"type": "CommunityPoolSpend",
					"community_pool_spend": {
						"amount": "1000000usei"
					},
					"deposit": "10000000usei"
				}`,
			},
			wantErr:    true,
			wantErrMsg: "invalid ethereum address format",
		},
		{
			name: "returns error on community pool spend proposal with missing amount",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Invalid spend",
					"description": "Missing amount",
					"type": "CommunityPoolSpend",
					"community_pool_spend": {
						"recipient": "0x1234567890123456789012345678901234567890"
					},
					"deposit": "10000000usei"
				}`,
			},
			wantErr:    true,
			wantErrMsg: "amount must be greater than zero",
		},
		{
			name: "returns error on community pool spend proposal with invalid recipient",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Invalid spend",
					"description": "Invalid recipient",
					"type": "CommunityPoolSpend",
					"community_pool_spend": {
						"recipient": "invalid",
						"amount": "1000000usei"
					},
					"deposit": "10000000usei"
				}`,
			},
			wantErr:    true,
			wantErrMsg: "invalid ethereum address format",
		},
		{
			name: "returns error on community pool spend proposal with invalid amount format",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Community Pool Spend",
					"description": "Test invalid amount",
					"type": "CommunityPoolSpend",
					"community_pool_spend": {
						"recipient": "0x1234567890123456789012345678901234567890",
						"amount": "invalid"
					},
					"deposit": "10000000usei"
				}`,
			},
			wantErr:    true,
			wantErrMsg: "invalid amount format: invalid decimal coin expression: invalid",
		},
		{
			name: "returns proposal id on submit update resource dependency proposal with valid content",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Update Resource Dependencies",
					"description": "Add dependency mappings for bank module",
					"type": "UpdateResourceDependencyMapping",
					"resource_mapping": {
						"resource": "/cosmos.bank.v1beta1.MsgSend",
						"dependencies": [
							"/cosmos.auth.v1beta1.QueryAccount",
							"/cosmos.bank.v1beta1.QueryBalance"
						],
						"access_ops": [
							{
								"resource_type": "BANK",
								"access_type": "READ",
								"identifier_template": "*"
							},
							{
								"resource_type": "ANY",
								"access_type": "COMMIT",
								"identifier_template": "*"
							}
						],
						"dynamic_enabled": true
					}
				}`,
			},
			wantErr: false,
			wantRet: []byte{31: expectedProposalID},
		},
		{
			name: "returns error on update resource dependency proposal with no resource mapping",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Update Resource Dependencies",
					"description": "Missing resource mapping",
					"type": "UpdateResourceDependencyMapping"
				}`,
			},
			wantErr:    true,
			wantErrMsg: "resource mapping must be specified",
		},
		{
			name: "returns error on update resource dependency proposal with missing resource",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Update Resource Dependencies",
					"description": "Missing resource field",
					"type": "UpdateResourceDependencyMapping",
					"resource_mapping": {
						"dependencies": ["/cosmos.auth.v1beta1.QueryAccount"],
						"access_ops": [
							{
								"resource_type": "ANY",
								"access_type": "COMMIT",
								"identifier_template": "*"
							}
						]
					}
				}`,
			},
			wantErr:    true,
			wantErrMsg: "resource must be specified",
		},
		{
			name: "returns error on update resource dependency proposal with missing dependencies",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Update Resource Dependencies",
					"description": "Missing dependencies",
					"type": "UpdateResourceDependencyMapping",
					"resource_mapping": {
						"resource": "/cosmos.bank.v1beta1.MsgSend",
						"access_ops": [
							{
								"resource_type": "ANY",
								"access_type": "COMMIT",
								"identifier_template": "*"
							}
						]
					}
				}`,
			},
			wantErr:    true,
			wantErrMsg: "at least one dependency must be specified",
		},
		{
			name: "returns error on update resource dependency proposal with missing access ops",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Update Resource Dependencies",
					"description": "Missing access operations",
					"type": "UpdateResourceDependencyMapping",
					"resource_mapping": {
						"resource": "/cosmos.bank.v1beta1.MsgSend",
						"dependencies": ["/cosmos.auth.v1beta1.QueryAccount"]
					}
				}`,
			},
			wantErr:    true,
			wantErrMsg: "at least one access operation must be specified",
		},
		{
			name: "returns error on update resource dependency proposal with invalid last access op",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Update Resource Dependencies",
					"description": "Last access op is not COMMIT",
					"type": "UpdateResourceDependencyMapping",
					"resource_mapping": {
						"resource": "/cosmos.bank.v1beta1.MsgSend",
						"dependencies": ["/cosmos.auth.v1beta1.QueryAccount"],
						"access_ops": [
							{
								"resource_type": "BANK",
								"access_type": "READ",
								"identifier_template": "*"
							}
						]
					}
				}`,
			},
			wantErr:    true,
			wantErrMsg: "last access operation must be COMMIT",
		},
		{
			name: "returns proposal id on submit resource dependency mapping proposal with valid content",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Resource Dependency Mapping",
					"description": "Update resource dependencies",
					"type": "UpdateResourceDependencyMapping",
					"changes": [
						{
							"key": "resource",
							"value": "resource1"
						},
						{
							"key": "dependencies",
							"value": ["dep1", "dep2"]
						}
					],
					"deposit": "10000000usei"
				}`,
			},
			wantErr:    true,
			wantErrMsg: "resource mapping must be specified",
		},
		{
			name: "returns error on resource dependency mapping proposal with no changes",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Invalid mapping",
					"description": "This proposal has no changes",
					"type": "UpdateResourceDependencyMapping",
					"deposit": "10000000usei"
				}`,
			},
			wantErr:    true,
			wantErrMsg: "resource mapping must be specified",
		},
		{
			name: "returns error on resource dependency mapping proposal with missing resource",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Invalid mapping",
					"description": "Missing resource",
					"type": "UpdateResourceDependencyMapping",
					"changes": [
						{
							"key": "dependencies",
							"value": ["dep1", "dep2"]
						}
					],
					"deposit": "10000000usei"
				}`,
			},
			wantErr:    true,
			wantErrMsg: "resource mapping must be specified",
		},
		{
			name: "returns error on resource dependency mapping proposal with missing dependencies",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Invalid mapping",
					"description": "Missing dependencies",
					"type": "UpdateResourceDependencyMapping",
					"changes": [
						{
							"key": "resource",
							"value": "resource1"
						}
					],
					"deposit": "10000000usei"
				}`,
			},
			wantErr:    true,
			wantErrMsg: "resource mapping must be specified",
		},
		{
			name: "returns error on resource dependency mapping proposal with invalid resource type",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Invalid mapping",
					"description": "Invalid resource type",
					"type": "UpdateResourceDependencyMapping",
					"changes": [
						{
							"key": "resource",
							"value": 123
						},
						{
							"key": "dependencies",
							"value": ["dep1", "dep2"]
						}
					],
					"deposit": "10000000usei"
				}`,
			},
			wantErr:    true,
			wantErrMsg: "resource mapping must be specified",
		},
		{
			name: "returns error on resource dependency mapping proposal with invalid dependencies type",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Invalid mapping",
					"description": "Invalid dependencies type",
					"type": "UpdateResourceDependencyMapping",
					"changes": [
						{
							"key": "resource",
							"value": "resource1"
						},
						{
							"key": "dependencies",
							"value": "not an array"
						}
					],
					"deposit": "10000000usei"
				}`,
			},
			wantErr:    true,
			wantErrMsg: "resource mapping must be specified",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := &testApp.EvmKeeper

			testPrivHex := hex.EncodeToString(privKey.Bytes())
			key, _ := crypto.HexToECDSA(testPrivHex)

			k.SetAddressMapping(ctx, tt.args.callerSeiAddress, tt.args.caller)
			k.SetAddressMapping(ctx, recipientSeiAddress, recipientEvmAddress)

			mintAmt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(20_000_000)))
			sendAmt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(10_000_000)))
			require.Nil(t, k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, mintAmt))
			require.Nil(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, tt.args.callerSeiAddress, sendAmt))

			addr := common.HexToAddress(gov.GovAddress)

			abi := pcommon.MustGetABI(f, "abi.json")
			inputs, err := abi.Pack(gov.SubmitProposalMethod, tt.args.proposal)
			require.Nil(t, err)

			txData := ethtypes.LegacyTx{
				GasPrice: big.NewInt(1000000000000),
				Gas:      20000000,
				To:       &addr,
				Value:    tt.args.value,
				Data:     inputs,
				Nonce:    0,
			}
			chainID := k.ChainID(ctx)
			chainCfg := evmtypes.DefaultChainConfig()
			ethCfg := chainCfg.EthereumConfig(chainID)
			blockNum := big.NewInt(ctx.BlockHeight())
			signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
			tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
			require.Nil(t, err)
			txwrapper, err := ethtx.NewLegacyTx(tx)
			require.Nil(t, err)
			req, err := evmtypes.NewMsgEVMTransaction(txwrapper)
			require.Nil(t, err)

			msgServer := keeper.NewMsgServerImpl(k)
			ante.Preprocess(ctx, req)
			gotRet, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)

			if tt.wantErr {
				require.NotEmpty(t, gotRet.VmError)
				require.Equal(t, string(gotRet.ReturnData), tt.wantErrMsg)
			} else {
				require.Empty(t, gotRet.VmError)
				require.Nil(t, err)
				require.Equal(t, tt.wantRet, gotRet.ReturnData)
			}
		})
	}
}
