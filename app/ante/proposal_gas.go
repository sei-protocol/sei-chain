package ante

import (
	"math"
	"math/bits"

	authtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/types"
	paramtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
)

// AssociateTxStateAccessGas covers one signer's state reads and writes in the
// paid MsgAssociate ante path, including address association, account sequence
// updates, and fee handling. It is calibrated above the 39,833 gas remaining
// after size and signature charges for a maximal single-signer paid associate
// under the default store gas schedule. The proposal model applies this amount
// once per signer and once more for a distinct fee grant. Changes to those paths
// or the store schedule must recalibrate this overhead.
const AssociateTxStateAccessGas uint64 = 45_000

// AssociateTxGasDimensions contains the state-independent transaction shape
// used to model a native associate's proposal-gas contribution.
type AssociateTxGasDimensions struct {
	TxSize         uint64
	SignerCount    uint64
	SignatureCount uint64
	UsesFeeGrant   bool
}

// AssociateTxProposalGasWanted returns the deterministic block-gas contribution
// for a native MsgAssociate. It models the paid path as serialized size gas,
// configured verification gas per signature, and fixed state-access gas per
// signer and fee grant, then applies the configured Cosmos gas multiplier.
// Association state and the transaction's attacker-controlled gas limit do not
// affect the result.
func AssociateTxProposalGasWanted(
	dimensions AssociateTxGasDimensions,
	authParams authtypes.Params,
	cosmosGasParams paramtypes.CosmosGasParams,
) uint64 {
	txSizeGas := saturatingMultiply(dimensions.TxSize, authParams.TxSizeCostPerByte)
	signatureCost := max(authParams.SigVerifyCostSecp256k1, authParams.SigVerifyCostED25519)
	signatureGas := saturatingMultiply(dimensions.SignatureCount, signatureCost)
	stateAccessCount := dimensions.SignerCount
	if dimensions.UsesFeeGrant {
		stateAccessCount = saturatingAdd(stateAccessCount, 1)
	}
	stateAccessGas := saturatingMultiply(stateAccessCount, AssociateTxStateAccessGas)
	unscaledGas := saturatingAdd(txSizeGas, signatureGas)
	unscaledGas = saturatingAdd(unscaledGas, stateAccessGas)
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
