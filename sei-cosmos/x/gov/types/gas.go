package types

import sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"

const (
	// DeferredVoteTallyGas is charged when a vote adds a governance tally record.
	DeferredVoteTallyGas sdk.Gas = 10_000
)
