package gov

import (
	"encoding/json"
	"math/big"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	"github.com/stretchr/testify/require"

	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

// forgeLayout is `forge inspect GovStorage storage-layout --json`.
type forgeLayout struct {
	Storage []forgeEntry `json:"storage"`
	Types   map[string]struct {
		Members []forgeEntry `json:"members"`
	} `json:"types"`
}

type forgeEntry struct {
	Label  string `json:"label"`
	Offset int    `json:"offset"`
	Slot   string `json:"slot"`
	Type   string `json:"type"`
}

// TestLayoutMatchesForge pins every slot and member offset to the compiler's
// layout of docs/giga-storage/GovStorage.sol. Regenerate testdata with
// `forge inspect GovStorage storage-layout --json` after editing the schema.
func TestLayoutMatchesForge(t *testing.T) {
	bz, err := os.ReadFile("testdata/gov_storage_layout.json")
	require.NoError(t, err)
	var layout forgeLayout
	require.NoError(t, json.Unmarshal(bz, &layout))

	topLevel := map[string]member{
		"lastProposalId":     {slotProposalIDs + lastProposalID.slot, lastProposalID.offset},
		"startingProposalId": {slotProposalIDs + startingProposalID.slot, startingProposalID.offset},
		"proposals":          {slotProposals, 0},
		"votes":              {slotVotes, 0},
		"deposits":           {slotDeposits, 0},
		"depositors":         {slotDepositors, 0},
		"votingQueue":        {slotVotingQueue, 0},
		"depositQueue":       {slotDepositQueue, 0},
		"votersByProposal":   {slotVotersByProposal, 0},
		"tallies":            {slotTallies, 0},
		"tallyDeductions":    {slotTallyDeductions, 0},
	}
	got := map[string]member{}
	for _, e := range layout.Storage {
		got[e.Label] = member{slot: mustUint(t, e.Slot), offset: e.Offset}
	}
	require.Equal(t, topLevel, got)

	structs := map[string]map[string]member{
		"Proposal": {
			"status":          proposalStatus,
			"isExpedited":     proposalIsExpedited,
			"submitTime":      proposalSubmitTime,
			"depositEndTime":  proposalDepositEndTime,
			"votingStartTime": proposalVotingStartTime,
			"votingEndTime":   proposalVotingEndTime,
			"totalDeposit":    proposalTotalDeposit,
			"tallyYes":        proposalTallyYes,
			"tallyAbstain":    proposalTallyAbstain,
			"tallyNo":         proposalTallyNo,
			"tallyNoWithVeto": proposalTallyNoWithVeto,
			"content":         proposalContent,
			"contentType":     proposalContentType,
			"__gap":           {8, 0},
		},
		"Tally": {
			"yes":        tallyYes,
			"abstain":    tallyAbstain,
			"no":         tallyNo,
			"noWithVeto": tallyNoWithVeto,
			"power":      tallyPower,
			"counting":   tallyCounting,
		},
		"ProposalQueue": {
			"keys":      {0, 0},
			"positions": {1, 0},
		},
		"WeightedVote": {
			"option": weightedVoteOption,
			"weight": weightedVoteWeight,
		},
	}
	structName := regexp.MustCompile(`^t_struct\((\w+)\)\d+_storage$`)
	seen := map[string]bool{}
	for typeID, typ := range layout.Types {
		m := structName.FindStringSubmatch(typeID)
		if m == nil || strings.HasSuffix(m[1], "Set") {
			continue
		}
		want, ok := structs[m[1]]
		require.True(t, ok, "struct %s has no Go layout", m[1])
		got := map[string]member{}
		for _, e := range typ.Members {
			got[e.Label] = member{slot: mustUint(t, e.Slot), offset: e.Offset}
		}
		require.Equal(t, want, got, m[1])
		seen[m[1]] = true
	}
	require.Len(t, seen, len(structs))
}

func mustUint(t *testing.T, s string) uint64 {
	v, err := strconv.ParseUint(s, 10, 64)
	require.NoError(t, err)
	return v
}

// TestStoreReadsCompilerWrites runs GovStorageHarness, which writes GovStorage
// with solc's codegen, and reads the result back through store. Rebuild
// testdata with `forge inspect GovStorageHarness deployedBytecode` under solc
// 0.8.37 and the remappings giga-storage/=docs/giga-storage/ and
// giga-precompiles/=giga/evmonly/precompiles/.
func TestStoreReadsCompilerWrites(t *testing.T) {
	code, err := os.ReadFile("testdata/GovStorageHarness.bin-runtime")
	require.NoError(t, err)
	voter := common.HexToAddress("0x00000000000000000000000000000000000000a1")
	other := common.HexToAddress("0x00000000000000000000000000000000000000b2")
	content := []byte("long enough to spill past one storage word")

	seed, err := abiOf(t, "seed(address,address,bytes)").Pack("seed", voter, other, content)
	require.NoError(t, err)
	cfg := &runtime.Config{Origin: voter, GasLimit: 10_000_000}
	_, db, err := runtime.Execute(common.FromHex(strings.TrimSpace(string(code))), seed, cfg)
	require.NoError(t, err)
	// runtime.Execute deploys the code at the zero-padded address of "contract".
	s := store{solstorage.Store{State: stateDBView{db}, Address: common.BytesToAddress([]byte("contract"))}}

	require.Equal(t, uint64(7), s.lastProposalID())
	require.Equal(t, uint64(3), s.startingProposalID())

	prop, ok := s.proposal(5)
	require.True(t, ok)
	require.Equal(t, proposal{
		status:          2,
		isExpedited:     true,
		submitTime:      1_700_000_001,
		depositEndTime:  1_700_000_002,
		votingStartTime: 1_700_000_003,
		votingEndTime:   1_700_000_004,
		totalDeposit:    ether(21),
		tallyYes:        big.NewInt(22),
		tallyAbstain:    big.NewInt(23),
		tallyNo:         big.NewInt(24),
		tallyNoWithVeto: big.NewInt(25),
	}, prop)
	contentType, stored, err := s.proposalContent(5)
	require.NoError(t, err)
	require.Equal(t, uint8(2), contentType)
	require.Equal(t, content, stored)

	require.Equal(t, []weightedVote{
		{option: 1, weight: 700_000_000_000_000_000},
		{option: 4, weight: 300_000_000_000_000_000},
	}, must(s.vote(5, voter)))
	require.Equal(t, ether(31), s.deposit(5, voter))
	require.Equal(t, ether(32), s.deposit(5, other))

	require.Equal(t, []common.Hash{solstorage.AddressKey(other)}, members(s.depositors(5)))
	require.Equal(t, []common.Hash{solstorage.AddressKey(voter)}, members(s.votersByProposal(5)))
	require.Equal(t, uint64(2), s.votingQueue().Len())
	require.True(t, s.votingQueue().contains(5, 1_700_000_004))
	require.True(t, s.votingQueue().contains(6, 1_700_000_004))
	due, end, ok := s.votingQueue().due(1_700_000_004)
	require.True(t, ok)
	require.Equal(t, uint64(5), due)
	require.Equal(t, uint64(1_700_000_004), end)
	require.Equal(t, uint64(1), s.depositQueue().Len())
	require.True(t, s.depositQueue().contains(3, 1_700_000_002))

	require.Equal(t, tallyState{
		results: map[int32]*big.Int{
			OptionYes:        big.NewInt(41),
			OptionAbstain:    big.NewInt(42),
			OptionNo:         big.NewInt(43),
			OptionNoWithVeto: big.NewInt(44),
		},
		power:    big.NewInt(45),
		counting: true,
	}, s.tally(5))
	require.Equal(t, big.NewInt(46), s.tallyDeduction(5, other))
	require.False(t, s.depositors(5).Contains(solstorage.AddressKey(voter)))
}

func members(set solstorage.Set) []common.Hash {
	out := make([]common.Hash, set.Len())
	for i := range out {
		out[i] = set.At(uint64(i))
	}
	return out
}

func ether(n int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(n), big.NewInt(1e18))
}

