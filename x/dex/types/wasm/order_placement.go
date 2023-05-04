package wasm

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/x/dex/types"
)

type SudoOrderPlacementMsg struct {
	OrderPlacements OrderPlacementMsgDetails `json:"bulk_order_placements"`
}

type OrderPlacementMsgDetails struct {
	Orders   []types.Order               `json:"orders"`
	Deposits []types.ContractDepositInfo `json:"deposits"`
}

func (m *SudoOrderPlacementMsg) IsEmpty() bool {
	return len(m.OrderPlacements.Deposits) == 0 && len(m.OrderPlacements.Orders) == 0
}

type SudoOrderPlacementResponse struct {
	UnsuccessfulOrders []UnsuccessfulOrder `json:"unsuccessful_orders"`
}

type UnsuccessfulOrder struct {
	ID     uint64 `json:"id"`
	Reason string `json:"reason"`
}

func (r SudoOrderPlacementResponse) String() string {
	return fmt.Sprintf("Unsuccessful orders count: %d", len(r.UnsuccessfulOrders))
}
