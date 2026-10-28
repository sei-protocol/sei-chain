package gov

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultParamsAreValid(t *testing.T) {
	require.NoError(t, DefaultParams().Validate())
	require.NoError(t, testParams.Validate())
}

func TestParamsValidateRejects(t *testing.T) {
	for name, edit := range map[string]func(*Params){
		"no voting period":         func(p *Params) { p.VotingPeriod = 0 },
		"huge voting period":       func(p *Params) { p.VotingPeriod = maxPeriod + 1 },
		"expedited not shorter":    func(p *Params) { p.ExpeditedVotingPeriod = p.VotingPeriod },
		"no deposit period":        func(p *Params) { p.MaxDepositPeriod = 0 },
		"sub-second voting period": func(p *Params) { p.VotingPeriod = minPeriod - 1 },
		"seconds as nanoseconds": func(p *Params) {
			p.VotingPeriod, p.ExpeditedVotingPeriod, p.MaxDepositPeriod = 172_800, 86_400, 172_800
		},
		"sub-second expedited":       func(p *Params) { p.ExpeditedVotingPeriod = minPeriod - 1 },
		"missing deposit":            func(p *Params) { p.MinDeposit = nil },
		"zero deposit":               func(p *Params) { p.MinDeposit = new(big.Int) },
		"zero expedited deposit":     func(p *Params) { p.MinExpeditedDeposit = new(big.Int) },
		"wei remainder":              func(p *Params) { p.MinDeposit = big.NewInt(1) },
		"expedited deposit too low":  func(p *Params) { p.MinExpeditedDeposit = new(big.Int) },
		"quorum above one":           func(p *Params) { p.Quorum = decOne + 1 },
		"zero threshold":             func(p *Params) { p.Threshold = 0 },
		"veto above one":             func(p *Params) { p.VetoThreshold = decOne + 1 },
		"zero expedited threshold":   func(p *Params) { p.ExpeditedThreshold = 0 },
		"expedited quorum above one": func(p *Params) { p.ExpeditedQuorum = decOne + 1 },
	} {
		p := DefaultParams()
		edit(&p)
		require.Error(t, p.Validate(), name)
	}
}
