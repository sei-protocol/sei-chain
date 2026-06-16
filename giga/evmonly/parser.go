package evmonly

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

type parsedTx struct {
	tx     *ethtypes.Transaction
	sender common.Address
}

func parseBlockTxs(ctx context.Context, txs [][]byte, signer ethtypes.Signer) ([]parsedTx, error) {
	parsed := make([]parsedTx, len(txs))
	for i, raw := range txs {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		tx, sender, err := parseTx(raw, signer)
		if err != nil {
			return nil, fmt.Errorf("parse tx %d: %w", i, err)
		}
		parsed[i] = parsedTx{tx: tx, sender: sender}
	}
	return parsed, nil
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
