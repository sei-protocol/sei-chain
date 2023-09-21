package ethtx

import sdk "github.com/cosmos/cosmos-sdk/types"

func (tx *BlobTx) SetTo(v string) {
	tx.To = v
}

func (tx *BlobTx) SetAmount(v sdk.Int) {
	tx.Amount = &v
}

func (tx *BlobTx) SetGasFeeCap(v sdk.Int) {
	tx.GasFeeCap = &v
}

func (tx *BlobTx) SetGasTipCap(v sdk.Int) {
	tx.GasTipCap = &v
}

func (tx *BlobTx) SetAccesses(v AccessList) {
	tx.Accesses = v
}

func (tx *BlobTx) SetBlobFeeCap(v sdk.Int) {
	tx.BlobFeeCap = &v
}

func (tx *BlobTx) SetBlobHashes(v [][]byte) {
	tx.BlobHashes = v
}

func (tx *BlobTx) SetBlobSidecar(v *BlobTxSidecar) {
	tx.Sidecar = v
}
