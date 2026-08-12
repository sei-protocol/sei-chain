package antedecorators

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
)

// FeeExemptTxGasWanted is the fixed block-gas contribution for transactions that can be fee-exempt.
const FeeExemptTxGasWanted uint64 = 200_000

// GasWantedForTx returns the state-independent gas contribution reported for tx.
// Potential fee exemption is determined only from message shape so changes in chain
// state cannot change the contribution between mempool admission and proposal checks.
func GasWantedForTx(tx sdk.Tx, declaredGas uint64) uint64 {
	if canBeFeeExempt(tx) {
		return FeeExemptTxGasWanted
	}
	return declaredGas
}

func canBeFeeExempt(tx sdk.Tx) bool {
	msgs := tx.GetMsgs()
	if len(msgs) == 0 {
		return false
	}

	switch msgs[0].(type) {
	case *evmtypes.MsgAssociate:
		return len(msgs) == 1
	case *oracletypes.MsgAggregateExchangeRateVote:
		for _, msg := range msgs[1:] {
			if _, ok := msg.(*oracletypes.MsgAggregateExchangeRateVote); !ok {
				return false
			}
		}
		return true
	default:
		return false
	}
}
