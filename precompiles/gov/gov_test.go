package gov_test

import (
	"embed"
	"encoding/hex"
	"math/big"
	"testing"
	"time"

	ethabi "github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	authztypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/authz"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	pcommon "github.com/sei-protocol/sei-chain/precompiles/common"
	"github.com/sei-protocol/sei-chain/precompiles/gov"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/ante"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
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
			ante.Preprocess(ctx, req, k.ChainID(ctx), false)
			res, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)
			if tt.wantErr {
				require.NotEmpty(t, res.VmError)
				require.Nil(t, res.ReturnData)
			} else {
				require.Nil(t, err)
				require.Empty(t, res.VmError)
				tt.verify(t, ctx, seiAddr, tt.args.proposal)
			}
		})
	}
}

func TestVoteAndProposalAuthorizationFlow(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	blockTime := time.Unix(1_700_000_000, 0).UTC()
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2).WithBlockTime(blockTime)
	k := &testApp.EvmKeeper

	content := govtypes.ContentFromProposalType("authorized vote", "authorized vote", govtypes.ProposalTypeText, false)
	proposal, err := testApp.GovKeeper.SubmitProposal(ctx, content)
	require.NoError(t, err)
	testApp.GovKeeper.ActivateVotingPeriod(ctx, proposal)

	granterSeiAddr, granterEVMAddr := testkeeper.MockAddressPair()
	granteeSeiAddr, granteeEVMAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, granterSeiAddr, granterEVMAddr)
	k.SetAddressMapping(ctx, granteeSeiAddr, granteeEVMAddr)

	p, err := gov.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)
	statedb := state.NewDBImpl(ctx, k, true)
	evm := vm.EVM{StateDB: statedb}

	call := func(caller common.Address, methodName string, value *big.Int, readOnly bool, delegateCall bool, args ...interface{}) ([]byte, error) {
		t.Helper()
		input, packErr := p.ABI.Pack(methodName, args...)
		require.NoError(t, packErr)
		ret, _, callErr := p.RunAndCalculateGas(&evm, caller, caller, input, 1_000_000, value, nil, readOnly, delegateCall)
		return ret, callErr
	}
	assertSuccess := func(methodName string, ret []byte) {
		t.Helper()
		outputs, unpackErr := p.ABI.Methods[methodName].Outputs.Unpack(ret)
		require.NoError(t, unpackErr)
		require.Equal(t, []interface{}{true}, outputs)
	}

	expiration := blockTime.Add(time.Hour)
	ret, err := call(
		granterEVMAddr,
		gov.GrantVoteMethod,
		big.NewInt(0),
		false,
		false,
		granteeEVMAddr,
		expiration.Unix(),
	)
	require.NoError(t, err)
	assertSuccess(gov.GrantVoteMethod, ret)

	voteMsgType := sdk.MsgTypeURL(&govtypes.MsgVote{})
	authorization, storedExpiration := testApp.AuthzKeeper.GetCleanAuthorization(
		statedb.Ctx(),
		granteeSeiAddr,
		granterSeiAddr,
		voteMsgType,
	)
	require.IsType(t, &authztypes.GenericAuthorization{}, authorization)
	require.Equal(t, expiration, storedExpiration)
	submitMsgType := sdk.MsgTypeURL(&govtypes.MsgSubmitProposal{})
	submitAuthorization, _ := testApp.AuthzKeeper.GetCleanAuthorization(
		statedb.Ctx(),
		granteeSeiAddr,
		granterSeiAddr,
		submitMsgType,
	)
	require.Nil(t, submitAuthorization)
	weightedAuthorization, _ := testApp.AuthzKeeper.GetCleanAuthorization(
		statedb.Ctx(),
		granteeSeiAddr,
		granterSeiAddr,
		sdk.MsgTypeURL(&govtypes.MsgVoteWeighted{}),
	)
	require.Nil(t, weightedAuthorization)

	ret, err = call(
		granteeEVMAddr,
		gov.VoteWithAuthzMethod,
		nil,
		false,
		false,
		granterEVMAddr,
		proposal.ProposalId,
		int32(govtypes.OptionYes),
	)
	require.NoError(t, err)
	assertSuccess(gov.VoteWithAuthzMethod, ret)
	vote, found := testApp.GovKeeper.GetVote(statedb.Ctx(), proposal.ProposalId, granterSeiAddr)
	require.True(t, found)
	require.Equal(t, govtypes.OptionYes, vote.Options[0].Option)

	_, err = call(
		granteeEVMAddr,
		gov.SubmitWithAuthzMethod,
		nil,
		false,
		false,
		granterEVMAddr,
		`{"title":"unauthorized proposal","description":"vote grant is insufficient","type":"Text"}`,
	)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)

	ret, err = call(
		granterEVMAddr,
		gov.GrantProposalMethod,
		nil,
		false,
		false,
		granteeEVMAddr,
		expiration.Unix(),
	)
	require.NoError(t, err)
	assertSuccess(gov.GrantProposalMethod, ret)
	submitAuthorization, submitExpiration := testApp.AuthzKeeper.GetCleanAuthorization(
		statedb.Ctx(),
		granteeSeiAddr,
		granterSeiAddr,
		submitMsgType,
	)
	require.IsType(t, &authztypes.GenericAuthorization{}, submitAuthorization)
	require.Equal(t, expiration, submitExpiration)

	initialDepositValue := big.NewInt(10_000_000_000_000)
	initialDeposit := sdk.NewCoins(sdk.NewCoin(k.GetBaseDenom(statedb.Ctx()), sdk.NewInt(10)))
	precompileAddr := k.GetSeiAddressOrDefault(statedb.Ctx(), common.HexToAddress(gov.GovAddress))
	require.NoError(t, k.BankKeeper().MintCoins(statedb.Ctx(), evmtypes.ModuleName, initialDeposit))
	require.NoError(t, k.BankKeeper().SendCoinsFromModuleToAccount(statedb.Ctx(), evmtypes.ModuleName, precompileAddr, initialDeposit))
	ret, err = call(
		granteeEVMAddr,
		gov.SubmitWithAuthzMethod,
		initialDepositValue,
		false,
		false,
		granterEVMAddr,
		`{"title":"authorized proposal","description":"submitted through authz","type":"Text"}`,
	)
	require.NoError(t, err)
	submitOutputs, err := p.ABI.Methods[gov.SubmitWithAuthzMethod].Outputs.Unpack(ret)
	require.NoError(t, err)
	authorizedProposalID := submitOutputs[0].(uint64)
	authorizedProposal, found := testApp.GovKeeper.GetProposal(statedb.Ctx(), authorizedProposalID)
	require.True(t, found)
	require.Equal(t, initialDeposit, authorizedProposal.TotalDeposit)
	ret, err = call(granterEVMAddr, gov.RevokeVoteMethod, nil, false, false, granteeEVMAddr)
	require.NoError(t, err)
	assertSuccess(gov.RevokeVoteMethod, ret)
	authorization, _ = testApp.AuthzKeeper.GetCleanAuthorization(
		statedb.Ctx(),
		granteeSeiAddr,
		granterSeiAddr,
		voteMsgType,
	)
	require.Nil(t, authorization)
	submitAuthorization, _ = testApp.AuthzKeeper.GetCleanAuthorization(
		statedb.Ctx(),
		granteeSeiAddr,
		granterSeiAddr,
		submitMsgType,
	)
	require.IsType(t, &authztypes.GenericAuthorization{}, submitAuthorization)

	_, err = call(
		granteeEVMAddr,
		gov.VoteWithAuthzMethod,
		nil,
		false,
		false,
		granterEVMAddr,
		proposal.ProposalId,
		int32(govtypes.OptionNo),
	)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	vote, found = testApp.GovKeeper.GetVote(statedb.Ctx(), proposal.ProposalId, granterSeiAddr)
	require.True(t, found)
	require.Equal(t, govtypes.OptionYes, vote.Options[0].Option)

	ret, err = call(granterEVMAddr, gov.RevokeProposalMethod, nil, false, false, granteeEVMAddr)
	require.NoError(t, err)
	assertSuccess(gov.RevokeProposalMethod, ret)
	submitAuthorization, _ = testApp.AuthzKeeper.GetCleanAuthorization(
		statedb.Ctx(),
		granteeSeiAddr,
		granterSeiAddr,
		submitMsgType,
	)
	require.Nil(t, submitAuthorization)
	_, err = call(
		granteeEVMAddr,
		gov.SubmitWithAuthzMethod,
		nil,
		false,
		false,
		granterEVMAddr,
		`{"title":"revoked proposal","description":"proposal grant was revoked","type":"Text"}`,
	)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)

	_, err = call(
		granterEVMAddr,
		gov.GrantVoteMethod,
		nil,
		false,
		false,
		granteeEVMAddr,
		blockTime.Unix(),
	)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)

	_, err = call(
		granterEVMAddr,
		gov.GrantVoteMethod,
		big.NewInt(1),
		false,
		false,
		granteeEVMAddr,
		expiration.Unix(),
	)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)

	_, err = call(
		granterEVMAddr,
		gov.GrantVoteMethod,
		nil,
		true,
		false,
		granteeEVMAddr,
		expiration.Unix(),
	)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)

	_, err = call(
		granterEVMAddr,
		gov.GrantVoteMethod,
		nil,
		false,
		true,
		granteeEVMAddr,
		expiration.Unix(),
	)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
}

