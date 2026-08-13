package types

import (
	"crypto/sha256"
	"errors"
	"fmt"

	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

// ErrTxInCache is returned to the client if we saw tx earlier
var ErrTxInCache = errors.New("tx already exists in cache")

// TxKey is the fixed length array key used as an index.
type TxKey [sha256.Size]byte

// ToProto converts Data to protobuf
func (txKey *TxKey) ToProto() *tmproto.TxKey {
	tp := new(tmproto.TxKey)

	txBzs := make([]byte, len(txKey))
	if len(txKey) > 0 {
		for i := range txKey {
			txBzs[i] = txKey[i]
		}
		tp.TxKey = txBzs
	}

	return tp
}

// TxKeyFromProto takes a protobuf representation of TxKey &
// returns the native type.
func TxKeyFromProto(dp *tmproto.TxKey) (TxKey, error) {
	if dp == nil {
		return TxKey{}, errors.New("nil data")
	}
	var txBzs [sha256.Size]byte
	for i := range dp.TxKey {
		txBzs[i] = dp.TxKey[i]
	}

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

// ErrTxTooLarge defines an error when a transaction is too big to be sent in a
// message to other peers.
type ErrTxTooLarge struct {
	Max    int
	Actual int
}

func (e ErrTxTooLarge) Error() string {
	return fmt.Sprintf("Tx too large. Max size is %d, but got %d", e.Max, e.Actual)
}

// ErrMempoolIsFull defines an error where Tendermint and the application cannot
// handle that much load.
type ErrMempoolIsFull struct {
	NumTxs      int
	MaxTxs      int
	TxsBytes    int64
	MaxTxsBytes int64
}

func (e ErrMempoolIsFull) Error() string {
	return fmt.Sprintf(
		"mempool is full: number of txs %d (max: %d), total txs bytes %d (max: %d)",
		e.NumTxs,
		e.MaxTxs,
		e.TxsBytes,
		e.MaxTxsBytes,
	)
}

// ErrMempoolPendingIsFull defines an error where there are too many pending transactions
// not processed yet
type ErrMempoolPendingIsFull struct {
	NumTxs      int
	MaxTxs      int
	TxsBytes    int64
	MaxTxsBytes int64
}

func (e ErrMempoolPendingIsFull) Error() string {
	return fmt.Sprintf(
		"mempool pending set is full: number of txs %d (max: %d), total txs bytes %d (max: %d)",
		e.NumTxs,
		e.MaxTxs,
		e.TxsBytes,
		e.MaxTxsBytes,
	)
}

// ErrPreCheck defines an error where a transaction fails a pre-check.
type ErrPreCheck struct {
	Reason error
}

func (e ErrPreCheck) Error() string {
	return e.Reason.Error()
}

// IsPreCheckError returns true if err is due to pre check failure.
func IsPreCheckError(err error) bool {
	return errors.As(err, &ErrPreCheck{})
}
