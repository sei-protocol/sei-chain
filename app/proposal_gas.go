package app

import (
	"math"

	appante "github.com/sei-protocol/sei-chain/app/ante"
	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"
	authsigning "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// proposalGasWanted returns the contribution proposal construction and
// validation must use for tx. Native associates use the modeled paid-path cost;
// every other transaction keeps the gas reported by its normal ante path.
func (app *App) proposalGasWanted(ctx sdk.Context, tx sdk.Tx, txSize uint64, gasWanted uint64) uint64 {
	if !evmtypes.IsTxMsgAssociate(tx) {
		return gasWanted
	}

	signerCount, signatureCount, usesFeeGrant, ok := associateTxGasDimensions(tx)
	if !ok {
		return math.MaxUint64
	}

	queryCtx := ctx.WithGasMeter(storetypes.NewNoConsumptionInfiniteGasMeter())
	authParams := app.AccountKeeper.GetParams(queryCtx)
	cosmosGasParams := app.ParamsKeeper.GetCosmosGasParams(queryCtx)
	return appante.AssociateTxProposalGasWanted(
		appante.AssociateTxGasDimensions{
			TxSize:         txSize,
			SignerCount:    signerCount,
			SignatureCount: signatureCount,
			UsesFeeGrant:   usesFeeGrant,
		},
		authParams,
		cosmosGasParams,
	)
}

// associateTxGasDimensions derives every variable-cost dimension from the
// transaction bytes. Signature count uses the number of populated multisig
// leaves when available and never falls below the number of account signers.
func associateTxGasDimensions(tx sdk.Tx) (signerCount, signatureCount uint64, usesFeeGrant, ok bool) {
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return 0, 0, false, false
	}

	signerCount = uint64(len(sigTx.GetSigners()))
	if signerCount == 0 {
		return 0, 0, false, false
	}
	signatureCount = signerCount
	if signatures, err := sigTx.GetSignaturesV2(); err == nil {
		var populatedSignatureCount uint64
		for _, signature := range signatures {
			populatedSignatureCount = saturatingSignatureCountAdd(populatedSignatureCount, countSignatureLeaves(signature.Data))
		}
		if populatedSignatureCount > signatureCount {
			signatureCount = populatedSignatureCount
		}
	}

	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return 0, 0, false, false
	}
	feeGranter := feeTx.FeeGranter()
	feePayer := feeTx.FeePayer()
	usesFeeGrant = feeGranter != nil && !feeGranter.Equals(feePayer)
	return signerCount, signatureCount, usesFeeGrant, true
}

func countSignatureLeaves(data signing.SignatureData) uint64 {
	switch signatureData := data.(type) {
	case *signing.SingleSignatureData:
		return 1
	case *signing.MultiSignatureData:
		var count uint64
		for _, nested := range signatureData.Signatures {
			count = saturatingSignatureCountAdd(count, countSignatureLeaves(nested))
		}
		return count
	default:
		return 0
	}
}

func saturatingSignatureCountAdd(a, b uint64) uint64 {
	if math.MaxUint64-a < b {
		return math.MaxUint64
	}
	return a + b
}
