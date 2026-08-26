package gov_test

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/require"

	seiapp "github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/legacyabci"
	appparams "github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/app/retiredibc"
	retiredibcgov "github.com/sei-protocol/sei-chain/app/retiredibc/gov"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/gov"
	govtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/gov/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/staking"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// Fixtures were encoded by app.MakeEncodingConfig at release/v6.6 commit 829785f19.
// The upgrade fixtures carry the nested client-state Any in the canonical form a
// v6.6 node writes to state: v6.6's UnpackAny repacks every Any from its decoded
// value, so stored bytes always spell out the client state's empty non-nullable
// submessages rather than the minimal encoding.
const (
	clientUpdateContentHex  = "0a0e7265636f76657220636c69656e7412157265706c6163652066726f7a656e20636c69656e741a0f30372d74656e6465726d696e742d30220f30372d74656e6465726d696e742d31"
	clientUpdateProposalHex = "082912750a282f6962632e636f72652e636c69656e742e76312e436c69656e7455706461746550726f706f73616c12490a0e7265636f76657220636c69656e7412157265706c6163652066726f7a656e20636c69656e741a0f30372d74656e6465726d696e742d30220f30372d74656e6465726d696e742d311801220c0a01301201301a01302201302a0608a593c99d06320608a5d9d39d06420b088092b8c398feffffff014a0b088092b8c398feffffff01"
	upgradeContentHex       = "0a0b494243207570677261646512117072657365727665206368616e6e656c731a220a0676362e372e30120b088092b8c398feffffff0118c0c407220772656c6561736522460a2b2f6962632e6c69676874636c69656e74732e74656e6465726d696e742e76312e436c69656e74537461746512170a09706163696669632d3112001a0022002a0032003a00"
	upgradeProposalHex      = "082a12b4010a232f6962632e636f72652e636c69656e742e76312e5570677261646550726f706f73616c128c010a0b494243207570677261646512117072657365727665206368616e6e656c731a220a0676362e372e30120b088092b8c398feffffff0118c0c407220772656c6561736522460a2b2f6962632e6c69676874636c69656e74732e74656e6465726d696e742e76312e436c69656e74537461746512170a09706163696669632d3112001a0022002a0032003a001801220c0a01301201301a01302201302a0608a593c99d06320608a5d9d39d06420b088092b8c398feffffff014a0b088092b8c398feffffff01"
)

