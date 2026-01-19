package verify

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/sei-protocol/sei-chain/testutil/processblock"
	"github.com/stretchr/testify/require"
)

// Check balance changes as result of executing the provided transactions.
// Only works if all transactions are successful.
func Balance(t *testing.T, app *processblock.App, f BlockRunnable, txs []signing.Tx) BlockRunnable {
	return func() []uint32 {
		expectedChanges := map[string]map[string]int64{} // denom -> (account -> delta)
		for _, tx := range txs {
			for _, fee := range tx.GetFee() {
				updateExpectedBalanceChange(expectedChanges, tx.FeePayer().String(), fee, false)
			}
			for _, msg := range tx.GetMsgs() {
				switch m := msg.(type) {
				case *banktypes.MsgSend:
					updateMultipleExpectedBalanceChange(expectedChanges, m.FromAddress, m.Amount, false)
					updateMultipleExpectedBalanceChange(expectedChanges, m.ToAddress, m.Amount, true)
				case *banktypes.MsgMultiSend:
					for _, input := range m.Inputs {
						updateMultipleExpectedBalanceChange(expectedChanges, input.Address, input.Coins, false)
					}
					for _, output := range m.Outputs {
						updateMultipleExpectedBalanceChange(expectedChanges, output.Address, output.Coins, true)
					}
				default:
					// TODO: add coverage for other balance-affecting messages to enable testing for those message types
					continue
				}
			}
		}
		expectedBalances := map[string]map[string]int64{}
		for denom, changes := range expectedChanges {
			expectedBalances[denom] = map[string]int64{}
			for account, delta := range changes {
				balance := app.BankKeeper.GetBalance(app.Ctx(), sdk.MustAccAddressFromBech32(account), denom)
				expectedBalances[denom][account] = balance.Amount.Int64() + delta
			}
		}

		results := f()

		for denom, expectedBalances := range expectedBalances {
			for account, expectedBalance := range expectedBalances {
				actualBalance := app.BankKeeper.GetBalance(app.Ctx(), sdk.MustAccAddressFromBech32(account), denom)
				require.Equal(t, expectedBalance, actualBalance.Amount.Int64())
			}
		}

		return results
	}
}

func updateMultipleExpectedBalanceChange(changes map[string]map[string]int64, account string, coins sdk.Coins, positive bool) {
	for _, coin := range coins {
		updateExpectedBalanceChange(changes, account, coin, positive)
	}
}

func updateExpectedBalanceChange(changes map[string]map[string]int64, account string, coin sdk.Coin, positive bool) {
	if _, ok := changes[coin.Denom]; !ok {
		changes[coin.Denom] = map[string]int64{}
	}
	if _, ok := changes[coin.Denom][account]; !ok {
		changes[coin.Denom][account] = 0
	}
	if positive {
		changes[coin.Denom][account] += coin.Amount.Int64()
	} else {
		changes[coin.Denom][account] -= coin.Amount.Int64()
	}
}
