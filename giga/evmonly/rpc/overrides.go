package rpc

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/export"

	gigaconfig "github.com/sei-protocol/sei-chain/giga/config"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

var (
	errStateOverrideTooManyAccounts = errors.New("state override has too many accounts")
	errStateOverrideTooManySlots    = errors.New("state override has too many slots")
)

// overrideLimits caps the size of a call's state override. A zero limit is unbounded.
type overrideLimits struct {
	accounts int
	slots    int
}

func newOverrideLimits(limits gigaconfig.RPCConfig) overrideLimits {
	return overrideLimits{accounts: limits.MaxStateOverrideAccounts, slots: limits.MaxStateOverrideSlots}
}

// callOverrides returns the overrides a call's optional state and block override arguments name,
// refusing a state override larger than l allows.
func (l overrideLimits) callOverrides(state *export.StateOverride, block *export.BlockOverrides) (evmonly.CallOverrides, error) {
	var overrides evmonly.CallOverrides
	if err := l.check(state); err != nil {
		return overrides, err
	}
	if state != nil {
		overrides.State = *state
	}
	if block != nil {
		overrides.Block = utils.Some(*block)
	}
	return overrides, nil
}

// check refuses a state override naming more accounts, or setting more slots of one account, than
// l allows.
func (l overrideLimits) check(state *export.StateOverride) error {
	if state == nil {
		return nil
	}
	if l.accounts > 0 && len(*state) > l.accounts {
		return fmt.Errorf("%w (%d > %d)", errStateOverrideTooManyAccounts, len(*state), l.accounts)
	}
	if l.slots <= 0 {
		return nil
	}
	for address, account := range *state {
		if slots := max(len(account.State), len(account.StateDiff)); slots > l.slots {
			return fmt.Errorf("%w for %s (%d > %d)", errStateOverrideTooManySlots, address, slots, l.slots)
		}
	}
	return nil
}
