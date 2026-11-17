package cli

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testTime is 2026-11-12T00:00:00Z in Unix nanoseconds.
const testTime = uint64(1_794_441_600_000_000_000)

var testVoter = common.HexToAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8")

func mustPack(t *testing.T, args interface{ Pack(...any) ([]byte, error) }, v any) []byte {
	bz, err := args.Pack(v)
	require.NoError(t, err)
	return bz
}

func testProposal(t *testing.T, id uint64) proposalData {
	return proposalData{
		Id:     id,
		Status: 2,
		FinalTallyResult: tallyData{
			Yes: big.NewInt(0), Abstain: big.NewInt(0), No: big.NewInt(0), NoWithVeto: big.NewInt(0),
		},
		SubmitTime:      testTime,
		DepositEndTime:  testTime + 100_000_000_000,
		TotalDeposit:    wei(20_000_000),
		VotingStartTime: testTime,
		VotingEndTime:   testTime + 30_000_000_000,
		IsExpedited:     true,
		ContentType:     contentSoftwareUpgrade,
		Content: mustPack(t, softwareUpgArgs, softwareUpgradeContent{
			Title: "T", Description: "D", Plan: upgradePlan{Name: "v2.0", Height: 100, Info: "{}"}, IsExpedited: true,
		}),
	}
}

func TestQueryProposalPrintsV2(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	p := testProposal(t, 3)
	stub.on("proposal(uint64)", func(a []any) ([]any, error) {
		assert.Equal(t, uint64(3), a[0])
		return []any{p}, nil
	})
	require.NoError(t, r.run("", args("q gov proposal 3 -o json --evm-rpc", stub.url)...))
	require.JSONEq(t, `{
  "proposal_id": "3",
  "content": {
    "@type": "/cosmos.upgrade.v1beta1.SoftwareUpgradeProposal",
    "title": "T",
    "description": "D",
    "plan": {"name": "v2.0", "time": "0001-01-01T00:00:00Z", "height": "100", "info": "{}"}
  },
  "status": "PROPOSAL_STATUS_VOTING_PERIOD",
  "final_tally_result": {"yes": "0", "abstain": "0", "no": "0", "no_with_veto": "0"},
  "submit_time": "2026-11-12T00:00:00Z",
  "deposit_end_time": "2026-11-12T00:01:40Z",
  "total_deposit": [{"denom": "usei", "amount": "20000000"}],
  "voting_start_time": "2026-11-12T00:00:00Z",
  "voting_end_time": "2026-11-12T00:00:30Z",
  "is_expedited": true
}`, r.out.String())
}

func TestQueryProposalsFollowsPages(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	p := testProposal(t, 1)
	stub.on("proposals(int32,address,address,bytes)", func(a []any) ([]any, error) {
		assert.Equal(t, int32(3), a[0])
		assert.Equal(t, testVoter, a[1])
		switch string(a[3].([]byte)) {
		case "":
			return []any{[]proposalData{}, []byte("p2")}, nil
		case "p2":
			return []any{[]proposalData{p}, []byte("p3")}, nil
		default:
			return []any{[]proposalData{p}, []byte{}}, nil
		}
	})
	// An empty page is followed until --limit proposals are collected.
	require.NoError(t, r.run("", args("q gov proposals --status passed --limit 1 -o json --voter", testVoter.Hex(), "--evm-rpc", stub.url)...))
	require.Contains(t, r.out.String(), `"next_key":"AHAz"`)
	require.Contains(t, r.out.String(), `"proposal_id":"1"`)

	// --page-key resumes from a printed next_key.
	require.NoError(t, r.run("", args("q gov proposals --status passed --page-key AHAz -o json --voter", testVoter.Hex(), "--evm-rpc", stub.url)...))
	require.Contains(t, r.out.String(), `"next_key":null`)

	require.ErrorContains(t, r.run("", args("q gov proposals --voter sei1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq --evm-rpc", stub.url)...), "not a 0x address")
	require.ErrorContains(t, newTestRoot(t).run("", args("q gov proposals --page 2 --evm-rpc", stub.url)...), "--page is not supported")
}

