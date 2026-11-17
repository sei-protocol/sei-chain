package staking

import (
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

var (
	errValidatorExists   = errors.New("validator already exists for this operator")
	errPubkeyExists      = errors.New("validator already exists for this consensus key")
	errInvalidGenesis    = errors.New("invalid genesis validator")
	errStakingNotWritten = errors.New("staking's params must be written before its validators")
)

// DefaultParams returns x/staking's default params as Sei sets them.
func DefaultParams() chainparams.Staking {
	return chainparams.Staking{
		UnbondingTime:                      uint64(21 * 24 * time.Hour),
		MaxValidators:                      35,
		MaxEntries:                         7,
		MaxVotingPowerRatio:                big.NewInt(200_000_000_000_000_000),
		MaxVotingPowerEnforcementThreshold: big.NewInt(1_000_000),
	}
}

// SetParams writes p as staking's params, for genesis.
func SetParams(state precompiles.State, p chainparams.Staking) error {
	return chainparams.WriteStaking(state, p)
}

// HasParams reports whether staking's params have been written.
func HasParams(state precompiles.State) bool {
	return chainparams.HasStaking(state)
}

// GenesisValidator is a validator staking starts with. Amounts are wei, whole
// usei.
type GenesisValidator struct {
	Operator          common.Address
	ConsensusPubkey   common.Hash // ed25519
	SelfDelegation    *big.Int
	MinSelfDelegation *big.Int
}

// AddGenesisValidator creates g's validator unbonded and delegates its
// self-delegation from the operator's balance, as a genesis transaction's
// MsgCreateValidator does. The next EndBlock bonds it if it ranks among the
// first MaxValidators. It fails when staking's params are not written, or the
// operator or the consensus key already has a validator.
func (p Precompile) AddGenesisValidator(block precompiles.BlockContext, state precompiles.State, g GenesisValidator) error {
	if !chainparams.HasStaking(state) {
		return errStakingNotWritten
	}
	if err := g.check(); err != nil {
		return err
	}
	s := newStore(state)
	if _, ok := s.validator(g.Operator); ok {
		return fmt.Errorf("%w: %s", errValidatorExists, g.Operator)
	}
	if s.validatorByPubkey(g.ConsensusPubkey) != (common.Address{}) {
		return fmt.Errorf("%w: %s", errPubkeyExists, g.ConsensusPubkey)
	}

	v := validator{
		consensusPubkey:   g.ConsensusPubkey,
		status:            statusUnbonded,
		tokens:            new(big.Int),
		delegatorShares:   new(big.Int),
		minSelfDelegation: g.MinSelfDelegation,
	}
	s.setValidator(g.Operator, v)
	s.validatorSet().Add(solstorage.AddressKey(g.Operator))
	s.setValidatorByPubkey(g.ConsensusPubkey, g.Operator)
	s.powerIndex().insert(g.Operator, new(big.Int))
	if err := p.hooks().AfterValidatorCreated(block, state, g.Operator); err != nil {
		return err
	}
	_, err := p.delegate(block, s, s.params(), g.Operator, g.Operator, &v, g.SelfDelegation, statusUnbonded, g.Operator)
	return err
}

// check requires an operator, a consensus key, a positive minimum
// self-delegation, and a self-delegation of whole usei, at least that minimum,
// worth at least one unit of consensus power.
func (g GenesisValidator) check() error {
	switch {
	case g.Operator == (common.Address{}):
		return fmt.Errorf("%w: no operator", errInvalidGenesis)
	case g.ConsensusPubkey == (common.Hash{}):
		return fmt.Errorf("%w: no consensus key", errInvalidGenesis)
	case g.MinSelfDelegation == nil || g.MinSelfDelegation.Sign() <= 0:
		return fmt.Errorf("%w: minimum self-delegation must be positive", errInvalidGenesis)
	}
	if err := checkAmount(g.SelfDelegation); err != nil {
		return fmt.Errorf("%w: self-delegation: %w", errInvalidGenesis, err)
	}
	if g.SelfDelegation.Cmp(g.MinSelfDelegation) < 0 {
		return fmt.Errorf("%w: self-delegation below its minimum", errInvalidGenesis)
	}
	// Run takes calls only once an EndBlock has bonded a set with power, and
	// genesis validators are the only way into the first one.
	if consensusPower(g.SelfDelegation).Sign() == 0 {
		return fmt.Errorf("%w: self-delegation worth no consensus power", errInvalidGenesis)
	}
	return nil
}
