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

	deduct := func(validator common.Address, shares *big.Int) {
		s.setTallyDeduction(id, validator, new(big.Int).Add(s.tallyDeduction(id, validator), shares))
	}
	voters := s.votersByProposal(id)
	for n := voters.Len(); n > 0; n-- {
		if !take(budget, 1) {
			return false
		}
		member := voters.At(n - 1)
		voter := solstorage.MemberAddress(member)
		creditVoter(state, electorate, s, id, voter, snap, &t, deduct)
		if _, ok := snap.byOperator[voter]; !ok {
			s.setVote(id, voter, nil)
		}
		voters.Remove(member)
	}
	return true
}

// finishTally adds each bonded validator's own vote, weighted by its delegator
// shares less those whose delegators voted, then clears the tally and decides
// the outcome as x/gov's tally does.
func finishTally(state precompiles.State, electorate Electorate, s store, id uint64, prop proposal, params Params, snap electorateSnapshot) tallyOutcome {
	t := s.tally(id)
	creditValidators(s, id, snap, &t, func(validator common.Address) *big.Int { return s.tallyDeduction(id, validator) })
	for _, v := range snap.validators {
		if len(s.vote(id, v.Operator)) > 0 {
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

// liveTally returns proposal id's tally as if its voting ended now, without
// writing state, as x/gov's TallyResult query does for a proposal in voting.
func liveTally(state precompiles.State, electorate Electorate, s store, id uint64) tallyOutcome {
	snap := snapshotElectorate(state, electorate)
	t := s.tally(id)
	deductions := map[common.Address]*big.Int{}
	deducted := func(validator common.Address) *big.Int {
		if d, ok := deductions[validator]; ok {
			return d
		}
		return s.tallyDeduction(id, validator)
	}
	voters := s.votersByProposal(id)
	for i, n := uint64(0), voters.Len(); i < n && !state.Failed(); i++ {
		creditVoter(state, electorate, s, id, solstorage.MemberAddress(voters.At(i)), snap, &t, func(validator common.Address, shares *big.Int) {
			deductions[validator] = new(big.Int).Add(deducted(validator), shares)
		})
	}
	creditValidators(s, id, snap, &t, deducted)
	return tallyOutcome{results: t.results}
}

// creditVoter adds voter's vote on proposal id to t through each of its
// delegations to a bonded validator, passing deduct the shares it takes from
// that validator's own vote.
func creditVoter(state precompiles.State, electorate Electorate, s store, id uint64, voter common.Address, snap electorateSnapshot, t *tallyState, deduct func(common.Address, *big.Int)) {
	if electorate == nil {
		return
	}
	vote := s.vote(id, voter)
	for _, d := range electorate.Delegations(state, voter) {
		v, ok := snap.byOperator[d.Validator]
		if !ok {
			continue
		}
		deduct(v.Operator, d.Shares)
		t.add(vote, votingPower(d.Shares, v))
	}
}

// creditValidators adds each bonded validator's own vote on proposal id to t,
// weighted by its delegator shares less deducted(validator).
func creditValidators(s store, id uint64, snap electorateSnapshot, t *tallyState, deducted func(common.Address) *big.Int) {
	for _, v := range snap.validators {
		if vote := s.vote(id, v.Operator); len(vote) > 0 {
			t.add(vote, votingPower(new(big.Int).Sub(v.DelegatorShares, deducted(v.Operator)), v))
		}
	}
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

// recordTally sets p's tally to o's voting power in whole tokens.
func (p *proposal) recordTally(o tallyOutcome) {
	p.tallyYes = o.tokens(OptionYes)
	p.tallyAbstain = o.tokens(OptionAbstain)
	p.tallyNo = o.tokens(OptionNo)
	p.tallyNoWithVeto = o.tokens(OptionNoWithVeto)
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