func TestQueryVotesLimitCapsWithinAPage(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	voters := []common.Address{{1}, {2}, {3}}
	stub.on("votes(uint64,bytes)", func(a []any) ([]any, error) {
		assert.Empty(t, a[1].([]byte))
		page := make([]voteData, len(voters))
		for i, v := range voters {
			page[i] = voteData{ProposalId: 3, Voter: v, Options: []weightedOption{{Option: 1, Weight: e18(1, 1)}}}
		}
		return []any{page, []byte{}}, nil
	})
	voted := func() []string {
		var resp struct {
			Votes []struct {
				Voter string `json:"voter"`
			} `json:"votes"`
			Pagination struct {
				NextKey *string `json:"next_key"`
			} `json:"pagination"`
		}
		require.NoError(t, json.Unmarshal(r.out.Bytes(), &resp))
		var out []string
		for _, v := range resp.Votes {
			out = append(out, v.Voter)
		}
		if resp.Pagination.NextKey != nil {
			out = append(out, "next="+*resp.Pagination.NextKey)
		}
		return out
	}

	require.NoError(t, r.run("", args("q gov votes 3 --limit 2 -o json --evm-rpc", stub.url)...))
	got := voted()
	require.Equal(t, []string{voters[0].Hex(), voters[1].Hex()}, got[:2])
	require.Len(t, got, 3)

	// The next_key resumes inside the page, after the votes already printed.
	require.NoError(t, r.run("", args("q gov votes 3 -o json --page-key", got[2][len("next="):], "--evm-rpc", stub.url)...))
	require.Equal(t, []string{voters[2].Hex()}, voted())
}

func TestQueryProposalNamesTheCancelledPlan(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	p := testProposal(t, 4)
	p.ContentType = contentCancelSoftwareUpgrade
	p.Content = mustPack(t, cancelUpgradeArgs, cancelSoftwareUpgradeContent{Title: "T", Description: "D", Name: "v2.0"})
	stub.on("proposal(uint64)", func([]any) ([]any, error) { return []any{p}, nil })

	require.NoError(t, r.run("", args("q gov proposal 4 -o json --evm-rpc", stub.url)...))
	var resp struct {
		Content map[string]any `json:"content"`
	}
	require.NoError(t, json.Unmarshal(r.out.Bytes(), &resp))
	require.Equal(t, map[string]any{
		"@type":       "/cosmos.upgrade.v1beta1.CancelSoftwareUpgradeProposal",
		"name":        "v2.0",
		"title":       "T",
		"description": "D",
	}, resp.Content)

	require.NoError(t, r.run("", args("q gov proposal 4 -o text --evm-rpc", stub.url)...))
	require.Contains(t, r.out.String(), "name: v2.0")
}

func TestQueryVotePrintsV2(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	stub.on("getVote(uint64,address)", func(a []any) ([]any, error) {
		return []any{voteData{ProposalId: a[0].(uint64), Voter: a[1].(common.Address), Options: []weightedOption{{Option: 1, Weight: e18(1, 1)}}}}, nil
	})
	require.NoError(t, r.run("", args("q gov vote 3 "+testVoter.Hex()+" -o json --evm-rpc", stub.url)...))
	require.JSONEq(t, `{
  "proposal_id": "3",
  "voter": "`+testVoter.Hex()+`",
  "option": "VOTE_OPTION_YES",
  "options": [{"option": "VOTE_OPTION_YES", "weight": "1.000000000000000000"}]
}`, r.out.String())
}

func TestQueryDepositsPrintsV2(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	stub.on("deposits(uint64,bytes)", func(a []any) ([]any, error) {
		return []any{[]depositData{{ProposalId: a[0].(uint64), Depositor: testVoter, Amount: wei(7)}}, []byte{}}, nil
	})
	require.NoError(t, r.run("", args("q gov deposits 3 -o json --evm-rpc", stub.url)...))
	require.JSONEq(t, `{
  "deposits": [{"proposal_id": "3", "depositor": "`+testVoter.Hex()+`", "amount": [{"denom": "usei", "amount": "7"}]}],
  "pagination": {"next_key": null, "total": "0"}
}`, r.out.String())
}

func TestQueryGovParamsPrintsV2(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	stub.on("params()", func([]any) ([]any, error) {
		return []any{govParamsData{
			VotingPeriod: 30_000_000_000, ExpeditedVotingPeriod: 15_000_000_000,
			MinDeposit: wei(10_000_000), MaxDepositPeriod: 100_000_000_000, MinExpeditedDeposit: wei(20_000_000),
			Quorum: e18(1, 2), Threshold: e18(1, 2), VetoThreshold: e18(334, 1000),
			ExpeditedQuorum: e18(2, 3), ExpeditedThreshold: e18(2, 3),
		}}, nil
	})
	require.NoError(t, r.run("", args("q gov params -o json --evm-rpc", stub.url)...))
	require.JSONEq(t, `{
  "voting_params": {"voting_period": "30000000000", "expedited_voting_period": "15000000000"},
  "tally_params": {
    "quorum": "0.500000000000000000",
    "threshold": "0.500000000000000000",
    "veto_threshold": "0.334000000000000000",
    "expedited_quorum": "0.666666666666666666",
    "expedited_threshold": "0.666666666666666666"
  },
  "deposit_params": {
    "min_deposit": [{"denom": "usei", "amount": "10000000"}],
    "max_deposit_period": "100000000000",
    "min_expedited_deposit": [{"denom": "usei", "amount": "20000000"}]
  }
}`, r.out.String())

	require.NoError(t, r.run("", args("q gov param tallying -o json --evm-rpc", stub.url)...))
	require.Contains(t, r.out.String(), `"quorum":"0.500000000000000000"`)
}

