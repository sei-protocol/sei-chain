package distribution

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/staking"
)

var _ precompiles.EndBlocker = Precompile{}

// EndBlock sends the block's base fees from FeeCollector to the epoch's DAO
// and, in the last block of an epoch not yet settled, settles it, then fixes
// the DAO address of the epoch after it. Its first EndBlock fixes the DAO
// address of the epoch the block is in and settles from that epoch on. Before
// distribution's params are written it does nothing.
//
// Its failures are base fees that could not be sent, which stay in
// FeeCollector as the epoch's tips; a settlement that could not be made, which
// writes nothing, so the next epoch's settlement pays both; and committee
// members that could not be paid, whose rewards go to the DAO. It never
// returns an error.
func (Precompile) EndBlock(block precompiles.BlockContext, state precompiles.State) ([]error, error) {
	if !chainparams.HasDistribution(state) {
		return nil, nil
	}
	s := newStore(state)
	if s.epochDAO() == (common.Address{}) {
		s.start(block)
	}
	var failures []error
	if err := s.sendBaseFees(block); err != nil {
		failures = append(failures, err)
	}
	if epoch, ok := block.ClosingEpoch.Get(); ok && epoch >= s.nextSettlementEpoch() {
		failures = append(failures, s.settleReporting(block, epoch)...)
		s.setEpochDAO(s.params().DAOAddress)
	}
	return failures, nil
}

// start fixes the params' DAO address for the epoch block is in, and
// settlement to start with that epoch.
func (s store) start(block precompiles.BlockContext) {
	s.setEpochDAO(s.params().DAOAddress)
	epoch, ok := block.Epoch.Get()
	if !ok {
		epoch = block.ClosingEpoch.Or(0)
	}
	s.setNextSettlementEpoch(epoch)
}

// sendBaseFees sends block's base fees, its gas used times its base fee, from
// FeeCollector to the epoch's DAO.
func (s store) sendBaseFees(block precompiles.BlockContext) error {
	if block.BaseFee == nil {
		return nil
	}
	fees := new(big.Int).Mul(new(big.Int).SetUint64(block.GasUsed), block.BaseFee)
	if err := s.transfer(FeeCollector, s.epochDAO(), fees); err != nil {
		return fmt.Errorf("block %d's base fees stay in the fee collector: %w", block.Number, err)
	}
	return nil
}

// settleReporting settles epoch as settle does, writing nothing and reporting
// why when it fails, and marks it settled when it succeeds.
func (s store) settleReporting(block precompiles.BlockContext, epoch uint64) []error {
	snap := s.State.Snapshot()
	unpaid, err := s.settle(block)
	if err != nil {
		s.State.RevertToSnapshot(snap)
		return []error{fmt.Errorf("%w: epoch %d not settled: %w", precompiles.ErrDegraded, epoch, err)}
	}
	s.setNextSettlementEpoch(epoch + 1)
	return unpaid
}

// settle pays the epoch closing at block as SIP-7 settles it: its tips and
// mint to the bonded set's validators by power, and the rest to the DAO. It
// returns the members it could not credit, whose shares go to the DAO.
func (s store) settle(block precompiles.BlockContext) ([]error, error) {
	// The base fees have left FeeCollector, so what it holds is the epoch's
	// tips.
	tips := s.State.GetBalance(FeeCollector)
	minted, err := s.mint(block.UnixNano)
	if err != nil {
		return nil, err
	}
	// The bonded set staking stored at the previous epoch's end is the
	// committee this epoch ran under: its snapshot then projected the
	// operations that end applied.
	committee, err := staking.BondedSet(s.State)
	if err != nil {
		return nil, err
	}
	// Half the tips, rounded down, go to the DAO; the other half and the mint
	// are the pool.
	daoTips := new(big.Int).Rsh(tips, 1)
	pool := new(big.Int).Add(minted, tips)
	pool.Sub(pool, daoTips)

	total := new(big.Int)
	for _, m := range committee {
		total.Add(total, new(big.Int).SetUint64(m.Power))
	}
	credited := new(big.Int)
	var unpaid []error
	for _, m := range committee {
		if total.Sign() == 0 {
			break
		}
		// Each share is rounded down once, after multiplying.
		share := new(big.Int).Mul(pool, new(big.Int).SetUint64(m.Power))
		share.Quo(share, total)
		if err := s.credit(m.Operator, share); err != nil {
			unpaid = append(unpaid, fmt.Errorf("%w: %s's share of %s goes to the DAO: %w", precompiles.ErrDegraded, m.Operator, share, err))
			continue
		}
		credited.Add(credited, share)
	}

	// FeeCollector ends empty: the DAO takes its half of the tips and what no
	// share took, rounding included.
	s.State.AddBalance(FeeCollector, minted)
	if err := s.transfer(FeeCollector, DistributionEscrow, credited); err != nil {
		return nil, err
	}
	return unpaid, s.transfer(FeeCollector, s.epochDAO(), s.State.GetBalance(FeeCollector))
}

// credit allocates amount to val at its commission rate.
func (s store) credit(val common.Address, amount *big.Int) error {
	v, err := validatorStake(s, val)
	if err != nil {
		return err
	}
	return s.allocate(val, amount, v.CommissionRate)
}