func TestRetiredIBCProposalV66CodecCompatibility(t *testing.T) {
	encoding := seiapp.MakeEncodingConfig()
	require.Equal(t, "ibc.core.client.v1.ClientUpdateProposal", proto.MessageName(&retiredibcgov.ClientUpdateProposal{}))
	require.Equal(t, "ibc.core.client.v1.UpgradeProposal", proto.MessageName(&retiredibcgov.UpgradeProposal{}))

	var clientUpdate retiredibcgov.ClientUpdateProposal
	clientUpdateFixture := mustDecodeHex(t, clientUpdateContentHex)
	require.NoError(t, encoding.Marshaler.Unmarshal(clientUpdateFixture, &clientUpdate))
	require.Equal(t, "recover client", clientUpdate.Title)
	require.Equal(t, "07-tendermint-0", clientUpdate.SubjectClientId)
	clientUpdateRoundTrip, err := encoding.Marshaler.Marshal(&clientUpdate)
	require.NoError(t, err)
	require.Equal(t, clientUpdateFixture, clientUpdateRoundTrip)

	var upgrade retiredibcgov.UpgradeProposal
	upgradeFixture := mustDecodeHex(t, upgradeContentHex)
	require.NoError(t, encoding.Marshaler.Unmarshal(upgradeFixture, &upgrade))
	require.Equal(t, "v6.7.0", upgrade.Plan.Name)
	require.Equal(t, "/ibc.lightclients.tendermint.v1.ClientState", upgrade.UpgradedClientState.TypeUrl)
	require.Equal(t, []byte{
		0x0a, 0x09, 'p', 'a', 'c', 'i', 'f', 'i', 'c', '-', '1', // chain_id
		0x12, 0x00, 0x1a, 0x00, 0x22, 0x00, 0x2a, 0x00, 0x32, 0x00, 0x3a, 0x00, // empty non-nullable submessages
	}, upgrade.UpgradedClientState.Value)
	require.Nil(t, upgrade.UpgradedClientState.GetCachedValue())
	require.NotPanics(t, func() { _ = upgrade.String() })
	upgradeRoundTrip, err := encoding.Marshaler.Marshal(&upgrade)
	require.NoError(t, err)
	require.Equal(t, upgradeFixture, upgradeRoundTrip)

	proposals := []struct {
		name            string
		encodedProposal string
		typeURL         string
		contentType     any
	}{
		{"client update", clientUpdateProposalHex, retiredibcgov.ClientUpdateProposalTypeURL, &retiredibcgov.ClientUpdateProposal{}},
		{"upgrade", upgradeProposalHex, retiredibcgov.UpgradeProposalTypeURL, &retiredibcgov.UpgradeProposal{}},
	}
	for _, encoding := range []struct {
		name   string
		config func() appparams.EncodingConfig
	}{
		{"app", seiapp.MakeEncodingConfig},
		{"legacy app", seiapp.MakeLegacyEncodingConfig},
	} {
		t.Run(encoding.name, func(t *testing.T) {
			for _, proposalFixture := range proposals {
				t.Run(proposalFixture.name, func(t *testing.T) {
					var proposal govtypes.Proposal
					codec := encoding.config().Marshaler
					require.NoError(t, codec.Unmarshal(mustDecodeHex(t, proposalFixture.encodedProposal), &proposal))
					require.Equal(t, proposalFixture.typeURL, proposal.Content.TypeUrl)
					require.IsType(t, proposalFixture.contentType, proposal.GetContent())
				})
			}
		})
	}
}

func TestRetiredIBCProposalSubmissionIsRejected(t *testing.T) {
	for _, tc := range []struct {
		content govtypes.Content
		typeURL string
	}{
		{&retiredibcgov.ClientUpdateProposal{Title: "title", Description: "description"}, retiredibcgov.ClientUpdateProposalTypeURL},
		{&retiredibcgov.UpgradeProposal{Title: "title", Description: "description"}, retiredibcgov.UpgradeProposalTypeURL},
	} {
		packed, err := codectypes.NewAnyWithValue(tc.content.(proto.Message))
		require.NoError(t, err)
		require.Equal(t, tc.typeURL, packed.TypeUrl)

		msg, err := govtypes.NewMsgSubmitProposal(tc.content, nil, sdk.AccAddress("12345678901234567890"))
		require.NoError(t, err)
		requireRetiredIBCError(t, msg.ValidateBasic())
	}

	testApp := seiapp.Setup(t, false, false, false)
	ctx := testApp.NewContext(false, tmproto.Header{})
	content := &retiredibcgov.ClientUpdateProposal{}
	msg, err := govtypes.NewMsgSubmitProposal(content, nil, sdk.AccAddress("12345678901234567890"))
	require.NoError(t, err)
	handler := testApp.MsgServiceRouter().Handler(msg)
	require.NotNil(t, handler)
	_, err = handler(ctx, msg)
	requireRetiredIBCError(t, err)
}

func TestRetiredIBCProposalDepositQueueDoesNotPanic(t *testing.T) {
	testApp := seiapp.Setup(t, false, false, false)
	ctx := testApp.BaseApp.NewContext(false, tmproto.Header{Time: time.Date(2023, time.January, 5, 0, 0, 0, 0, time.UTC)})

	proposal := decodeV66Proposal(t, testApp, clientUpdateProposalHex)
	testApp.GovKeeper.SetProposal(ctx, proposal)
	testApp.GovKeeper.InsertInactiveProposalQueue(ctx, proposal.ProposalId, proposal.DepositEndTime)

	require.NotPanics(t, func() {
		gov.EndBlocker(ctx, testApp.GovKeeper)
	})
	_, found := testApp.GovKeeper.GetProposal(ctx, proposal.ProposalId)
	require.False(t, found)
}

