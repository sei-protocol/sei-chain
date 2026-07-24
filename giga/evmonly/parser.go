package evmonly

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"golang.org/x/sync/errgroup"
)

func parseBlockTxs(ctx context.Context, txs [][]byte, signer ethtypes.Signer, workers int) ([]PreparedTx, error) {
	parsed := make([]PreparedTx, len(txs))
	if len(txs) == 0 {
		return parsed, nil
	}
	if workers <= 1 || len(txs) == 1 {
		for i, raw := range txs {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			prepared, err := parsePreparedTx(raw, signer)
			if err != nil {
				return nil, fmt.Errorf("parse tx %d: %w", i, err)
			}
			parsed[i] = prepared
		}
		return parsed, nil
	}
	if workers > len(txs) {
		workers = len(txs)
	}

	parseErrs := make([]error, len(txs))
	g, groupCtx := errgroup.WithContext(ctx)
	g.SetLimit(workers)
	for i, raw := range txs {
		i, raw := i, raw
		g.Go(func() error {
			if err := groupCtx.Err(); err != nil {
				return err
			}
			prepared, err := parsePreparedTx(raw, signer)
			if err != nil {
				parseErrs[i] = err
				return nil
			}
			parsed[i] = prepared
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	for i, err := range parseErrs {
		if err != nil {
			return nil, fmt.Errorf("parse tx %d: %w", i, err)
		}
	}
	return parsed, nil
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
