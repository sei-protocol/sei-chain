// Package statesync defines the governance parameter that sets the trust period
// nodes state sync with.
//
// The value is held in a dedicated x/params subspace and is editable via the
// standard ParameterChangeProposal gov flow. Nothing in the app reads it: a node
// that state syncs over RPC reads it from its RPC servers' params store, at the
// key sei-tendermint's config.StateSyncTrustPeriodQueryKey names.
package statesync

import (
	"fmt"
	"time"

	paramtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
)

// SubspaceName is the x/params subspace that holds the state sync trust period.
const SubspaceName = "statesync"

// KeyTrustPeriod is the param key for how long after its time a header can still
// serve as the trusted root a state syncing node verifies light blocks from. It must
// stay below the unbonding time, so that the validators who signed the root are
// still slashable.
var KeyTrustPeriod = []byte("TrustPeriod")

// ParamKeyTable returns the key table for the statesync subspace.
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable(
		paramtypes.NewParamSetPair(KeyTrustPeriod, new(time.Duration), validateTrustPeriod),
	)
}

// validateTrustPeriod type-checks the trust period and requires it to be positive.
func validateTrustPeriod(i interface{}) error {
	v, ok := i.(time.Duration)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v <= 0 {
		return fmt.Errorf("state sync trust period must be positive, got %s", v)
	}
	return nil
}
