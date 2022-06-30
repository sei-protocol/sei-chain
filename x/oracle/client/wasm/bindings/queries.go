package bindings

import "github.com/sei-protocol/sei-chain/x/oracle/types"

type SeiOracleQuery struct {
	// queries the oracle exchange rates
	ExchangeRates *types.QueryExchangeRatesRequest `json:"exchange_rates,omitempty"`
	// queries the oracle TWAPs
	OracleTwaps *types.QueryTwapsRequest `json:"oracle_twaps,omitempty"`
}
