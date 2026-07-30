//go:build mock_balances

package app

import (
	"math/big"
	"testing"

	"github.com/sei-protocol/sei-chain/x/evm/state"
)

func TestMempoolBalanceFloor(t *testing.T) {
	below := big.NewInt(0)
	if got := mempoolBalanceFloor(below); got.Cmp(state.TopOffAmount) != 0 {
		t.Errorf("floor(0) = %s; want TopOffAmount %s", got, state.TopOffAmount)
	}

	above := new(big.Int).Add(state.TopOffAmount, big.NewInt(1))
	if got := mempoolBalanceFloor(above); got.Cmp(above) != 0 {
		t.Errorf("floor(above) = %s; want unchanged %s", got, above)
	}
}
