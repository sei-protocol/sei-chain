package types

import sdk "github.com/cosmos/cosmos-sdk/types"

type SudoOrderCancellationMsg struct {
	OrderCancellations OrderCancellationMsgDetails `json:"bulk_order_cancellations"`
}

type OrderCancellationMsgDetails struct {
	OrderCancellations []ContractOrderCancellation `json:"cancellations"`
}

type ContractOrderCancellation struct {
	Account           string  `json:"account"`
	PriceDenom        string  `json:"price_denom"`
	AssetDenom        string  `json:"asset_denom"`
	Price             sdk.Dec `json:"price"`
	Quantity          sdk.Dec `json:"quantity"`
	PositionDirection string  `json:"position_direction"`
	PositionEffect    string  `json:"position_effect"`
	Leverage          sdk.Dec `json:"leverage"`
}
