package types

import "fmt"

const LimitOrderType string = "Limit"
const MarketOrderType string = "Market"

type SudoOrderPlacementMsg struct {
	OrderPlacements OrderPlacementMsgDetails `json:"bulk_order_placements"`
}

type OrderPlacementMsgDetails struct {
	Orders   []ContractOrderPlacement `json:"orders"`
	Deposits []ContractDepositInfo    `json:"deposits"`
}

type ContractOrderPlacement struct {
	Id                uint64 `json:"id"`
	Account           string `json:"account"`
	PriceDenom        string `json:"price_denom"`
	AssetDenom        string `json:"asset_denom"`
	Price             string `json:"price"`
	Quantity          string `json:"quantity"`
	OrderType         string `json:"order_type"`
	PositionDirection string `json:"position_direction"`
	PositionEffect    string `json:"position_effect"`
	Leverage          string `json:"leverage"`
}

type ContractDepositInfo struct {
	Account string `json:"account"`
	Denom   string `json:"denom"`
	Amount  string `json:"amount"`
}

func GetOrderType(limit bool) string {
	if limit {
		return LimitOrderType
	}
	return MarketOrderType
}

type SudoOrderPlacementResponse struct {
	UnsuccessfulOrderIds []uint64 `json:"unsuccessful_order_ids"`
}

func (r SudoOrderPlacementResponse) String() string {
	return fmt.Sprintf("Unsuccessful IDs count: %d", len(r.UnsuccessfulOrderIds))
}
