package evm

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// DecodeBlockTxs returns transactions in input order with their hashes cached.
// Each transaction is checked by validate; work is spread across workers.
func DecodeBlockTxs(txs [][]byte, workers int, validate func(*ethtypes.Transaction) error) ([]*ethtypes.Transaction, error) {
	decoded := make([]*ethtypes.Transaction, len(txs))
	err := forEach(len(txs), workers, func(i int) error {
		tx := new(ethtypes.Transaction)
		if err := tx.UnmarshalBinary(txs[i]); err != nil {
			return err
		}
		if err := validate(tx); err != nil {
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

// ResolveBlockSenders returns verified senders in transaction order.
// Known senders are reused when compatible with signer; other senders are recovered.
func ResolveBlockSenders(txs []*ethtypes.Transaction, signer ethtypes.Signer, senders []utils.Option[common.Address], workers int) ([]common.Address, error) {
	resolved := make([]common.Address, len(txs))
	err := forEach(len(txs), workers, func(i int) error {
		tx := txs[i]
		if i < len(senders) {
			if sender, ok := senders[i].Get(); ok && signerAdmits(signer, tx) == nil {
				resolved[i] = sender
				return nil
			}
		}
		sender, err := ethtypes.Sender(signer, tx)
		if err != nil {
			return err
		}
		resolved[i] = sender
		return nil
	})
	if err != nil {
		return nil, err
	}
	return resolved, nil
}

// signerAdmits checks replay protection, chain ID, and transaction-type support.
func signerAdmits(signer ethtypes.Signer, tx *ethtypes.Transaction) error {
	if !tx.Protected() {
		return errors.New("unprotected Ethereum transaction")
	}
	if tx.Type() == ethtypes.BlobTxType {
		return ethtypes.ErrTxTypeNotSupported
	}
	chainID := signer.ChainID()
	if chainID == nil || tx.ChainId().Cmp(chainID) != 0 {
		return fmt.Errorf("%w: expected %v, got %s", ethtypes.ErrInvalidChainId, chainID, tx.ChainId())
	}
	// SignatureValues checks transaction-type support before inspecting the signature.
	// A zero signature probes support without recovering the sender.
	var probe [crypto.SignatureLength]byte
	_, _, _, err := signer.SignatureValues(tx, probe[:])
	return err
}