func TestRetiredIBCProposalPassingVoteFailsExecution(t *testing.T) {
	testApp := seiapp.Setup(t, false, false, false)
	ctx := testApp.BaseApp.NewContext(false, tmproto.Header{})
	addrs := seiapp.AddTestAddrs(testApp, ctx, 2, valTokens)
	SortAddresses(addrs)

	params := testApp.StakingKeeper.GetParams(ctx)
	params.MinCommissionRate = sdk.ZeroDec()
	testApp.StakingKeeper.SetParams(ctx, params)
	header := tmproto.Header{Height: testApp.LastBlockHeight() + 1}
	legacyabci.BeginBlock(ctx, header.Height, []abci.VoteInfo{}, []abci.Misbehavior{}, testApp.BeginBlockKeepers)
	createValidators(t, staking.NewHandler(testApp.StakingKeeper), ctx, []sdk.ValAddress{sdk.ValAddress(addrs[0])}, []int64{10})
	staking.EndBlocker(ctx, testApp.StakingKeeper)

	proposal := decodeV66Proposal(t, testApp, upgradeProposalHex)
	proposal.Status = govtypes.StatusVotingPeriod
	proposal.VotingStartTime = ctx.BlockTime()
	proposal.VotingEndTime = ctx.BlockTime().Add(time.Second)
	testApp.GovKeeper.SetProposal(ctx, proposal)
	testApp.GovKeeper.InsertActiveProposalQueue(ctx, proposal.ProposalId, proposal.VotingEndTime)
	require.NoError(t, testApp.GovKeeper.AddVote(ctx, proposal.ProposalId, addrs[0], govtypes.NewNonSplitVoteOption(govtypes.OptionYes)))

	ctx = ctx.WithBlockTime(proposal.VotingEndTime.Add(time.Second))
	require.NotPanics(t, func() {
		gov.EndBlocker(ctx, testApp.GovKeeper)
	})

	stored, found := testApp.GovKeeper.GetProposal(ctx, proposal.ProposalId)
	require.True(t, found)
	require.Equal(t, govtypes.StatusFailed, stored.Status)
}

func TestRetiredIBCCompatibilityDoesNotRestoreIBCModuleOrMessages(t *testing.T) {
	encoding := seiapp.MakeEncodingConfig()
	var msg sdk.Msg
	err := encoding.InterfaceRegistry.UnpackAny(&codectypes.Any{
		TypeUrl: "/ibc.core.client.v1.MsgCreateClient",
		Value:   nil,
	}, &msg)
	require.ErrorContains(t, err, "no concrete type registered")

	_, ibcRegistered := seiapp.ModuleBasics["ibc"]
	_, transferRegistered := seiapp.ModuleBasics["transfer"]
	require.False(t, ibcRegistered)
	require.False(t, transferRegistered)
}

func decodeV66Proposal(t *testing.T, testApp *seiapp.App, encoded string) govtypes.Proposal {
	t.Helper()
	var proposal govtypes.Proposal
	require.NoError(t, testApp.AppCodec().Unmarshal(mustDecodeHex(t, encoded), &proposal))
	return proposal
}

func mustDecodeHex(t *testing.T, encoded string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(encoded)
	require.NoError(t, err)
	return decoded
}

func requireRetiredIBCError(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	codespace, code, _ := sdkerrors.ABCIInfo(err, false)
	require.Equal(t, "ibc", codespace)
	require.EqualValues(t, 103, code)
	require.ErrorIs(t, err, retiredibc.ErrDeprecated)
}
