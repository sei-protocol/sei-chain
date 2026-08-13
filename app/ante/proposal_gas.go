package ante

import (
	"math"
	"math/bits"

	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	paramtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
)

// AssociateTxStateAccessGas covers the state reads and writes in the paid
// MsgAssociate ante path, including address association, account sequence
// updates, and fee handling. It is calibrated above the 39,833 gas remaining
// after size and signature charges for a maximal paid associate transaction
// under the default store gas schedule. Changes to that path or schedule must
// recalibrate this overhead; variable costs are added separately from params.
const AssociateTxStateAccessGas uint64 = 45_000

// AssociateTxProposalGasWanted returns the deterministic block-gas contribution
// for a native MsgAssociate. It models the paid path as serialized size gas,
// one secp256k1 signature verification, and fixed state-access overhead, then
// applies the configured Cosmos gas multiplier. Association state and the
// transaction's attacker-controlled gas limit do not affect the result.
func AssociateTxProposalGasWanted(
	txSize uint64,
	authParams authtypes.Params,
	cosmosGasParams paramtypes.CosmosGasParams,
) uint64 {
	txSizeGas := saturatingMultiply(txSize, authParams.TxSizeCostPerByte)
	unscaledGas := saturatingAdd(txSizeGas, authParams.SigVerifyCostSecp256k1)
	unscaledGas = saturatingAdd(unscaledGas, AssociateTxStateAccessGas)
	return saturatingMultiplyDivide(
		unscaledGas,
		cosmosGasParams.CosmosGasMultiplierNumerator,
		cosmosGasParams.CosmosGasMultiplierDenominator,
	)
}

func saturatingAdd(a, b uint64) uint64 {
	result, carry := bits.Add64(a, b, 0)
	if carry != 0 {
		return math.MaxUint64
	}
	return result
}

func saturatingMultiply(a, b uint64) uint64 {
	high, low := bits.Mul64(a, b)
	if high != 0 {
		return math.MaxUint64
	}
	return low
}

func saturatingMultiplyDivide(value, multiplier, divisor uint64) uint64 {
	if divisor == 0 {
		return math.MaxUint64
	}
	high, low := bits.Mul64(value, multiplier)
	if high >= divisor {
		return math.MaxUint64
	}
	result, _ := bits.Div64(high, low, divisor)
	return result
}
