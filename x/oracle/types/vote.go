package types

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v2"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// NewAggregateExchangeRateVote creates a AggregateExchangeRateVote instance
func NewAggregateExchangeRateVote(exchangeRateTuples ExchangeRateTuples, voter sdk.ValAddress) AggregateExchangeRateVote {
	return AggregateExchangeRateVote{
		ExchangeRateTuples: exchangeRateTuples,
		Voter:              voter.String(),
	}
}

// String implement stringify
func (v AggregateExchangeRateVote) String() string {
	out, _ := yaml.Marshal(v)
	return string(out)
}

// NewExchangeRateTuple creates a ExchangeRateTuple instance
func NewExchangeRateTuple(denom string, exchangeRate sdk.Dec) ExchangeRateTuple {
	return ExchangeRateTuple{
		denom,
		exchangeRate,
	}
}

// String implement stringify
func (v ExchangeRateTuple) String() string {
	out, _ := yaml.Marshal(v)
	return string(out)
}

// ExchangeRateTuples - array of ExchangeRateTuple
type ExchangeRateTuples []ExchangeRateTuple

// String implements fmt.Stringer interface
func (tuples ExchangeRateTuples) String() string {
	out, _ := yaml.Marshal(tuples)
	return string(out)
}

// ParseExchangeRateTuples ExchangeRateTuple parser
func ParseExchangeRateTuples(tuplesStr string) (ExchangeRateTuples, error) {
	tuplesStr = strings.TrimSpace(tuplesStr)
	if len(tuplesStr) == 0 {
		return nil, nil
	}

	tupleStrs := strings.Split(tuplesStr, ",")
	tuples := make(ExchangeRateTuples, len(tupleStrs))
	duplicateCheckMap := make(map[string]bool)
	for i, tupleStr := range tupleStrs {
		decCoin, err := sdk.ParseDecCoin(tupleStr)
		if err != nil {
			return nil, err
		}

		tuples[i] = ExchangeRateTuple{
			Denom:        decCoin.Denom,
			ExchangeRate: decCoin.Amount,
		}

		if _, ok := duplicateCheckMap[decCoin.Denom]; ok {
			return nil, fmt.Errorf("duplicated denom %s", decCoin.Denom)
		}

		duplicateCheckMap[decCoin.Denom] = true
	}

	return tuples, nil
}

// String implement stringify
func (ex OracleExchangeRate) String() string {
	out, _ := yaml.Marshal(ex)
	return string(out)
}

// OracleExchangeRates - array of OracleExchangeRate
type DenomOracleExchangeRatePairs []DenomOracleExchangeRatePair

// String implements fmt.Stringer interface
func (rates DenomOracleExchangeRatePairs) String() string {
	out, _ := yaml.Marshal(rates)
	return string(out)
}

func NewDenomOracleExchangeRatePair(denom string, exchangeRate sdk.Dec, lastUpdate sdk.Int, lastUpdateTimestamp int64) DenomOracleExchangeRatePair {
	return DenomOracleExchangeRatePair{
		Denom: denom,
		OracleExchangeRate: OracleExchangeRate{
			ExchangeRate:        exchangeRate,
			LastUpdate:          lastUpdate,
			LastUpdateTimestamp: lastUpdateTimestamp,
		},
	}
}

// VotePenaltyCounter - array of VotePenaltyCounter
type VotePenaltyCounters []VotePenaltyCounter