// stateDBView reads storage from a geth StateDB. Only GetState is supported.
type stateDBView struct {
	db vm.StateDB
}

func (v stateDBView) GetState(a common.Address, k common.Hash) common.Hash {
	return v.db.GetState(a, k)
}

func (v stateDBView) SetState(common.Address, common.Hash, common.Hash) { panic("read-only") }
func (v stateDBView) GetBalance(common.Address) *big.Int                { panic("unsupported") }
func (v stateDBView) AddBalance(common.Address, *big.Int)               { panic("unsupported") }
func (v stateDBView) SubBalance(common.Address, *big.Int) error         { panic("unsupported") }
func (v stateDBView) GetNonce(common.Address) uint64                    { panic("unsupported") }
func (v stateDBView) SetNonce(common.Address, uint64)                   { panic("unsupported") }
func (v stateDBView) GetCode(common.Address) []byte                     { panic("unsupported") }
func (v stateDBView) Failed() bool                                      { return false }
func (v stateDBView) Snapshot() int                                     { panic("unsupported") }
func (v stateDBView) RevertToSnapshot(int)                              { panic("unsupported") }

// TestContentEncodingMatchesSolc checks encodeContent against solc's
// abi.encode of each content struct, run in GovStorageHarness. Each harness
// encoder takes the struct's fields as its arguments.
func TestContentEncodingMatchesSolc(t *testing.T) {
	code, err := os.ReadFile("testdata/GovStorageHarness.bin-runtime")
	require.NoError(t, err)
	bytesOut := abi.Arguments{{Type: abi.Type{T: abi.BytesTy}}}
	solc := func(fn string, c govContent) []byte {
		tuple := contentCodecs[c.contentType()].args[0].Type
		inputs := make(abi.Arguments, len(tuple.TupleElems))
		fields := make([]interface{}, len(tuple.TupleElems))
		for i, e := range tuple.TupleElems {
			inputs[i] = abi.Argument{Name: tuple.TupleRawNames[i], Type: *e}
			fields[i] = reflect.ValueOf(c).Field(i).Interface()
		}
		method := abi.NewMethod(fn, fn, abi.Function, "pure", false, false, inputs, bytesOut)
		args, err := inputs.Pack(fields...)
		require.NoError(t, err, fn)
		out, _, err := runtime.Execute(common.FromHex(strings.TrimSpace(string(code))), append(method.ID, args...), &runtime.Config{GasLimit: 10_000_000})
		require.NoError(t, err, method.Sig)
		values, err := bytesOut.Unpack(out)
		require.NoError(t, err)
		return values[0].([]byte)
	}
	long := strings.Repeat("long enough to spill past a word ", 3)
	for fn, c := range map[string]govContent{
		"textContent": textContent{Title: "t", Description: long, IsExpedited: true},
		"paramChangeContent": paramChangeContent{Title: long, Description: "d", IsExpedited: true, Changes: []chainparams.Change{
			{Subspace: "gov", Key: "votingparams.voting_period", Value: common.BigToHash(big.NewInt(7)).Bytes()},
			{Subspace: "gov", Key: long, Value: []byte(long)},
		}},
	} {
		_, encoded := encodeContent(c)
		require.Equal(t, solc(fn, c), encoded, fn)
	}
}
