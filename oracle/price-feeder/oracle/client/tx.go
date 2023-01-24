package client

import (
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"sync"
)

type AccountInfo struct {
	AccountNumber   uint64
	AccountSequence uint64
	mtx             sync.Mutex
}

var oracleAccountInfo = AccountInfo{}

// BroadcastTx attempts to generate, sign and broadcast a transaction with the
// given set of messages. It will also simulate gas requirements if necessary.
// It will return an error upon failure.
//
// Note, BroadcastTx is copied from the SDK except it removes a few unnecessary
// things like prompting for confirmation and printing the response. Instead,
// we return the TxResponse.
func BroadcastTx(clientCtx client.Context, txf tx.Factory, msgs ...sdk.Msg) (*sdk.TxResponse, error) {
	err := prepareFactory(clientCtx, txf)
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
	res, err := clientCtx.BroadcastTx(txBytes)
	if err != nil {
		// When error happen, it could be that the sequence number are mismatching
		// We need to reset sequence number to 0 so that it query latest value from the chain next time
		resetAccountSequence()
	}

	return res, err
}

// prepareFactory ensures the account defined by ctx.GetFromAddress() exists.
// We keep a local copy of account sequence number and manually increment it.
// If the local sequence number is 0, we will initialize it with the latest value getting from the chain.
func prepareFactory(ctx client.Context, txf tx.Factory) error {
	oracleAccountInfo.mtx.Lock()
	defer oracleAccountInfo.mtx.Unlock()
	fromAddr := ctx.GetFromAddress()
	if err := txf.AccountRetriever().EnsureExists(ctx, fromAddr); err != nil {
		return err
	}
	if oracleAccountInfo.AccountNumber == 0 || oracleAccountInfo.AccountSequence == 0 {

		accountNum, sequence, err := txf.AccountRetriever().GetAccountNumberSequence(ctx, fromAddr)
		if err != nil {
			return err
		}
		oracleAccountInfo.AccountNumber = accountNum
		oracleAccountInfo.AccountSequence = sequence
	} else {
		oracleAccountInfo.AccountSequence++
	}
	txf.WithAccountNumber(oracleAccountInfo.AccountNumber).WithAccountNumber(oracleAccountInfo.AccountSequence).WithGas(0)
	return nil
}

func resetAccountSequence() {
	oracleAccountInfo.mtx.Lock()
	defer oracleAccountInfo.mtx.Unlock()
	oracleAccountInfo.AccountSequence = 0
}
