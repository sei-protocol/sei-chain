package simulation

import (
	"bytes"
	"fmt"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/kv"

	"github.com/sei-protocol/sei-chain/x/oracle/types"
)

// NewDecodeStore returns a decoder function closure that unmarshals the KVPair's
// Value to the corresponding oracle type.
func NewDecodeStore(cdc codec.Codec) func(kvA, kvB kv.Pair) string {
	return func(kvA, kvB kv.Pair) string {
		switch {
		case bytes.Equal(kvA.Key[:1], types.ExchangeRateKey):
			var exchangeRateA, exchangeRateB sdk.DecProto
			cdc.MustUnmarshal(kvA.Value, &exchangeRateA)
			cdc.MustUnmarshal(kvB.Value, &exchangeRateB)
			return fmt.Sprintf("%v\n%v", exchangeRateA, exchangeRateB)
		case bytes.Equal(kvA.Key[:1], types.FeederDelegationKey):
			return fmt.Sprintf("%v\n%v", sdk.AccAddress(kvA.Value), sdk.AccAddress(kvB.Value))
		case bytes.Equal(kvA.Key[:1], types.VotePenaltyCounterKey):
			var counterA, counterB types.VotePenaltyCounter
			cdc.MustUnmarshal(kvA.Value, &counterA)
			cdc.MustUnmarshal(kvB.Value, &counterB)
			return fmt.Sprintf("%v\n%v", counterA, counterB)
		case bytes.Equal(kvA.Key[:1], types.AggregateExchangeRateVoteKey):
			var voteA, voteB types.AggregateExchangeRateVote
			cdc.MustUnmarshal(kvA.Value, &voteA)
			cdc.MustUnmarshal(kvB.Value, &voteB)
			return fmt.Sprintf("%v\n%v", voteA, voteB)
		case bytes.Equal(kvA.Key[:1], types.VoteTargetKey):
			var voteTargetA, voteTargetB types.Denom
			cdc.MustUnmarshal(kvA.Value, &voteTargetA)
			cdc.MustUnmarshal(kvB.Value, &voteTargetB)
			return fmt.Sprintf("%v\n%v", voteTargetA, voteTargetB)
		default:
			panic(fmt.Sprintf("invalid oracle key prefix %X", kvA.Key[:1]))
		}
	}
}