func TestPrecompileExecutor_submitProposal(t *testing.T) {
	// Setup shared test resources
	privKey := testkeeper.MockPrivateKey()
	callerSeiAddress, callerEvmAddress := testkeeper.PrivateKeyToAddresses(privKey)
	recipientSeiAddress, recipientEvmAddress := testkeeper.MockAddressPair()

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
		},
		{
			name: "returns proposal id on submit text proposal with valid content and no deposit (zero value)",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Gov Proposal",
					"description": "This is a gov proposal",
					"type": "Text"
				}`,
				value: big.NewInt(0),
			},
			wantErr: false,
		},
		{
			name: "returns proposal id on submit text proposal with valid content and nil deposit",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Gov Proposal",
					"description": "This is a gov proposal",
					"type": "Text"
				}`,
				value: nil,
			},
			wantErr: false,
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
					"is_expedited": false
				}`,
				value: big.NewInt(1_000_000_000_000_000_000),
			},
			wantErr: false,
		},
		{
			name: "returns proposal id on submit cancel software upgrade proposal",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Cancel Upgrade",
					"description": "Cancel the pending software upgrade",
					"type": "CancelSoftwareUpgrade"
				}`,
				value: big.NewInt(1_000_000_000_000_000_000),
			},
			wantErr: false,
		},
		{
			name: "returns error on parameter change proposal with no changes",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Invalid proposal",
					"description": "This proposal has no changes",
					"type": "ParameterChange"
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
					]
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
					}
				}`,
			},
			wantErr: false,
		},
		{
			name: "returns error on software upgrade proposal with no plan",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Invalid upgrade",
					"description": "This proposal has no plan",
					"type": "SoftwareUpgrade"
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
					}
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
					}
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
					}
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
					}
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
					}
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
					}
				}`,
				value: big.NewInt(1_000_000_000_000_000_000),
			},
			wantErr: false,
		},
		{
			name: "returns error on community pool spend proposal with no parameters",
			args: args{
				caller:           callerEvmAddress,
				callerSeiAddress: callerSeiAddress,
				proposal: `{
					"title": "Invalid spend",
					"description": "This proposal has no parameters",
					"type": "CommunityPoolSpend"
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
					}
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
					}
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
					}
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
					}
				}`,
			},
			wantErr:    true,
			wantErrMsg: "invalid amount format: invalid decimal coin expression: invalid",
		},
		{
			name: "returns error on update resource dependency proposal due to deprecation",
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
			wantErrMsg: "unsupported proposal type: UpdateResourceDependencyMapping",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a fresh testApp instance for each test
			testApp := testkeeper.EVMTestApp
			// Create a fresh context for each test
			ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(3)

			// Dynamically determine the expected proposal ID for this test
			proposals := testApp.GovKeeper.GetProposals(ctx)
			expectedProposalID := byte(len(proposals) + 1)

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
			ante.Preprocess(ctx, req, k.ChainID(ctx), false)
			gotRet, err := msgServer.EVMTransaction(sdk.WrapSDKContext(ctx), req)

			if tt.wantErr {
				require.NotEmpty(t, gotRet.VmError)
				require.Nil(t, gotRet.ReturnData)
			} else {
				require.Empty(t, gotRet.VmError)
				require.Nil(t, err)
				require.Equal(t, []byte{31: expectedProposalID}, gotRet.ReturnData)
			}
		})
	}
}

