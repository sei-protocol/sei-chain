package gov

import (
	"math"
	"math/big"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

func TestParseDec(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Dec
		err  bool
	}{
		{in: "0", want: 0},
		{in: "1", want: DecOne},
		{in: "0.5", want: DecOne / 2},
		{in: "0.334", want: 334_000_000_000_000_000},
		{in: "0.000000000000000001", want: 1},
		{in: "1.000000000000000000", want: DecOne},
		{in: "18.446744073709551615", want: math.MaxUint64},
		{in: "18.446744073709551616", err: true},
		{in: "0.0000000000000000001", err: true},
		{in: "", err: true},
		{in: ".5", err: true},
		{in: "-0.5", err: true},
		{in: "+0.5", err: true},
		{in: "0.-5", err: true},
		{in: "0.5x", err: true},
		{in: "1e18", err: true},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseDec(tc.in)
			if tc.err {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			reparsed, err := ParseDec(got.String())
			require.NoError(t, err)
			require.Equal(t, got, reparsed)
		})
	}
}

func TestDecStringMatchesSDKDec(t *testing.T) {
	for _, s := range []string{"0", "0.334", "0.5", "1", "0.000000000000000001", "0.999999999999999999"} {
		d, err := ParseDec(s)
		require.NoError(t, err)
		require.Equal(t, sdk.MustNewDecFromStr(s).String(), d.String())
	}
}

func TestDecTextRoundTrip(t *testing.T) {
	var d Dec
	require.NoError(t, d.UnmarshalText([]byte("0.25")))
	require.Equal(t, DecOne/4, d)
	text, err := d.MarshalText()
	require.NoError(t, err)
	require.Equal(t, "0.250000000000000000", string(text))
	require.Error(t, d.UnmarshalText([]byte("nope")))
}

func TestParamsValidate(t *testing.T) {
	require.NoError(t, DefaultParams().Validate())
	for name, mutate := range map[string]func(*Params){
		"zero voting period":  func(p *Params) { p.VotingPeriod = 0 },
		"zero quorum":         func(p *Params) { p.Quorum = 0 },
		"quorum above one":    func(p *Params) { p.Quorum = DecOne + 1 },
		"zero threshold":      func(p *Params) { p.Threshold = 0 },
		"threshold above one": func(p *Params) { p.Threshold = DecOne + 1 },
		"zero veto threshold": func(p *Params) { p.VetoThreshold = 0 },
		"veto above one":      func(p *Params) { p.VetoThreshold = DecOne + 1 },
	} {
		t.Run(name, func(t *testing.T) {
			p := DefaultParams()
			mutate(&p)
			require.Error(t, p.Validate())
		})
	}
	p := DefaultParams()
	p.Quorum, p.Threshold, p.VetoThreshold = DecOne, DecOne, DecOne
	require.NoError(t, p.Validate())
}

func TestGenesisValidate(t *testing.T) {
	a, b := common.HexToAddress("0xa"), common.HexToAddress("0xb")
	valid := Genesis{Voters: []Voter{{Address: a, Weight: 1}, {Address: b, Weight: 2}}, Params: DefaultParams()}
	require.NoError(t, valid.Validate())

	tooMany := Genesis{Params: DefaultParams()}
	for i := range MaxVoters + 1 {
		tooMany.Voters = append(tooMany.Voters, Voter{Address: common.BigToAddress(big.NewInt(int64(i + 1))), Weight: 1})
	}
	for name, g := range map[string]Genesis{
		"no voters":       {Params: DefaultParams()},
		"too many voters": tooMany,
		"zero address":    {Voters: []Voter{{Weight: 1}}, Params: DefaultParams()},
		"duplicate":       {Voters: []Voter{{Address: a, Weight: 1}, {Address: a, Weight: 1}}, Params: DefaultParams()},
		"zero weight":     {Voters: []Voter{{Address: a}}, Params: DefaultParams()},
		"overflow":        {Voters: []Voter{{Address: a, Weight: math.MaxUint64}, {Address: b, Weight: 1}}, Params: DefaultParams()},
		"bad params":      {Voters: []Voter{{Address: a, Weight: 1}}},
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, g.Validate())
			_, err := New(g)
			require.Error(t, err)
		})
	}
	maxWeights := Genesis{Voters: []Voter{{Address: a, Weight: math.MaxUint64 - 1}, {Address: b, Weight: 1}}, Params: DefaultParams()}
	require.NoError(t, maxWeights.Validate())
}

func TestPasses(t *testing.T) {
	p := DefaultParams() // quorum 0.334, threshold 0.5, veto 0.334
	for _, tc := range []struct {
		name  string
		tally Tally
		total uint64
		want  bool
	}{
		{name: "no votes", tally: Tally{}, total: 100},
		{name: "no weight", tally: Tally{Yes: 1}, total: 0},
		{name: "below quorum", tally: Tally{Yes: 33}, total: 100},
		{name: "exactly quorum", tally: Tally{Yes: 334}, total: 1000, want: true},
		{name: "just below quorum", tally: Tally{Yes: 333}, total: 1000},
		{name: "all abstain", tally: Tally{Abstain: 100}, total: 100},
		{name: "abstain counts toward quorum", tally: Tally{Yes: 1, Abstain: 40}, total: 100, want: true},
		{name: "exactly half yes fails", tally: Tally{Yes: 50, No: 50}, total: 100},
		{name: "majority yes", tally: Tally{Yes: 51, No: 49}, total: 100, want: true},
		{name: "veto above threshold", tally: Tally{Yes: 60, NoWithVeto: 40}, total: 100},
		{name: "veto exactly at threshold", tally: Tally{Yes: 666, NoWithVeto: 334}, total: 1000, want: true},
		{name: "veto share includes abstain", tally: Tally{Yes: 40, Abstain: 30, NoWithVeto: 30}, total: 100, want: true},
		{name: "unanimous", tally: Tally{Yes: 100}, total: 100, want: true},
		{name: "max weights", tally: Tally{Yes: math.MaxUint64}, total: math.MaxUint64, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, p.Passes(tc.tally, tc.total))
			require.Equal(t, tc.want, cosmosPasses(p, tc.tally, tc.total))
		})
	}
}

