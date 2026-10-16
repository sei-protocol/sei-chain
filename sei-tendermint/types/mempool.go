package types

import (
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	tmbytes "github.com/sei-protocol/sei-chain/sei-tendermint/libs/bytes"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

// ErrTxInCache is returned when the mempool rejects a tx it has already seen. The errors below
// wrap it to say why the tx was rejected.
var ErrTxInCache = errors.New("tx already exists in cache")

var (
	// ErrTxInMempool is returned when the tx is already in the mempool or is being checked.
	ErrTxInMempool = fmt.Errorf("%w: tx is pending", ErrTxInCache)
	// ErrTxNonceTooLow is returned when the tx's nonce is below the sender's lowest pending nonce.
	ErrTxNonceTooLow = fmt.Errorf("%w: nonce too low", ErrTxInCache)
	// ErrTxReplacementUnderpriced is returned when the mempool holds a tx with the same nonce
	// and at least the same priority.
	ErrTxReplacementUnderpriced = fmt.Errorf("%w: replacement transaction underpriced", ErrTxInCache)
	// ErrTxInsufficientFunds is returned when the tx would replace a ready tx with the same nonce
	// but the sender cannot afford it.
	ErrTxInsufficientFunds = fmt.Errorf("%w: insufficient funds", ErrTxInCache)
)

// TxHash is the fixed length array hash used as an index.
type TxHash crypto.Hash

func (txHash TxHash) Bytes() tmbytes.HexBytes {
	return crypto.Hash(txHash).Bytes()
}

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
	return txHash.Bytes().String()
}

func (txHash TxHash) Format(s fmt.State, verb rune) {
	txHash.Bytes().Format(s, verb)
}

// TxHashFromProto takes a protobuf representation of TxHash &
// returns the native type.
func TxHashFromProto(dp *tmproto.TxKey) (TxHash, error) {
	if dp == nil {
		return TxHash{}, errors.New("nil data")
	}
	if len(dp.TxKey) != sha256.Size {
		return TxHash{}, fmt.Errorf("invalid tx hash length: %d, expected: %d", len(dp.TxKey), sha256.Size)
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
