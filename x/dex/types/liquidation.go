package types

import (
	"fmt"
)

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
	SuccessfulAccounts []string `json:"successful_accounts"`
	LiquidationOrders  []Order  `json:"liquidation_orders"`
}

func (r SudoLiquidationResponse) String() string {
	return fmt.Sprintf("Successful accounts count: %d", len(r.SuccessfulAccounts))
}
