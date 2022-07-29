package types

type SudoSetPricesMsg struct {
	SetPrices SetPrices `json:"set_prices"`
}

type SetPrices struct {
	Prices []SetPrice `json:"prices"`
}

type SetPrice struct {
	Epoch         uint64 `json:"epoch"`
	PriceDenom    string `json:"price_denom"`
	AssetDenom    string `json:"asset_denom"`
	ExchangePrice string `json:"exchange_price"`
	OraclePrice   string `json:"oracle_price"`
}
