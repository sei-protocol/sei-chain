package antedecorators

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
)

// FeeExemptTxGasWanted is the fixed block-gas contribution for transactions that can be fee-exempt.
const FeeExemptTxGasWanted uint64 = 25_000

type feeExemptTxShape uint8

const (
	notFeeExempt feeExemptTxShape = iota
	associateFeeExempt
	oracleVoteFeeExempt
)

// GasWantedForTx returns the state-independent gas contribution reported for tx.
// Potential fee exemption is determined only from message shape so changes in chain
// state cannot change the contribution between mempool admission and proposal checks.
func GasWantedForTx(tx sdk.Tx, declaredGas uint64) uint64 {
	if feeExemptShape(tx) != notFeeExempt {
		return FeeExemptTxGasWanted
	}
	return declaredGas
}

// ExecutionGasLimitForTx returns an enforceable limit that cannot exceed tx's reported contribution.
func ExecutionGasLimitForTx(tx sdk.Tx, declaredGas uint64) uint64 {
	return min(declaredGas, GasWantedForTx(tx, declaredGas))
}

func feeExemptShape(tx sdk.Tx) feeExemptTxShape {
	msgs := tx.GetMsgs()
	if len(msgs) == 0 {
		return notFeeExempt
	}

	switch msgs[0].(type) {
	case *evmtypes.MsgAssociate:
		if len(msgs) == 1 {
			return associateFeeExempt
		}
	case *oracletypes.MsgAggregateExchangeRateVote:
		for _, msg := range msgs[1:] {
			if _, ok := msg.(*oracletypes.MsgAggregateExchangeRateVote); !ok {
				return notFeeExempt
			}
		}
		return oracleVoteFeeExempt
	}
	return notFeeExempt
}
