package bindings

import "github.com/sei-protocol/sei-chain/x/dex/types"

type SeiDexQuery struct {
	// queries the dex TWAPs
	DexTwaps           *types.QueryGetTwapsRequest        `json:"dex_twaps,omitempty"`
	GetOrders          *types.QueryGetOrdersRequest       `json:"get_orders,omitempty"`
	GetOrderByID       *types.QueryGetOrderByIDRequest    `json:"get_order_by_id,omitempty"`
	GetOrderSimulation *types.QueryOrderSimulationRequest `json:"order_simulation,omitempty"`
	GetLatestPrice     *types.QueryGetLatestPriceRequest  `json:"get_latest_price,omitempty"`
}
