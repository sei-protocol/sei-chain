package simulation_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tendermint/tendermint/crypto/ed25519"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/kv"

	"github.com/sei-protocol/sei-chain/x/oracle/keeper"
	sim "github.com/sei-protocol/sei-chain/x/oracle/simulation"
	"github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/sei-protocol/sei-chain/x/oracle/utils"
)

var (
	delPk      = ed25519.GenPrivKey().PubKey()
	feederAddr = sdk.AccAddress(delPk.Address())
	valAddr    = sdk.ValAddress(delPk.Address())
)

func TestDecodeDistributionStore(t *testing.T) {
	cdc := keeper.MakeTestCodec(t)
	dec := sim.NewDecodeStore(cdc)

	exchangeRate := sdk.NewDecWithPrec(1234, 1)
	missCounter := uint64(23)
	abstainCounter := uint64(21)

	aggregateVote := types.NewAggregateExchangeRateVote(types.ExchangeRateTuples{
		{Denom: utils.MicroAtomDenom, ExchangeRate: sdk.NewDecWithPrec(1234, 1)},
	}, valAddr)
	votePenaltyCounter := types.VotePenaltyCounter{MissCount: missCounter, AbstainCount: abstainCounter}

	denom := "usei"

	kvPairs := kv.Pairs{
		Pairs: []kv.Pair{
			{Key: types.ExchangeRateKey, Value: cdc.MustMarshal(&sdk.DecProto{Dec: exchangeRate})},
			{Key: types.FeederDelegationKey, Value: feederAddr.Bytes()},
			{Key: types.VotePenaltyCounterKey, Value: cdc.MustMarshal(&votePenaltyCounter)},
			{Key: types.AggregateExchangeRateVoteKey, Value: cdc.MustMarshal(&aggregateVote)},
			{Key: types.VoteTargetKey, Value: cdc.MustMarshal(&types.Denom{Name: denom})},
			{Key: []byte{0x99}, Value: []byte{0x99}},
		},
	}

	tests := []struct {
		name        string
		expectedLog string
	}{
		{"ExchangeRate", fmt.Sprintf("%v\n%v", exchangeRate, exchangeRate)},
		{"FeederDelegation", fmt.Sprintf("%v\n%v", feederAddr, feederAddr)},
		{"VotePenaltyCounter", fmt.Sprintf("%v\n%v", votePenaltyCounter, votePenaltyCounter)},
		{"AggregateVote", fmt.Sprintf("%v\n%v", aggregateVote, aggregateVote)},
		{"VoteTarget", fmt.Sprintf("name: %v\n\nname: %v\n", denom, denom)},
		{"other", ""},
	}

	for i, tt := range tests {
		i, tt := i, tt
		t.Run(tt.name, func(t *testing.T) {
			switch i {
			case len(tests) - 1:
				require.Panics(t, func() { dec(kvPairs.Pairs[i], kvPairs.Pairs[i]) }, tt.name)
			default:
				require.Equal(t, tt.expectedLog, dec(kvPairs.Pairs[i], kvPairs.Pairs[i]), tt.name)
			}
		})
	}
}
