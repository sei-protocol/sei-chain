package client

import (
	"fmt"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"sync/atomic"
)

var (
	AtomicSequenceNumber atomic.Uint64
)

// BroadcastTx attempts to generate, sign and broadcast a transaction with the
// given set of messages. It will also simulate gas requirements if necessary.
// It will return an error upon failure.
//
// Note, BroadcastTx is copied from the SDK except it removes a few unnecessary
// things like prompting for confirmation and printing the response. Instead,
// we return the TxResponse.
func BroadcastTx(clientCtx client.Context, txf tx.Factory, msgs ...sdk.Msg) (*sdk.TxResponse, error) {
	txf, err := prepareFactory(clientCtx, txf)
	if err != nil {
		return nil, err
	}

	// Build unsigned tx
	transaction, err := tx.BuildUnsignedTx(txf, msgs...)
	if err != nil {
		return nil, err
	}

	// Sign the transaction
	if err = tx.Sign(txf, clientCtx.GetFromName(), transaction, true); err != nil {
		return nil, err
	}

	// Get bytes to send
	txBytes, err := clientCtx.TxConfig.TxEncoder()(transaction.GetTx())
	if err != nil {
		return nil, err
	}
	fmt.Printf("[Price Feeder] Sending broadcast tx with account %d sequence %d\n", txf.AccountNumber(), txf.Sequence())

	return clientCtx.BroadcastTx(txBytes)
}

// prepareFactory ensures the account defined by ctx.GetFromAddress() exists and
// if the account number and/or the account sequence number are zero (not set),
// they will be queried for and set on the provided Factory. A new Factory with
// the updated fields will be returned.
func prepareFactory(clientCtx client.Context, txf tx.Factory) (tx.Factory, error) {
	fromAddr := clientCtx.GetFromAddress()
	if err := txf.AccountRetriever().EnsureExists(clientCtx, fromAddr); err != nil {
		return txf, err
	}
	accountNum, sequence, err := txf.AccountRetriever().GetAccountNumberSequence(clientCtx, fromAddr)
	if err != nil {
		return txf, err
	}
	if !AtomicSequenceNumber.CompareAndSwap(0, sequence) {
		sequence = AtomicSequenceNumber.Add(1)
	}
	txf = txf.WithAccountNumber(accountNum).WithSequence(sequence).WithGas(0)
	return txf, nil
}
