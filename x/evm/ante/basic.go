package ante

import (
	"crypto/sha256"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/params"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

type BasicDecorator struct {
}

func NewBasicDecorator() *BasicDecorator {
	return &BasicDecorator{}
}

// cherrypicked from go-ethereum:txpool:ValidateTransaction
func (gl BasicDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if !ctx.IsCheckTx() {
		return next(ctx, tx, simulate)
	}
	msg := evmtypes.MustGetEVMTransactionMessage(tx)
	etx, _ := msg.AsTransaction()

	if etx.To() == nil && len(etx.Data()) > params.MaxInitCodeSize {
		return ctx, fmt.Errorf("%w: code size %v, limit %v", core.ErrMaxInitCodeSizeExceeded, len(etx.Data()), params.MaxInitCodeSize)
	}

	if etx.Value().Sign() < 0 {
		return ctx, sdkerrors.ErrInvalidCoins
	}

	intrGas, err := core.IntrinsicGas(etx.Data(), etx.AccessList(), etx.To() == nil, true, true, true)
	if err != nil {
		return ctx, err
	}
	if etx.Gas() < intrGas {
		return ctx, sdkerrors.ErrOutOfGas
	}

	// Ensure blob transactions have valid commitments
	if etx.Type() == ethtypes.BlobTxType {
		sidecar := etx.BlobTxSidecar()
		if sidecar == nil {
			return ctx, fmt.Errorf("missing sidecar in blob transaction")
		}
		// Ensure the number of items in the blob transaction and various side
		// data match up before doing any expensive validations
		hashes := etx.BlobHashes()
		if len(hashes) == 0 {
			return ctx, fmt.Errorf("blobless blob transaction")
		}
		if len(hashes) > params.MaxBlobGasPerBlock/params.BlobTxBlobGasPerBlob {
			return ctx, fmt.Errorf("too many blobs in transaction: have %d, permitted %d", len(hashes), params.MaxBlobGasPerBlock/params.BlobTxBlobGasPerBlob)
		}
		if err := validateBlobSidecar(hashes, sidecar); err != nil {
			return ctx, err
		}
	}

	return next(ctx, tx, simulate)
}

func validateBlobSidecar(hashes []common.Hash, sidecar *ethtypes.BlobTxSidecar) error {
	if len(sidecar.Blobs) != len(hashes) {
		return fmt.Errorf("invalid number of %d blobs compared to %d blob hashes", len(sidecar.Blobs), len(hashes))
	}
	if len(sidecar.Commitments) != len(hashes) {
		return fmt.Errorf("invalid number of %d blob commitments compared to %d blob hashes", len(sidecar.Commitments), len(hashes))
	}
	if len(sidecar.Proofs) != len(hashes) {
		return fmt.Errorf("invalid number of %d blob proofs compared to %d blob hashes", len(sidecar.Proofs), len(hashes))
	}
	// Blob quantities match up, validate that the provers match with the
	// transaction hash before getting to the cryptography
	hasher := sha256.New()
	for i, want := range hashes {
		hasher.Write(sidecar.Commitments[i][:])
		hash := hasher.Sum(nil)
		hasher.Reset()

		var vhash common.Hash
		vhash[0] = params.BlobTxHashVersion
		copy(vhash[1:], hash[1:])

		if vhash != want {
			return fmt.Errorf("blob %d: computed hash %#x mismatches transaction one %#x", i, vhash, want)
		}
	}
	// Blob commitments match with the hashes in the transaction, verify the
	// blobs themselves via KZG
	for i := range sidecar.Blobs {
		if err := kzg4844.VerifyBlobProof(sidecar.Blobs[i], sidecar.Commitments[i], sidecar.Proofs[i]); err != nil {
			return fmt.Errorf("invalid blob %d: %v", i, err)
		}
	}
	return nil
}
