package evm

import "errors"

// Errors a validator's mempool refuses a transaction with, outside the application's admission
// checks that ErrTxRejected marks.
var (
	// ErrTxAlreadyKnown refuses a transaction the mempool still holds. Its message matches
	// go-ethereum's txpool.ErrAlreadyKnown, which Ethereum clients recognise.
	ErrTxAlreadyKnown = errors.New("already known")
	// ErrTxBadNonce refuses a transaction whose nonce is not the next one its sender's lane admits.
	ErrTxBadNonce = errors.New("bad nonce")
	// ErrTxTooLarge refuses a transaction larger, in bytes or gas, than one lane block holds.
	ErrTxTooLarge = errors.New("transaction too large")
	// ErrNotProducing refuses a transaction while the validator has no produce session.
	ErrNotProducing = errors.New("not producing")
)
