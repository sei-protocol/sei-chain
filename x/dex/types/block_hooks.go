package types

import sdk "github.com/cosmos/cosmos-sdk/types"

type SudoNewBlockMsg struct {
	NewBlock NewBlockRequest `json:"new_block"`
}

type NewBlockRequest struct {
	Epoch int64 `json:"epoch"`
}

type SudoFinalizeBlockMsg struct {
	FinalizeBlock FinalizeBlockRequest `json:"finalize_block"`
}

func NewSudoFinalizeBlockMsg() *SudoFinalizeBlockMsg {
	return &SudoFinalizeBlockMsg{
		FinalizeBlock: FinalizeBlockRequest{
			Results: []ContractOrderResult{},
		},
	}
}

func (m *SudoFinalizeBlockMsg) AddContractResult(result ContractOrderResult) {
	m.FinalizeBlock = FinalizeBlockRequest{
		Results: append(m.FinalizeBlock.Results, result),
	}
}

type FinalizeBlockRequest struct {
	Results []ContractOrderResult `json:"contract_order_results"`
}

type ContractOrderResult struct {
	ContractAddr          string                 `json:"contract_address"`
	OrderPlacementResults []OrderPlacementResult `json:"order_placement_results"`
	OrderExecutionResults []OrderExecutionResult `json:"order_execution_results"`
}

func NewContractOrderResult(contractAddr string) ContractOrderResult {
	return ContractOrderResult{
		ContractAddr:          contractAddr,
		OrderPlacementResults: []OrderPlacementResult{},
		OrderExecutionResults: []OrderExecutionResult{},
	}
}

func PopulateOrderPlacementResults(contractAddr string, orders []Order, resultMap map[string]ContractOrderResult) {
	for _, order := range orders {
		if _, ok := resultMap[order.Account]; !ok {
			resultMap[order.Account] = NewContractOrderResult(contractAddr)
		}
		resultsForAccount := resultMap[order.Account]
		resultsForAccount.OrderPlacementResults = append(resultsForAccount.OrderPlacementResults, OrderPlacementResult{
			OrderID: order.Id,
			Status:  order.Status,
		},
		)
		resultMap[order.Account] = resultsForAccount
	}
}

func PopulateOrderExecutionResults(contractAddr string, settlements []*SettlementEntry, resultMap map[string]ContractOrderResult) {
	for _, settlement := range settlements {
		if _, ok := resultMap[settlement.Account]; !ok {
			resultMap[settlement.Account] = NewContractOrderResult(contractAddr)
		}
		resultsForAccount := resultMap[settlement.Account]
		resultsForAccount.OrderExecutionResults = append(resultsForAccount.OrderExecutionResults,
			OrderExecutionResult{
				OrderID:          settlement.OrderId,
				ExecutionPrice:   settlement.ExecutionCostOrProceed,
				ExecutedQuantity: settlement.Quantity,
				TotalNotional:    settlement.ExecutionCostOrProceed.Mul(settlement.Quantity),
				Direction:        settlement.PositionDirection,
			},
		)
		resultMap[settlement.Account] = resultsForAccount
	}
}

type OrderPlacementResult struct {
	OrderID uint64      `json:"order_id"`
	Status  OrderStatus `json:"status_code"`
}

type OrderExecutionResult struct {
	OrderID          uint64  `json:"order_id"`
	ExecutionPrice   sdk.Dec `json:"execution_price"`
	ExecutedQuantity sdk.Dec `json:"executed_quantity"`
	TotalNotional    sdk.Dec `json:"total_notional"`
	Direction        string  `json:"position_direction"`
}
