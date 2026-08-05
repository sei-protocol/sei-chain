package scenarios

import (
	"context"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
	loadoffline "github.com/sei-protocol/sei-load/generator/offline"
)

type TransferWorkload struct {
	cfg                  Config
	state                State
	scenario             loadoffline.Scenario
	conflictParticipants int
	accountCursor        atomic.Uint64
}

func NewTransferWorkload(cfg Config, state State) (*TransferWorkload, error) {
	scenario, err := loadoffline.NewScenario(loadoffline.Transfer, offlineConfig(cfg))
	if err != nil {
		return nil, err
	}
	if err := scenario.SetupGenesis(state); err != nil {
		return nil, err
	}
	return &TransferWorkload{
		cfg:                  cfg,
		state:                state,
		scenario:             scenario,
		conflictParticipants: recipientConflictParticipants(cfg.TxsPerBlock, cfg.RecipientConflictRate),
	}, nil
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
		w.scenario.SeedSender(w.state, sender)
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
	signed, err := w.scenario.BuildTransaction(key, nonce, recipient)
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

func offlineConfig(cfg Config) loadoffline.Config {
	return loadoffline.Config{
		ChainID:       cfg.ChainID,
		GasPrice:      cfg.GasPrice,
		SenderBalance: cfg.SenderBalance,
		TransferValue: cfg.TransferValue,
		GasLimit:      cfg.TxGasLimit,
		ERC20Contract: cfg.ERC20Contract,
	}
}
