package scenarios

import (
	"context"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
	loadoffline "github.com/sei-protocol/sei-load/generator/offline"
)

type ERC20TransferWorkload struct {
	cfg                  Config
	state                State
	scenario             loadoffline.Scenario
	conflictParticipants int
	accountCursor        atomic.Uint64
}

func NewERC20TransferWorkload(cfg Config, state State) (*ERC20TransferWorkload, error) {
	scenario, err := loadoffline.NewScenario(loadoffline.ERC20Transfer, offlineConfig(cfg))
	if err != nil {
		return nil, err
	}
	if err := scenario.SetupGenesis(state); err != nil {
		return nil, err
	}
	return &ERC20TransferWorkload{
		cfg:                  cfg,
		state:                state,
		scenario:             scenario,
		conflictParticipants: recipientConflictParticipants(cfg.TxsPerBlock, cfg.RecipientConflictRate),
	}, nil
}

func (w *ERC20TransferWorkload) BuildBlock(ctx context.Context, number uint64) (evmonly.BlockRequest, error) {
	txs := make([][]byte, w.cfg.TxsPerBlock)
	for i := 0; i < w.cfg.TxsPerBlock; i++ {
		select {
		case <-ctx.Done():
			return evmonly.BlockRequest{}, ctx.Err()
		default:
		}
		accountIndex := w.accountCursor.Add(1)
		recipient := w.Recipient(number, i, accountIndex)
		raw, sender, err := w.buildTransferTx(accountIndex, recipient)
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

func (w *ERC20TransferWorkload) buildTransferTx(accountIndex uint64, recipient common.Address) ([]byte, common.Address, error) {
	key, err := DeterministicPrivateKey(accountIndex)
	if err != nil {
		return nil, common.Address{}, err
	}
	sender := crypto.PubkeyToAddress(key.PublicKey)
	signed, err := w.scenario.BuildTransaction(key, 0, recipient)
	if err != nil {
		return nil, common.Address{}, err
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		return nil, common.Address{}, err
	}
	return raw, sender, nil
}

func (w *ERC20TransferWorkload) Recipient(blockNumber uint64, txIndex int, accountIndex uint64) common.Address {
	return workloadRecipient(w.cfg, w.conflictParticipants, "sei-evmonly-loadtest-erc20-recipient", "sei-evmonly-loadtest-erc20-conflict-recipient", blockNumber, txIndex, accountIndex)
}

func ERC20BalanceSlot(owner common.Address) common.Hash {
	return loadoffline.ERC20BalanceSlot(owner)
}
