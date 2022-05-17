package types

type SudoSetFundingPaymentRateMsg struct {
	SetFundingPaymentRate SetFundingPaymentRate `json:"set_funding_payment_rate"`
}

type SetFundingPaymentRate struct {
	Epoch      uint64 `json:"epoch"`
	AssetDenom string `json:"asset_denom"`
	PriceDiff  string `json:"price_diff"`
	Negative   bool   `json:"negative"`
}
