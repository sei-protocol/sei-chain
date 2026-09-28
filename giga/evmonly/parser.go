package evmonly

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"golang.org/x/sync/errgroup"

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

// parseClaim is the number of transactions a worker takes per claim.
const parseClaim = 16

func parseBlockTxs(ctx context.Context, txs [][]byte, signer ethtypes.Signer, senders []utils.Option[common.Address], workers int) ([]PreparedTx, error) {
	parsed := make([]PreparedTx, len(txs))
	if len(txs) == 0 {
		return parsed, nil
	}
	if workers <= 1 || len(txs) == 1 {
		for i, raw := range txs {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			prepared, err := parsePreparedTx(raw, signer, senderAt(senders, i))
			if err != nil {
				return nil, fmt.Errorf("parse tx %d: %w", i, err)
			}
			parsed[i] = prepared
		}
		return parsed, nil
	}
	workers = min(workers, len(txs))

	g, groupCtx := errgroup.WithContext(ctx)
	var claimed atomic.Int64
	for range workers {
		g.Go(func() error {
			for {
				start := int(claimed.Add(parseClaim)) - parseClaim
				if start >= len(txs) {
					return nil
				}
				if err := groupCtx.Err(); err != nil {
					return err
				}
				for i := start; i < min(start+parseClaim, len(txs)); i++ {
					prepared, err := parsePreparedTx(txs[i], signer, senderAt(senders, i))
					if err != nil {
						return fmt.Errorf("parse tx %d: %w", i, err)
					}
					parsed[i] = prepared
				}
			}
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return parsed, nil
}

// parsePreparedTx decodes raw and resolves its sender. The sender is taken from
// known when present and signer would recover the same address; otherwise it is
// recovered from the signature.
func parsePreparedTx(raw []byte, signer ethtypes.Signer, known utils.Option[common.Address]) (PreparedTx, error) {
	tx, err := decodeRawTx(raw)
	if err != nil {
		return PreparedTx{}, err
	}
	if err := validateSupportedTx(tx); err != nil {
		return PreparedTx{}, err
	}
	if sender, ok := known.Get(); ok && signerAccepts(signer, tx) {
		return PreparedTx{Tx: tx, Sender: sender}, nil
	}
	sender, err := ethtypes.Sender(signer, tx)
	if err != nil {
		return PreparedTx{}, err
	}
	return PreparedTx{Tx: tx, Sender: sender}, nil
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
