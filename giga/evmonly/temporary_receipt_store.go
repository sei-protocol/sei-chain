package evmonly

import (
	"errors"
	"os"
	"sync"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
)

type temporaryReceiptStore struct {
	receipt.ReceiptStore

	directory string
	closeOnce sync.Once
	closeErr  error
}

// OpenTemporaryReceiptStore opens the Giga receipt backend in a new temporary
// directory. Closing the store removes the directory and all stored receipts.
func OpenTemporaryReceiptStore(parentDirectory string) (receipt.ReceiptStore, error) {
	directory, err := os.MkdirTemp(parentDirectory, "evmonly-receipts-")
	if err != nil {
		return nil, err
	}
	storageConfig, err := config.DefaultGigaStorageConfig(directory)
	if err != nil {
		return nil, errors.Join(err, os.RemoveAll(directory))
	}
	receiptConfig := storageConfig.ReceiptDBConfig
	// This temporary store is not registered with a storage garbage collector.
	receiptConfig.ExternalPruning = false
	receiptStore, err := receipt.NewReceiptStore(receiptConfig, nil)
	if err != nil {
		return nil, errors.Join(err, os.RemoveAll(directory))
	}
	return &temporaryReceiptStore{
		ReceiptStore: receiptStore,
		directory:    directory,
	}, nil
}

func (s *temporaryReceiptStore) Close() error {
	s.closeOnce.Do(func() {
		s.closeErr = errors.Join(s.ReceiptStore.Close(), os.RemoveAll(s.directory))
	})
	return s.closeErr
}
