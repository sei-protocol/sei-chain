package gov_test

import (
	"embed"
	"encoding/hex"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"math/big"
	"reflect"
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
		name           string
		args           args
		setup          func(ctx sdk.Context, k *keeper.Keeper, evmAddr common.Address, seiAddr sdk.AccAddress)
		verify         func(t *testing.T, ctx sdk.Context, seiAddr sdk.AccAddress, proposalID uint64)
		wantErr        bool
		avoidAssociate bool
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
			wantErr:        true,
			avoidAssociate: true,
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
			wantErr:        true,
			avoidAssociate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var args []byte
			var err error
			if tt.args.method == "deposit" {
				args, err = abi.Pack(tt.args.method, tt.args.proposal)
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
			} else {
				require.Nil(t, err)
				require.Empty(t, res.VmError)
				tt.verify(t, ctx, seiAddr, tt.args.proposal)
			}
		})
	}
}

func TestPrecompileExecutor_submitProposal(t *testing.T) {
	callerSeiAddress, callerEvmAddress := testkeeper.MockAddressPair()
	type fields struct {
		//govKeeper    pcommon.GovKeeper
		//govMsgServer pcommon.GovMsgServer
		//evmKeeper    pcommon.EVMKeeper
		//bankKeeper   pcommon.BankKeeper
	}
	type args struct {
		caller           common.Address
		callerSeiAddress sdk.AccAddress
		proposal         string
	}
	tests := []struct {
		name string
		//fields     fields
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
				proposal:         "{\"title\":\"Test Proposal\",\"description\":\"My awesome proposal\",\"is_expedited\":false,\"type\":\"Text\",\"deposit\":\"10000000usei\"}",
			},
			wantErr: false,
			wantRet: []byte{31: 1}, // proposal id 1 is expected
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testApp := testkeeper.EVMTestApp
			ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
			k := &testApp.EvmKeeper
			k.SetAddressMapping(ctx, tt.args.callerSeiAddress, tt.args.caller)
			stateDb := state.NewDBImpl(ctx, k, true)
			evm := vm.EVM{
				StateDB:   stateDb,
				TxContext: vm.TxContext{Origin: tt.args.caller},
			}
			amt := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(ctx), sdk.NewInt(10000000)))
			require.Nil(t, k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, amt))
			require.Nil(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, tt.args.callerSeiAddress, amt))

			govMsgServer := govkeeper.NewMsgServerImpl(testApp.GovKeeper)
			p, _ := gov.NewPrecompile(testApp.GovKeeper, govMsgServer, k, k.BankKeeper())
			submitProposalMethod, err := p.ABI.MethodById(p.GetExecutor().(*gov.PrecompileExecutor).SubmitProposalID)
			require.Nil(t, err)
			inputs, err := submitProposalMethod.Inputs.Pack(tt.args.proposal)
			require.Nil(t, err)
			gotRet, err := p.Run(&evm, tt.args.caller, common.Address{}, append(p.GetExecutor().(*gov.PrecompileExecutor).SubmitProposalID, inputs...), nil, false, false, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("Run() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				require.Equal(t, vm.ErrExecutionReverted, err)
				require.Equal(t, tt.wantErrMsg, string(gotRet))
			} else if !reflect.DeepEqual(gotRet, tt.wantRet) {
				t.Errorf("Run() gotRet = %v, want %v", gotRet, tt.wantRet)
			}
		})
	}
}
