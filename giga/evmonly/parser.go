package evmonly

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"golang.org/x/sync/errgroup"
)

// knownSenderFunc returns the already verified sender of the transaction with
// the given hash. A nil function knows no senders.
type knownSenderFunc func(common.Hash) (common.Address, bool)

func parseBlockTxs(ctx context.Context, txs [][]byte, signer ethtypes.Signer, known knownSenderFunc, workers int) ([]PreparedTx, error) {
	parsed := make([]PreparedTx, len(txs))
	if len(txs) == 0 {
		return parsed, nil
	}
	if workers <= 1 || len(txs) == 1 {
		for i, raw := range txs {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			prepared, err := parsePreparedTx(raw, signer, known)
			if err != nil {
				return nil, fmt.Errorf("parse tx %d: %w", i, err)
			}
			parsed[i] = prepared
		}
		return parsed, nil
	}
	workers = min(workers, len(txs))

	g, groupCtx := errgroup.WithContext(ctx)
	jobs := make(chan int)
	g.Go(func() error {
		defer close(jobs)
		for i := range txs {
			select {
			case jobs <- i:
			case <-groupCtx.Done():
				return groupCtx.Err()
			}
		}
		return nil
	})
	for range workers {
		g.Go(func() error {
			for i := range jobs {
				prepared, err := parsePreparedTx(txs[i], signer, known)
				if err != nil {
					return fmt.Errorf("parse tx %d: %w", i, err)
				}
				parsed[i] = prepared
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return parsed, nil
}

// parsePreparedTx decodes raw and resolves its sender. The sender is taken from
// known when it has an entry for the decoded transaction's hash, which binds the
// remembered sender to exactly these bytes; otherwise it is recovered from the
// signature.
func parsePreparedTx(raw []byte, signer ethtypes.Signer, known knownSenderFunc) (PreparedTx, error) {
	tx, err := decodeRawTx(raw)
	if err != nil {
		return PreparedTx{}, err
	}
	if err := validateSupportedTx(tx); err != nil {
		return PreparedTx{}, err
	}
	if known != nil {
		if sender, ok := known(tx.Hash()); ok {
			return PreparedTx{Tx: tx, Sender: sender}, nil
		}
	}
	sender, err := ethtypes.Sender(signer, tx)
	if err != nil {
		return PreparedTx{}, err
	}
	return PreparedTx{Tx: tx, Sender: sender}, nil
}

func parseTx(raw []byte, signer ethtypes.Signer) (*ethtypes.Transaction, common.Address, error) {
	tx, err := decodeRawTx(raw)
	if err != nil {
		return nil, common.Address{}, err
	}
	sender, err := ethtypes.Sender(signer, tx)
	if err != nil {
		return nil, common.Address{}, err
	}
	return tx, sender, nil
}

func decodeRawTx(raw []byte) (*ethtypes.Transaction, error) {
	tx := new(ethtypes.Transaction)
	if err := tx.UnmarshalBinary(raw); err != nil {
		return nil, err
	}
	return tx, nil
}
