package types

import (
	"crypto/sha256"
	"errors"

	tmbytes "github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// TxHash is the fixed length array hash used as an index.
type TxHash [sha256.Size]byte

// ToProto converts Data to protobuf
func (txHash *TxHash) ToProto() *tmproto.TxKey {
	tp := new(tmproto.TxKey)

	txBzs := make([]byte, len(txHash))
	if len(txHash) > 0 {
		copy(txBzs, txHash[:])
		tp.TxKey = txBzs
	}

	return tp
}

func (txHash TxHash) String() string {
	return tmbytes.HexBytes(txHash[:]).String()
}

// TxHashFromProto takes a protobuf representation of TxHash &
// returns the native type.
func TxHashFromProto(dp *tmproto.TxKey) (TxHash, error) {
	if dp == nil {
		return TxHash{}, errors.New("nil data")
	}
	var txBzs [sha256.Size]byte
	copy(txBzs[:], dp.TxKey)

	return txBzs, nil
}

func TxHashesListFromProto(dps []*tmproto.TxKey) ([]TxHash, error) {
	var txHashes []TxHash
	for _, txHash := range dps {
		txHash, err := TxHashFromProto(txHash)
		if err != nil {
			return nil, err
		}
		txHashes = append(txHashes, txHash)
	}
	return txHashes, nil
}
