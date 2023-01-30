package client

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/rs/zerolog"
)

type AccountInfo struct {
	AccountNumber       uint64
	AccountSequence     uint64
	ShouldResetSequence bool
}

var txAccountInfo = AccountInfo{}

// ObtainAccountInfo ensures the account defined by ctx.GetFromAddress() exists.
// We keep a local copy of account sequence number and manually increment it.
// If the local sequence number is 0, we will initialize it with the latest value getting from the chain.
func (accountInfo *AccountInfo) ObtainAccountInfo(ctx client.Context, txf tx.Factory, logger zerolog.Logger) (tx.Factory, error) {
	if accountInfo.AccountSequence == 0 || accountInfo.ShouldResetSequence {
		err := accountInfo.ResetAccountSequence(ctx, txf, logger)
		if err != nil {
			return txf, err
		}
		accountInfo.ShouldResetSequence = false
	}
	txf = txf.WithAccountNumber(accountInfo.AccountNumber)
	txf = txf.WithSequence(accountInfo.AccountSequence)
	txf = txf.WithGas(0)
	return txf, nil
}

// ResetAccountSequence will reset account sequence number to the latest sequence number in the chain
func (accountInfo *AccountInfo) ResetAccountSequence(ctx client.Context, txf tx.Factory, logger zerolog.Logger) error {
	fromAddr := ctx.GetFromAddress()
	if err := txf.AccountRetriever().EnsureExists(ctx, fromAddr); err != nil {
		return err
	}
	accountNum, sequence, err := txf.AccountRetriever().GetAccountNumberSequence(ctx, fromAddr)
	if err != nil {
		return err
	}
	logger.Info().Msg(fmt.Sprintf("Reset account number to %d and sequence number to %d", accountNum, sequence))
	accountInfo.AccountNumber = accountNum
	accountInfo.AccountSequence = sequence
	return nil
}
