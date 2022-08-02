package dex

import "github.com/sei-protocol/sei-chain/utils"

type LiquidationRequest struct {
	Requestor          string
	AccountToLiquidate string
}

func (l *LiquidationRequest) GetAccount() string {
	return l.Requestor
}

type LiquidationRequests struct {
	memStateItems[*LiquidationRequest]
}

func NewLiquidationRequests() *LiquidationRequests {
	return &LiquidationRequests{memStateItems: NewItems(utils.PtrCopier[LiquidationRequest])}
}

func (lrs *LiquidationRequests) Copy() *LiquidationRequests {
	return &LiquidationRequests{memStateItems: *lrs.memStateItems.Copy()}
}

func (lrs *LiquidationRequests) IsAccountLiquidating(accountToLiquidate string) bool {
	for _, lr := range lrs.internal {
		if lr.AccountToLiquidate == accountToLiquidate {
			return true
		}
	}
	return false
}
