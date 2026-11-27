package distribution

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/staking"
)

var (
	errWithdrawAddrDisabled = errors.New("setting a withdraw address is disabled")
	errBlockedWithdrawAddr  = errors.New("withdraw address may not receive rewards")
)

// blockedWithdrawAddrs are the accounts a withdraw address may not be: the
// escrows, whose balances must match the records they back, and the fee
// collector, whose balance is the epoch's tips.
var blockedWithdrawAddrs = map[common.Address]bool{
	DistributionEscrow:      true,
	FeeCollector:            true,
	staking.BondedEscrow:    true,
	staking.NotBondedEscrow: true,
	staking.StagedEscrow:    true,
}

// setWithdrawAddressCall pays the caller's rewards to args' withdraw address
// from now on, as x/distribution's SetWithdrawAddr does.
func setWithdrawAddressCall(ctx *precompiles.Context, s store, method *abi.Method, args []interface{}) ([]byte, error) {
	payee := args[0].(common.Address)
	if blockedWithdrawAddrs[payee] {
		return nil, fmt.Errorf("%w: %s", errBlockedWithdrawAddr, payee)
	}
	if !s.params().WithdrawAddrEnabled {
		return nil, errWithdrawAddrDisabled
	}
	s.setWithdrawAddress(ctx.Caller, payee)
	if err := emit(ctx, WithdrawAddressSetEvent, []common.Hash{addressTopic(ctx.Caller)}, payee); err != nil {
		return nil, err
	}
	return method.Outputs.Pack(true)
}

// withdrawDelegationRewardsCall pays the rewards the caller's delegation to
// each of validators is owed and starts it afresh, as x/distribution's
// WithdrawDelegationRewards does.
func withdrawDelegationRewardsCall(ctx *precompiles.Context, s store, method *abi.Method, validators []common.Address) ([]byte, error) {
	height := ctx.Block.Number
	for _, val := range validators {
		paid, err := s.withdrawDelegationRewards(height, val, ctx.Caller)
		if err != nil {
			return nil, err
		}
		if err := s.initializeDelegation(height, val, ctx.Caller); err != nil {
			return nil, err
		}
		if err := emit(ctx, DelegationRewardsWithdrawnEvent, []common.Hash{addressTopic(ctx.Caller), addressTopic(val)}, paid); err != nil {
			return nil, err
		}
	}
	return method.Outputs.Pack(true)
}

// withdrawValidatorCommissionCall pays the commission of the caller's
// validator, as x/distribution's WithdrawValidatorCommission does.
func withdrawValidatorCommissionCall(ctx *precompiles.Context, s store, method *abi.Method) ([]byte, error) {
	paid, err := s.withdrawValidatorCommission(ctx.Caller)
	if err != nil {
		return nil, err
	}
	if err := emit(ctx, ValidatorCommissionWithdrawnEvent, []common.Hash{addressTopic(ctx.Caller)}, paid); err != nil {
		return nil, err
	}
	return method.Outputs.Pack(true)
}
