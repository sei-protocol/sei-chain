package bindings

import "github.com/sei-protocol/sei-chain/x/oracle/types"

type SeiOracleQuery struct {
	// queries the oracle exchange rates
	ExchangeRates *WasmQueryExchangeRatesRequest `json:"exchange_rates,omitempty"`
}

type WasmQueryExchangeRatesRequest struct{}

// type WasmQueryOracleExchangeRate struct {
// 	ExchangeRate string `json:"exchange_rate,omitempty"`
// 	LastUpdate   string `json:"last_update,omitempty"`
// }

// type WasmQueryDenomOracleExchangeRate struct {
// 	Denom              string                      `json:"denom,omitempty"`
// 	OracleExchangeRate WasmQueryOracleExchangeRate `json:"oracle_exchange_rate,omitempty"`
// }

type WasmQueryExchangeRatesResponse struct {
	DenomOracleExchangeRatePairs types.DenomOracleExchangeRatePairs `json:"denom_oracle_exchange_rate_pairs,omitempty"`
	// DenomOracleExchangeRatePairs []WasmQueryDenomOracleExchangeRate `json:"denom_oracle_exchange_rate_pairs,omitempty"`
}
