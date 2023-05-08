package types

type SudoOrderCancellationMsg struct {
	OrderCancellations OrderCancellationMsgDetails `json:"bulk_order_cancellations"`
}

type OrderCancellationMsgDetails struct {
	IdsToCancel []uint64 `json:"ids"`
}
