package client

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rs/zerolog"
)

type AccountInfo struct {
	AccountNumber   uint64
	AccountSequence uint64
}

var oracleAccountInfo = AccountInfo{}

// BroadcastTx attempts to generate, sign and broadcast a transaction with the
// given set of messages. It will also simulate gas requirements if necessary.
// It will return an error upon failure.
//
// Note, BroadcastTx is copied from the SDK except it removes a few unnecessary
// things like prompting for confirmation and printing the response. Instead,
// we return the TxResponse.
func BroadcastTx(clientCtx client.Context, txf tx.Factory, logger zerolog.Logger, msgs ...sdk.Msg) (*sdk.TxResponse, error) {
	txf, err := prepareFactory(clientCtx, txf, logger)
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
	logger.Info().Msg(fmt.Sprintf("Sending broadcastTx with account sequence number %d", txf.Sequence()))
	res, err := clientCtx.BroadcastTx(txBytes)
	if err != nil {
		// When error happen, it could be that the sequence number are mismatching
		// We need to reset sequence number to query the latest value from the chain
		_ = resetAccountSequence(clientCtx, txf, logger)
	} else {
		// Only increment sequence number if we successfully broadcast the previous transaction
		oracleAccountInfo.AccountSequence = oracleAccountInfo.AccountSequence + 1
		logger.Info().Msg(fmt.Sprintf("Setting sequence number to %d", oracleAccountInfo.AccountSequence))
	}

	return res, err
}

// prepareFactory ensures the account defined by ctx.GetFromAddress() exists.
// We keep a local copy of account sequence number and manually increment it.
// If the local sequence number is 0, we will initialize it with the latest value getting from the chain.
func prepareFactory(ctx client.Context, txf tx.Factory, logger zerolog.Logger) (tx.Factory, error) {
	if oracleAccountInfo.AccountNumber == 0 || oracleAccountInfo.AccountSequence == 0 {
		err := resetAccountSequence(ctx, txf, logger)
		if err != nil {
			return txf, err
		}
	}
	txf = txf.WithAccountNumber(oracleAccountInfo.AccountNumber)
	txf = txf.WithSequence(oracleAccountInfo.AccountSequence)
	txf = txf.WithGas(0)
	return txf, nil
}

// resetAccountSequence will reset account sequence number to the latest sequence number in the chain
func resetAccountSequence(ctx client.Context, txf tx.Factory, logger zerolog.Logger) error {
	fromAddr := ctx.GetFromAddress()
	if err := txf.AccountRetriever().EnsureExists(ctx, fromAddr); err != nil {
		return err
	}
	accountNum, sequence, err := txf.AccountRetriever().GetAccountNumberSequence(ctx, fromAddr)
	if err != nil {
		return err
	}
	logger.Info().Msg(fmt.Sprintf("Reset account number to %d and sequence number to %d", accountNum, sequence))
	oracleAccountInfo.AccountNumber = accountNum
	oracleAccountInfo.AccountSequence = sequence
	return nil
}
