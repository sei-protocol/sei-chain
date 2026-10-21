package gov

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

// tallyOutcome is a finished tally: whether the proposal passes, whether its
// deposits burn, and the 1e18-scaled voting power behind each option.
type tallyOutcome struct {
	passes       bool
	burnDeposits bool
	results      map[int32]*big.Int
}

// electorateSnapshot is the bonded validators a tally weighs votes against.
type electorateSnapshot struct {
	validators []Validator
	byOperator map[common.Address]Validator
}

// snapshotElectorate reads electorate's bonded validators with delegator shares.
//
// TODO(giga staking): a tally can span blocks, so read the electorate as of
// the voting end time once staking can change it.
func snapshotElectorate(state precompiles.State, electorate Electorate) electorateSnapshot {
	snap := electorateSnapshot{byOperator: map[common.Address]Validator{}}
	if electorate == nil {
		return snap
	}
	for _, v := range electorate.BondedValidators(state) {
		if v.DelegatorShares.Sign() > 0 {
			snap.validators = append(snap.validators, v)
			snap.byOperator[v.Operator] = v
		}
	}
	return snap
}

// countVotes adds proposal id's votes to its tally while budget covers one
// unit per voter, as x/gov's incremental tally does. It removes each vote it
// counts, except a bonded validator's own vote, which finishTally reads. It
// reports whether every vote is counted.
func countVotes(state precompiles.State, electorate Electorate, s store, id uint64, snap electorateSnapshot, budget *uint64) bool {
	t := s.tally(id)
	t.counting = true
	defer s.setTally(id, t)

	voters := s.votersByProposal(id)
	for n := voters.Len(); n > 0; n-- {
		if !take(budget, 1) {
			return false
		}
		member := voters.At(n - 1)
		voter := solstorage.MemberAddress(member)
		vote := s.vote(id, voter)
		if electorate != nil {
			for _, d := range electorate.Delegations(state, voter) {
				v, ok := snap.byOperator[d.Validator]
				if !ok {
					continue
				}
				s.setTallyDeduction(id, v.Operator, new(big.Int).Add(s.tallyDeduction(id, v.Operator), d.Shares))
				t.add(vote, votingPower(d.Shares, v))
			}
		}
		if _, ok := snap.byOperator[voter]; !ok {
			s.setVote(id, voter, nil)
		}
		voters.Remove(member)
		s.proposalsVotedBy(voter).Remove(solstorage.Slot(id))
	}
	return true
}

// finishTally adds each bonded validator's own vote, weighted by its delegator
// shares less those whose delegators voted, then clears the tally and decides
// the outcome as x/gov's tally does.
func finishTally(state precompiles.State, electorate Electorate, s store, id uint64, prop proposal, params Params, snap electorateSnapshot) tallyOutcome {
	t := s.tally(id)
	for _, v := range snap.validators {
		if vote := s.vote(id, v.Operator); len(vote) > 0 {
			t.add(vote, votingPower(new(big.Int).Sub(v.DelegatorShares, s.tallyDeduction(id, v.Operator)), v))
			s.setVote(id, v.Operator, nil)
		}
		s.setTallyDeduction(id, v.Operator, new(big.Int))
	}
	s.setTally(id, tallyState{results: map[int32]*big.Int{
		OptionYes:        new(big.Int),
		OptionAbstain:    new(big.Int),
		OptionNo:         new(big.Int),
		OptionNoWithVeto: new(big.Int),
	}, power: new(big.Int)})

	out := tallyOutcome{results: t.results}
	if electorate == nil {
		return out
	}
	bonded := electorate.TotalBonded(state)
	if bonded.Sign() == 0 {
		return out
	}
	quorum, threshold := params.Quorum, params.Threshold
	if prop.isExpedited {
		quorum, threshold = params.ExpeditedQuorum, params.ExpeditedThreshold
	}
	if quoDec(t.power, new(big.Int).Mul(bonded, decOneInt)).Cmp(new(big.Int).SetUint64(quorum)) < 0 {
		out.burnDeposits = true
		return out
	}
	nonAbstain := new(big.Int).Sub(t.power, t.results[OptionAbstain])
	if nonAbstain.Sign() == 0 {
		return out
	}
	if quoDec(t.results[OptionNoWithVeto], t.power).Cmp(new(big.Int).SetUint64(params.VetoThreshold)) > 0 {
		out.burnDeposits = true
		return out
	}
	out.passes = quoDec(t.results[OptionYes], nonAbstain).Cmp(new(big.Int).SetUint64(threshold)) > 0
	return out
}

// votingPower is shares of v's delegator shares, in 1e18-scaled tokens.
func votingPower(shares *big.Int, v Validator) *big.Int {
	return quoDec(new(big.Int).Mul(shares, v.BondedTokens), v.DelegatorShares)
}

// add credits power to vote's options by their weights.
func (t *tallyState) add(vote []weightedVote, power *big.Int) {
	for _, option := range vote {
		weighted := mulDec(power, new(big.Int).SetUint64(option.weight))
		t.results[option.option].Add(t.results[option.option], weighted)
	}
	t.power.Add(t.power, power)
}

// tokens returns option's voting power, truncated to whole tokens.
func (o tallyOutcome) tokens(option int32) *big.Int {
	return new(big.Int).Quo(o.results[option], decOneInt)
}

var decOneInt = new(big.Int).SetUint64(decOne)

// mulDec multiplies 1e18-scaled decimals as sdk.Dec's Mul does.
func mulDec(a, b *big.Int) *big.Int {
	return roundDec(new(big.Int).Mul(a, b))
}

// quoDec divides 1e18-scaled decimals as sdk.Dec's Quo does.
func quoDec(a, b *big.Int) *big.Int {
	q := new(big.Int).Mul(a, decOneInt)
	q.Mul(q, decOneInt)
	return roundDec(q.Quo(q, b))
}

// roundDec divides non-negative x by 1e18, rounding half to even, as
// sdk.Dec's chopPrecisionAndRound does.
func roundDec(x *big.Int) *big.Int {
	q, r := new(big.Int).QuoRem(x, decOneInt, new(big.Int))
	switch r.Lsh(r, 1).Cmp(decOneInt) {
	case 1:
		q.Add(q, big.NewInt(1))
	case 0:
		if q.Bit(0) == 1 {
			q.Add(q, big.NewInt(1))
		}
	}
	return q
}