func TestQueryPlan(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	plan := upgradePlan{}
	stub.on("currentPlan()", func([]any) ([]any, error) { return []any{plan}, nil })
	require.ErrorContains(t, r.run("", args("q upgrade plan --evm-rpc", stub.url)...), "no upgrade scheduled")

	plan = upgradePlan{Name: "v2.0", Height: 100, Info: "{}"}
	require.NoError(t, r.run("", args("q upgrade plan -o json --evm-rpc", stub.url)...))
	require.JSONEq(t, `{"name": "v2.0", "time": "0001-01-01T00:00:00Z", "height": "100", "info": "{}"}`, r.out.String())
}

func TestQueryAppliedPrintsTheBlockHeader(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	applied := uint64(0)
	stub.on("appliedPlan(string)", func([]any) ([]any, error) { return []any{applied}, nil })
	require.ErrorContains(t, r.run("", args("q upgrade applied v2.0 --evm-rpc", stub.url)...), "no upgrade found")

	applied = 77
	require.NoError(t, r.run("", args("q upgrade applied v2.0 --evm-rpc", stub.url)...))
	require.Contains(t, r.out.String(), `"number": "0x4d"`)
}

func TestQueryBlockPrintsTheHeader(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	stub.head = 120

	require.NoError(t, r.run("", args("q block --evm-rpc", stub.url)...))
	require.Contains(t, r.out.String(), `"number": "0x78"`)

	require.NoError(t, r.run("", args("q block 0 --evm-rpc", stub.url)...))
	require.Contains(t, r.out.String(), `"number": "0x78"`)

	require.NoError(t, r.run("", args("q block 10 --evm-rpc", stub.url)...))
	require.Contains(t, r.out.String(), `"number": "0xa"`)
	// The node's hash is printed, not a hash recomputed from the fields.
	require.Contains(t, r.out.String(), `"hash": "`+stubBlockHash.Hex()+`"`)
	require.NotContains(t, r.out.String(), `"transactions"`)

	require.EqualError(t, r.run("", args("q block 121 --evm-rpc", stub.url)...), "block 121 not found")
	require.ErrorContains(t, r.run("", args("q block ten --evm-rpc", stub.url)...), `height "ten"`)
	require.ErrorIs(t, r.run("", args("q block --node tcp://localhost:26657 --evm-rpc", stub.url)...), errBothEndpoints)
}

func TestQuerySubspace(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	values := map[string]*big.Int{
		"tallyparams.quorum":              e18(1, 2),
		"tallyparams.threshold":           e18(1, 2),
		"tallyparams.veto_threshold":      e18(334, 1000),
		"tallyparams.expedited_quorum":    e18(2, 3),
		"tallyparams.expedited_threshold": e18(2, 3),
		"votingparams.voting_period":      big.NewInt(30_000_000_000),
	}
	stub.on("param(string,string)", func(a []any) ([]any, error) {
		assert.Equal(t, "gov", a[0])
		return []any{common.BigToHash(values[a[1].(string)]).Bytes()}, nil
	})
	require.NoError(t, r.run("", args("q params subspace gov tallyparams -o json --evm-rpc", stub.url)...))
	require.JSONEq(t, `{
  "subspace": "gov",
  "key": "tallyparams",
  "value": "{\"expedited_quorum\":\"0.666666666666666666\",\"expedited_threshold\":\"0.666666666666666666\",\"quorum\":\"0.500000000000000000\",\"threshold\":\"0.500000000000000000\",\"veto_threshold\":\"0.334000000000000000\"}"
}`, r.out.String())

	require.NoError(t, r.run("", args("q params subspace gov votingparams.voting_period -o json --evm-rpc", stub.url)...))
	require.JSONEq(t, `{"subspace": "gov", "key": "votingparams.voting_period", "value": "\"30000000000\""}`, r.out.String())

	require.ErrorContains(t, r.run("", args("q params subspace baseapp ABCIParams --evm-rpc", stub.url)...), "not a param of this chain")
}

func TestHeightReadsThatBlock(t *testing.T) {
	r := newTestRoot(t)
	stub := newRPCStub(t)
	stub.on("tallyResult(uint64)", func([]any) ([]any, error) {
		return []any{tallyData{Yes: big.NewInt(1), Abstain: big.NewInt(2), No: big.NewInt(3), NoWithVeto: big.NewInt(4)}}, nil
	})
	require.NoError(t, r.run("", args("q gov tally 1 --height 9 -o json --evm-rpc", stub.url)...))
	require.JSONEq(t, `{"yes": "1", "abstain": "2", "no": "3", "no_with_veto": "4"}`, r.out.String())
	require.Equal(t, "0x9", stub.lastCallBlock)
}