func TestGovQueryPrecompile(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(2)
	k := &testApp.EvmKeeper
	baseDenom := k.GetBaseDenom(ctx)

	// Seed a proposal in voting period.
	content := govtypes.ContentFromProposalType("query title", "query description", govtypes.ProposalTypeText, false)
	proposal, err := testApp.GovKeeper.SubmitProposal(ctx, content)
	require.Nil(t, err)
	testApp.GovKeeper.ActivateVotingPeriod(ctx, proposal)
	proposalID := proposal.ProposalId

	// Seed a vote.
	voterSeiAddr, voterEvmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, voterSeiAddr, voterEvmAddr)
	require.Nil(t, testApp.GovKeeper.AddVote(ctx, proposalID, voterSeiAddr, govtypes.NewNonSplitVoteOption(govtypes.OptionYes)))

	// Seed a deposit.
	depositorSeiAddr, depositorEvmAddr := testkeeper.MockAddressPair()
	k.SetAddressMapping(ctx, depositorSeiAddr, depositorEvmAddr)
	depositCoins := sdk.NewCoins(sdk.NewCoin(baseDenom, sdk.NewInt(1000000)))
	require.Nil(t, k.BankKeeper().MintCoins(ctx, evmtypes.ModuleName, depositCoins))
	require.Nil(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, evmtypes.ModuleName, depositorSeiAddr, depositCoins))
	_, err = testApp.GovKeeper.AddDeposit(ctx, proposalID, depositorSeiAddr, depositCoins)
	require.Nil(t, err)

	storedProposal, found := testApp.GovKeeper.GetProposal(ctx, proposalID)
	require.True(t, found)

	p, err := gov.NewPrecompile(testApp.GetPrecompileKeepers())
	require.Nil(t, err)
	govAbi := pcommon.MustGetABI(f, "abi.json")

	query := func(t *testing.T, methodName string, args ...interface{}) []interface{} {
		method, ok := govAbi.Methods[methodName]
		require.True(t, ok, "method %s not found in abi", methodName)
		inputs, err := method.Inputs.Pack(args...)
		require.Nil(t, err)
		statedb := state.NewDBImpl(ctx, k, true)
		evm := vm.EVM{StateDB: statedb}
		ret, _, err := p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), 1000000, nil, nil, true, false)
		require.Nil(t, err)
		outputs, err := method.Outputs.Unpack(ret)
		require.Nil(t, err)
		return outputs
	}

	t.Run("proposal", func(t *testing.T) {
		outputs := query(t, gov.ProposalQueryMethod, proposalID)
		require.Len(t, outputs, 1)
		got := *ethabi.ConvertType(outputs[0], new(gov.ProposalData)).(*gov.ProposalData)
		require.Equal(t, proposalID, got.Id)
		require.Equal(t, int32(govtypes.StatusVotingPeriod), got.Status)
		require.Equal(t, storedProposal.SubmitTime.Unix(), got.SubmitTime)
		require.Equal(t, storedProposal.DepositEndTime.Unix(), got.DepositEndTime)
		require.Equal(t, storedProposal.VotingStartTime.Unix(), got.VotingStartTime)
		require.Equal(t, storedProposal.VotingEndTime.Unix(), got.VotingEndTime)
		require.False(t, got.IsExpedited)
		require.Equal(t, []gov.Coin{{Amount: big.NewInt(1000000), Denom: baseDenom}}, got.TotalDeposit)
		require.Contains(t, string(got.Content), "\"@type\"")
		require.Contains(t, string(got.Content), "TextProposal")
		require.Contains(t, string(got.Content), "query title")
	})

	t.Run("proposals filtered by depositor", func(t *testing.T) {
		outputs := query(t, gov.ProposalsQueryMethod, int32(0), common.Address{}, depositorEvmAddr, []byte{})
		require.Len(t, outputs, 2)
		gotProposals := *ethabi.ConvertType(outputs[0], new([]gov.ProposalData)).(*[]gov.ProposalData)
		require.Len(t, gotProposals, 1)
		require.Equal(t, proposalID, gotProposals[0].Id)
		require.Contains(t, string(gotProposals[0].Content), "\"@type\"")
		require.Empty(t, outputs[1].([]byte))
	})

	t.Run("proposals filtered by voter and status", func(t *testing.T) {
		outputs := query(t, gov.ProposalsQueryMethod, int32(govtypes.StatusVotingPeriod), voterEvmAddr, common.Address{}, []byte{})
		gotProposals := *ethabi.ConvertType(outputs[0], new([]gov.ProposalData)).(*[]gov.ProposalData)
		require.Len(t, gotProposals, 1)
		require.Equal(t, proposalID, gotProposals[0].Id)
	})

	t.Run("vote", func(t *testing.T) {
		outputs := query(t, gov.VoteQueryMethod, proposalID, voterEvmAddr)
		require.Len(t, outputs, 1)
		got := *ethabi.ConvertType(outputs[0], new(gov.VoteData)).(*gov.VoteData)
		require.Equal(t, proposalID, got.ProposalId)
		require.Equal(t, voterSeiAddr.String(), got.Voter)
		require.Equal(t, []gov.WeightedVoteOptionData{{Option: int32(govtypes.OptionYes), Weight: sdk.OneDec().String()}}, got.Options)
	})

	t.Run("votes", func(t *testing.T) {
		outputs := query(t, gov.VotesQueryMethod, proposalID, []byte{})
		require.Len(t, outputs, 2)
		gotVotes := *ethabi.ConvertType(outputs[0], new([]gov.VoteData)).(*[]gov.VoteData)
		require.Len(t, gotVotes, 1)
		require.Equal(t, voterSeiAddr.String(), gotVotes[0].Voter)
		require.Empty(t, outputs[1].([]byte))
	})

	t.Run("params", func(t *testing.T) {
		outputs := query(t, gov.ParamsQueryMethod)
		require.Len(t, outputs, 1)
		got := *ethabi.ConvertType(outputs[0], new(gov.GovParams)).(*gov.GovParams)
		votingParams := testApp.GovKeeper.GetVotingParams(ctx)
		depositParams := testApp.GovKeeper.GetDepositParams(ctx)
		tallyParams := testApp.GovKeeper.GetTallyParams(ctx)
		require.Equal(t, uint64(votingParams.VotingPeriod.Seconds()), got.VotingPeriod)
		require.Equal(t, uint64(votingParams.ExpeditedVotingPeriod.Seconds()), got.ExpeditedVotingPeriod)
		require.Equal(t, uint64(depositParams.MaxDepositPeriod.Seconds()), got.MaxDepositPeriod)
		require.Len(t, got.MinDeposit, len(depositParams.MinDeposit))
		for i, coin := range depositParams.MinDeposit {
			require.Equal(t, coin.Denom, got.MinDeposit[i].Denom)
			require.Equal(t, coin.Amount.BigInt(), got.MinDeposit[i].Amount)
		}
		require.Len(t, got.MinExpeditedDeposit, len(depositParams.MinExpeditedDeposit))
		require.Equal(t, tallyParams.Quorum.String(), got.Quorum)
		require.Equal(t, tallyParams.Threshold.String(), got.Threshold)
		require.Equal(t, tallyParams.VetoThreshold.String(), got.VetoThreshold)
		require.Equal(t, tallyParams.ExpeditedQuorum.String(), got.ExpeditedQuorum)
		require.Equal(t, tallyParams.ExpeditedThreshold.String(), got.ExpeditedThreshold)
	})

	t.Run("deposit", func(t *testing.T) {
		outputs := query(t, gov.DepositQueryMethod, proposalID, depositorEvmAddr)
		require.Len(t, outputs, 1)
		got := *ethabi.ConvertType(outputs[0], new(gov.DepositData)).(*gov.DepositData)
		require.Equal(t, proposalID, got.ProposalId)
		require.Equal(t, depositorSeiAddr.String(), got.Depositor)
		require.Equal(t, []gov.Coin{{Amount: big.NewInt(1000000), Denom: baseDenom}}, got.Amount)
	})

	t.Run("deposits", func(t *testing.T) {
		outputs := query(t, gov.DepositsQueryMethod, proposalID, []byte{})
		require.Len(t, outputs, 2)
		gotDeposits := *ethabi.ConvertType(outputs[0], new([]gov.DepositData)).(*[]gov.DepositData)
		require.Len(t, gotDeposits, 1)
		require.Equal(t, depositorSeiAddr.String(), gotDeposits[0].Depositor)
		require.Equal(t, []gov.Coin{{Amount: big.NewInt(1000000), Denom: baseDenom}}, gotDeposits[0].Amount)
		require.Empty(t, outputs[1].([]byte))
	})

	t.Run("tallyResult", func(t *testing.T) {
		outputs := query(t, gov.TallyResultQueryMethod, proposalID)
		require.Len(t, outputs, 1)
		got := *ethabi.ConvertType(outputs[0], new(gov.TallyResultData)).(*gov.TallyResultData)
		// The voter has no staked tokens, so the live tally is all zeros.
		require.Equal(t, gov.TallyResultData{Yes: "0", Abstain: "0", No: "0", NoWithVeto: "0"}, got)
	})

	t.Run("vote query with zero voter address fails", func(t *testing.T) {
		method := govAbi.Methods[gov.VoteQueryMethod]
		inputs, err := method.Inputs.Pack(proposalID, common.Address{})
		require.Nil(t, err)
		statedb := state.NewDBImpl(ctx, k, true)
		evm := vm.EVM{StateDB: statedb}
		_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), 1000000, nil, nil, true, false)
		require.NotNil(t, err)
	})

	t.Run("tallyResult does not delete votes", func(t *testing.T) {
		method := govAbi.Methods[gov.TallyResultQueryMethod]
		inputs, err := method.Inputs.Pack(proposalID)
		require.Nil(t, err)
		statedb := state.NewDBImpl(ctx, k, true)
		evm := vm.EVM{StateDB: statedb}
		_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), 1000000, nil, nil, true, false)
		require.Nil(t, err)
		// Tally deletes votes as it counts; the view must run on a branched
		// context so the deletion is discarded.
		_, found := testApp.GovKeeper.GetVote(statedb.Ctx(), proposalID, voterSeiAddr)
		require.True(t, found)
	})

	t.Run("out of gas returns revert error instead of panicking", func(t *testing.T) {
		method := govAbi.Methods[gov.ProposalQueryMethod]
		inputs, err := method.Inputs.Pack(proposalID)
		require.Nil(t, err)
		statedb := state.NewDBImpl(ctx, k, true)
		evm := vm.EVM{StateDB: statedb}
		require.NotPanics(t, func() {
			_, _, err = p.RunAndCalculateGas(&evm, common.Address{}, common.Address{}, append(method.ID, inputs...), 1, nil, nil, true, false)
		})
		require.NotNil(t, err)
	})
}
