package distribution

import (
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/staking"
)

// The ABI tuples. Field order matches each tuple's component order.
type (
	DelegationReward struct {
		Validator common.Address
		Amount    *big.Int
	}

	Rewards struct {
		Rewards []DelegationReward
		Total   *big.Int
	}

	DistributionParams struct {
		WithdrawAddrEnabled bool
		DaoAddress          common.Address
	}
)

// rewardsQuery returns the rewards each of args' delegator's delegations is
// owed, as x/distribution's DelegationTotalRewards does.
func rewardsQuery(block precompiles.BlockContext, s store, method *abi.Method, args []interface{}) ([]byte, error) {
	delegator := args[0].(common.Address)
	out := Rewards{Rewards: []DelegationReward{}, Total: new(big.Int)}
	for _, val := range staking.DelegatorValidators(s.State, delegator) {
		owed, err := s.pendingRewards(block.Number, val, delegator)
		if err != nil {
			return nil, err
		}
		out.Rewards = append(out.Rewards, DelegationReward{Validator: val, Amount: owed})
		out.Total.Add(out.Total, owed)
	}
	return method.Outputs.Pack(out)
}

// delegationRewardsQuery returns the rewards args' delegation is owed, as
// x/distribution's DelegationRewards does.
func delegationRewardsQuery(block precompiles.BlockContext, s store, method *abi.Method, args []interface{}) ([]byte, error) {
	owed, err := s.pendingRewards(block.Number, args[1].(common.Address), args[0].(common.Address))
	if err != nil {
		return nil, err
	}
	return method.Outputs.Pack(owed)
}

func paramsQuery(s store, method *abi.Method) ([]byte, error) {
	params := s.params()
	return method.Outputs.Pack(DistributionParams{
		WithdrawAddrEnabled: params.WithdrawAddrEnabled,
		DaoAddress:          params.DAOAddress,
	})
}
