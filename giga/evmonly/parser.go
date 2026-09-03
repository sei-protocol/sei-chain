package evmonly

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"golang.org/x/sync/errgroup"
)

func parseBlockTxs(
	ctx context.Context,
	txs [][]byte,
	signer ethtypes.Signer,
	workers int,
	lookup PreparedTxLookup,
) ([]PreparedTx, error) {
	parsed := make([]PreparedTx, len(txs))
	if len(txs) == 0 {
		return parsed, nil
	}
	if workers <= 1 || len(txs) == 1 {
		for i, raw := range txs {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			prepared, err := lookupOrParsePreparedTx(raw, signer, lookup)
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
				prepared, err := lookupOrParsePreparedTx(txs[i], signer, lookup)
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

func lookupOrParsePreparedTx(raw []byte, signer ethtypes.Signer, lookup PreparedTxLookup) (PreparedTx, error) {
	if lookup != nil {
		hash := crypto.Keccak256Hash(raw)
		if prepared, ok := lookup(hash); ok && prepared.Tx != nil && prepared.Tx.Hash() == hash {
			if err := validateSupportedTx(prepared.Tx); err != nil {
				return PreparedTx{}, err
			}
			return prepared, nil
		}
	}
	return parsePreparedTx(raw, signer)
}

func parsePreparedTx(raw []byte, signer ethtypes.Signer) (PreparedTx, error) {
	tx, sender, err := parseTx(raw, signer)
	if err != nil {
		return PreparedTx{}, err
	}
	if err := validateSupportedTx(tx); err != nil {
		return PreparedTx{}, err
	}
	return PreparedTx{Tx: tx, Sender: sender}, nil
}

func parseTx(raw []byte, signer ethtypes.Signer) (*ethtypes.Transaction, common.Address, error) {
	var tx ethtypes.Transaction
	if err := tx.UnmarshalBinary(raw); err != nil {
		return nil, common.Address{}, err
	}
	sender, err := ethtypes.Sender(signer, &tx)
	if err != nil {
		return nil, common.Address{}, err
	}
	return &tx, sender, nil
}