// cosmosPasses is x/gov's TallyLegacy decision, in sdk.Dec.
func cosmosPasses(p Params, t Tally, total uint64) bool {
	dec := func(v uint64) sdk.Dec { return sdk.NewDecFromInt(sdk.NewIntFromUint64(v)) }
	param := func(d Dec) sdk.Dec { return sdk.MustNewDecFromStr(d.String()) }
	if total == 0 {
		return false
	}
	totalVotingPower := dec(t.Yes).Add(dec(t.Abstain)).Add(dec(t.No)).Add(dec(t.NoWithVeto))
	if totalVotingPower.Quo(dec(total)).LT(param(p.Quorum)) {
		return false
	}
	if totalVotingPower.Sub(dec(t.Abstain)).Equal(sdk.ZeroDec()) {
		return false
	}
	if dec(t.NoWithVeto).Quo(totalVotingPower).GT(param(p.VetoThreshold)) {
		return false
	}
	return dec(t.Yes).Quo(totalVotingPower.Sub(dec(t.Abstain))).GT(param(p.Threshold))
}

func TestPassesMatchesCosmosTally(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	fraction := func() Dec { return Dec(rng.Uint64N(1000)+1) * (DecOne / 1000) }
	for range 20_000 {
		p := Params{VotingPeriod: 1, Quorum: fraction(), Threshold: fraction(), VetoThreshold: fraction()}
		var tally Tally
		budget := rng.Uint64N(1000) + 1
		total := budget
		for _, option := range []*uint64{&tally.Yes, &tally.Abstain, &tally.No, &tally.NoWithVeto} {
			if rng.IntN(3) == 0 || budget == 0 {
				continue
			}
			*option = rng.Uint64N(budget + 1)
			budget -= *option
		}
		require.Equal(t, cosmosPasses(p, tally, total), p.Passes(tally, total), "params %+v tally %+v total %d", p, tally, total)
	}
}

type mapState map[common.Address]map[common.Hash]common.Hash

func (m mapState) GetState(addr common.Address, key common.Hash) common.Hash { return m[addr][key] }

func (m mapState) SetState(addr common.Address, key, value common.Hash) {
	if m[addr] == nil {
		m[addr] = map[common.Hash]common.Hash{}
	}
	if value == (common.Hash{}) {
		delete(m[addr], key)
		return
	}
	m[addr][key] = value
}

func TestStringStorage(t *testing.T) {
	state := mapState{}
	w := newWriter(Address, state)
	slot := storageKey("test")
	for _, v := range []string{
		"",
		"a",
		strings.Repeat("x", 32),
		strings.Repeat("y", 33),
		strings.Repeat("z", 1000),
		"short again",
		"\x00\x00trailing zero bytes\x00\x00",
		"",
	} {
		w.setStr(slot, v)
		require.Equal(t, v, w.str(slot))
		// Everything but the live string's slots is cleared.
		require.Len(t, state[Address], int(nonZeroSlots(v)))
	}
}

// nonZeroSlots is how many non-zero slots setStr leaves for v.
func nonZeroSlots(v string) uint64 {
	if v == "" {
		return 0
	}
	n := uint64(1)
	for i := 0; i < len(v); i += common.HashLength {
		if strings.Trim(v[i:min(i+common.HashLength, len(v))], "\x00") != "" {
			n++
		}
	}
	return n
}

func TestStorageSlotsAreDistinct(t *testing.T) {
	seen := map[common.Hash]string{}
	add := func(name string, slot common.Hash) {
		t.Helper()
		prev, ok := seen[slot]
		require.False(t, ok, "%s collides with %s", name, prev)
		seen[slot] = name
	}
	add("proposal count", slotProposalCount)
	add("queue head", slotQueueHead)
	add("queue tail", slotQueueTail)
	for field := fieldKind; field <= fieldPlanInfo; field++ {
		for id := range uint64(4) {
			add("proposal field", proposalSlot(id, field))
		}
	}
	for field := planFieldHeight; field <= planFieldProposal; field++ {
		add("plan field", planSlot(field))
	}
	for index := range uint64(4) {
		add("queue entry", queueSlot(index))
		add("vote", voteSlot(index, common.Address{0x01}))
		add("vote", voteSlot(index, common.Address{0x02}))
		add("chunk", stringChunkSlot(planSlot(planFieldName), index))
	}
}

func TestStoredValuesUseTheLowBytes(t *testing.T) {
	state := mapState{}
	w := newWriter(Address, state)
	slot := storageKey("test")
	w.setU64(slot, math.MaxUint64)
	require.Equal(t, common.BigToHash(new(big.Int).SetUint64(math.MaxUint64)), state.GetState(Address, slot))
	addr := common.HexToAddress("0x00112233445566778899aabbccddeeff00112233")
	w.setAddress(slot, addr)
	require.Equal(t, common.BytesToHash(addr[:]), state.GetState(Address, slot))
	require.Equal(t, addr, w.address(slot))
}
