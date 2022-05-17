package types

import "fmt"

type SudoLiquidationMsg struct {
	Liquidation ContractLiquidationDetails `json:"liquidation"`
}

type ContractLiquidationDetails struct {
	Requests []LiquidationRequest `json:"requests"`
}

type LiquidationRequest struct {
	Requestor string `json:"requestor"`
	Account   string `json:"account"`
}

type SudoLiquidationResponse struct {
	SuccessfulAccounts []string           `json:"successful_accounts"`
	LiquidationOrders  []LiquidationOrder `json:"liquidation_orders"`
}

type LiquidationOrder struct {
	Account    string `json:"account"`
	PriceDenom string `json:"price_denom"`
	AssetDenom string `json:"asset_denom"`
	Quantity   string `json:"quantity"`
	Long       bool   `json:"long"`
	Leverage   string `json:"leverage"`
}

func (r SudoLiquidationResponse) String() string {
	return fmt.Sprintf("Successful accounts count: %d", len(r.SuccessfulAccounts))
}
