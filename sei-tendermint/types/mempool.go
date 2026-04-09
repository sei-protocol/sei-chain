package types

import (
	"crypto/sha256"
	"errors"

	tmbytes "github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// TxKey is the fixed length array key used as an index.
type TxKey [sha256.Size]byte

// ToProto converts Data to protobuf
func (txKey *TxKey) ToProto() *tmproto.TxKey {
	tp := new(tmproto.TxKey)

	txBzs := make([]byte, len(txKey))
	if len(txKey) > 0 {
		copy(txBzs, txKey[:])
		tp.TxKey = txBzs
	}

	return tp
}

func (txKey TxKey) String() string {
	return tmbytes.HexBytes(txKey[:]).String()
}

// TxKeyFromProto takes a protobuf representation of TxKey &
// returns the native type.
func TxKeyFromProto(dp *tmproto.TxKey) (TxKey, error) {
	if dp == nil {
		return TxKey{}, errors.New("nil data")
	}
	var txBzs [sha256.Size]byte
	copy(txBzs[:], dp.TxKey)

	return txBzs, nil
}

func TxKeysListFromProto(dps []*tmproto.TxKey) ([]TxKey, error) {
	var txKeys []TxKey
	for _, txKey := range dps {
		txKey, err := TxKeyFromProto(txKey)
		if err != nil {
			return nil, err
		}
		txKeys = append(txKeys, txKey)
	}
	return txKeys, nil
}
