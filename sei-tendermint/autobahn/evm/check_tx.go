package evm

import "fmt"

// CheckTx parses and checks a transaction for admission, remembering accepted senders.
func CheckTx(app *Proxy, signer *Signer, raw []byte) (EvmTx, AdmittedTx, error) {
	tx, err := signer.ParseTx(raw)
	if err != nil {
		return EvmTx{}, AdmittedTx{}, fmt.Errorf("%w: %w", ErrTxRejected, err)
	}
	admitted, err := app.CheckTx(tx)
	if err != nil {
		return tx, admitted, err
	}
	signer.Remember(tx.Hash(), tx.Sender())
	return tx, admitted, nil
}
