package types

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

type SudoOrderPlacementMsg struct {
	OrderPlacements OrderPlacementMsgDetails `json:"bulk_order_placements"`
}

type OrderPlacementMsgDetails struct {
	Orders   []Order               `json:"orders"`
	Deposits []ContractDepositInfo `json:"deposits"`
}

type ContractDepositInfo struct {
	Account string  `json:"account"`
	Denom   string  `json:"denom"`
	Amount  sdk.Dec `json:"amount"`
}

type SudoOrderPlacementResponse struct {
	UnsuccessfulOrderIds []uint64 `json:"unsuccessful_order_ids"`
}

func (r SudoOrderPlacementResponse) String() string {
	return fmt.Sprintf("Unsuccessful IDs count: %d", len(r.UnsuccessfulOrderIds))
}
