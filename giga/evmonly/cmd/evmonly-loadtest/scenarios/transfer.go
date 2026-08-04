package scenarios

import (
	"context"
	"math/big"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
)

type TransferWorkload struct {
	cfg                  Config
	state                State
	signer               ethtypes.Signer
	conflictParticipants int
	accountCursor        atomic.Uint64
}

func NewTransferWorkload(cfg Config, state State) *TransferWorkload {
	return &TransferWorkload{
		cfg:                  cfg,
		state:                state,
		signer:               ethtypes.LatestSignerForChainID(cfg.ChainID),
		conflictParticipants: recipientConflictParticipants(cfg.TxsPerBlock, cfg.RecipientConflictRate),
	}
}

func (w *TransferWorkload) BuildBlock(ctx context.Context, number uint64) (evmonly.BlockRequest, error) {
	txs := make([][]byte, w.cfg.TxsPerBlock)
	for i := 0; i < w.cfg.TxsPerBlock; i++ {
		select {
		case <-ctx.Done():
			return evmonly.BlockRequest{}, ctx.Err()
		default:
		}
		accountIndex := w.accountCursor.Add(1)
		senderIndex := accountIndex
		nonce := uint64(0)
		if w.cfg.SameSender {
			senderIndex = number
			nonce = uint64(i) //nolint:gosec // i is bounded by txsPerBlock.
		}
		recipient := w.Recipient(number, i, accountIndex)
		raw, sender, err := w.buildTransferTx(senderIndex, nonce, recipient)
		if err != nil {
			return evmonly.BlockRequest{}, err
		}
		w.state.SetBalance(sender, w.cfg.SenderBalance)
		txs[i] = raw
	}
	return evmonly.BlockRequest{
		Context: BlockContext(w.cfg, number),
		Txs:     txs,
	}, nil
}

func (w *TransferWorkload) buildTransferTx(accountIndex, nonce uint64, recipient common.Address) ([]byte, common.Address, error) {
	key, err := DeterministicPrivateKey(accountIndex)
	if err != nil {
		return nil, common.Address{}, err
	}
	sender := crypto.PubkeyToAddress(key.PublicKey)
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    nonce,
		GasPrice: new(big.Int).Set(w.cfg.GasPrice),
		Gas:      w.cfg.TxGasLimit,
		To:       &recipient,
		Value:    new(big.Int).Set(w.cfg.TransferValue),
	})
	signed, err := ethtypes.SignTx(tx, w.signer, key)
	if err != nil {
		return nil, common.Address{}, err
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		return nil, common.Address{}, err
	}
	return raw, sender, nil
}

func (w *TransferWorkload) Recipient(blockNumber uint64, txIndex int, accountIndex uint64) common.Address {
	return workloadRecipient(w.cfg, w.conflictParticipants, "sei-evmonly-loadtest-recipient", "sei-evmonly-loadtest-conflict-recipient", blockNumber, txIndex, accountIndex)
}
