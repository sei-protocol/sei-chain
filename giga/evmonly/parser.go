package evmonly

import (
	"context"
	"errors"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// senderAt returns the already verified sender of txs[i], if any. senders is
// either empty or aligned with txs.
func senderAt(senders []utils.Option[common.Address], i int) utils.Option[common.Address] {
	if i < len(senders) {
		return senders[i]
	}
	return utils.None[common.Address]()
}

func parseBlockTxs(ctx context.Context, txs [][]byte, signer ethtypes.Signer, senders []utils.Option[common.Address], workers int) ([]PreparedTx, error) {
	decoded, err := decodeBlockTxs(ctx, txs, workers)
	if err != nil {
		return nil, err
	}
	return resolveBlockSenders(ctx, decoded, signer, senders, workers)
}

// decodeBlockTxs decodes and validates every raw transaction, spread across workers.
func decodeBlockTxs(ctx context.Context, txs [][]byte, workers int) ([]*ethtypes.Transaction, error) {
	decoded := make([]*ethtypes.Transaction, len(txs))
	err := abevm.ForEachTx(ctx, len(txs), workers, func(i int) error {
		tx, err := decodeSupportedTx(txs[i])
		if err != nil {
			return err
		}
		// The transaction caches its hash, which execution reads several times per transaction.
		tx.Hash()
		decoded[i] = tx
		return nil
	})
	if err != nil {
		return nil, err
	}
	return decoded, nil
}

// resolveBlockSenders resolves the sender of every decoded transaction, spread across workers.
func resolveBlockSenders(ctx context.Context, txs []*ethtypes.Transaction, signer ethtypes.Signer, senders []utils.Option[common.Address], workers int) ([]PreparedTx, error) {
	parsed := make([]PreparedTx, len(txs))
	err := abevm.ForEachTx(ctx, len(txs), workers, func(i int) error {
		sender, err := resolveSender(txs[i], signer, senderAt(senders, i))
		if err != nil {
			return err
		}
		parsed[i] = PreparedTx{Tx: txs[i], Sender: sender}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return parsed, nil
}

// parsePreparedTx decodes raw and resolves its sender.
func parsePreparedTx(raw []byte, signer ethtypes.Signer, known utils.Option[common.Address]) (PreparedTx, error) {
	tx, err := decodeSupportedTx(raw)
	if err != nil {
		return PreparedTx{}, err
	}
	sender, err := resolveSender(tx, signer, known)
	if err != nil {
		return PreparedTx{}, err
	}
	return PreparedTx{Tx: tx, Sender: sender}, nil
}

// decodeSupportedTx decodes raw and rejects transaction types the executor does not run.
func decodeSupportedTx(raw []byte) (*ethtypes.Transaction, error) {
	tx, err := decodeRawTx(raw)
	if err != nil {
		return nil, err
	}
	if err := validateSupportedTx(tx); err != nil {
		return nil, err
	}
	return tx, nil
}

// resolveSender returns tx's sender. It is taken from known when present and signer would recover
// the same address; otherwise it is recovered from the signature.
func resolveSender(tx *ethtypes.Transaction, signer ethtypes.Signer, known utils.Option[common.Address]) (common.Address, error) {
	if sender, ok := known.Get(); ok && signerAccepts(signer, tx) {
		return sender, nil
	}
	return ethtypes.Sender(signer, tx)
}

// signerAccepts reports whether signer recovers senders for tx: the transaction
// is bound to signer's chain and its type is enabled by signer's fork schedule.
// Signers without a chain ID (pre-EIP-155) never accept, so the sender is recovered.
func signerAccepts(signer ethtypes.Signer, tx *ethtypes.Transaction) bool {
	chainID := signer.ChainID()
	if chainID == nil || !tx.Protected() || tx.ChainId().Cmp(chainID) != 0 {
		return false
	}
	// SignatureValues rejects types the signer's forks do not enable before it
	// looks at the signature, so a zero one probes support without recovery.
	var probe [crypto.SignatureLength]byte
	r, _, _, err := signer.SignatureValues(tx, probe[:])
	_ = r
	return !errors.Is(err, ethtypes.ErrTxTypeNotSupported)
}

func decodeRawTx(raw []byte) (*ethtypes.Transaction, error) {
	tx := new(ethtypes.Transaction)
	if err := tx.UnmarshalBinary(raw); err != nil {
		return nil, err
	}
	return tx, nil
}
