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

type ERC20TransferWorkload struct {
	cfg                  Config
	state                State
	signer               ethtypes.Signer
	conflictParticipants int
	accountCursor        atomic.Uint64
}

var (
	erc20TransferSelector = [4]byte{0xa9, 0x05, 0x9c, 0xbb}
	// Minimal ERC20-like runtime for transfer(address,uint256), with balances at
	// storage slot 0 and a standard Transfer(address,address,uint256) log.
	erc20TransferRuntimeCode = common.FromHex("0x60003560e01c63a9059cbb1460145760006000fd5b60243560043533600052600060205260406000208054831060805780548303905580600052600060205260406000208054830190558160005280337fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef60206000a3600160005260206000f35b60006000fd")
)

func NewERC20TransferWorkload(cfg Config, state State) *ERC20TransferWorkload {
	state.SetCode(cfg.ERC20Contract, erc20TransferRuntimeCode)
	return &ERC20TransferWorkload{
		cfg:                  cfg,
		state:                state,
		signer:               ethtypes.LatestSignerForChainID(cfg.ChainID),
		conflictParticipants: recipientConflictParticipants(cfg.TxsPerBlock, cfg.RecipientConflictRate),
	}
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
		w.state.SetBalance(sender, w.cfg.SenderBalance)
		w.state.SetState(w.cfg.ERC20Contract, ERC20BalanceSlot(sender), common.BigToHash(w.cfg.TransferValue))
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
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    0,
		GasPrice: new(big.Int).Set(w.cfg.GasPrice),
		Gas:      w.cfg.TxGasLimit,
		To:       &w.cfg.ERC20Contract,
		Value:    new(big.Int),
		Data:     erc20TransferCalldata(recipient, w.cfg.TransferValue),
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

func (w *ERC20TransferWorkload) Recipient(blockNumber uint64, txIndex int, accountIndex uint64) common.Address {
	return workloadRecipient(w.cfg, w.conflictParticipants, "sei-evmonly-loadtest-erc20-recipient", "sei-evmonly-loadtest-erc20-conflict-recipient", blockNumber, txIndex, accountIndex)
}
func erc20TransferCalldata(recipient common.Address, amount *big.Int) []byte {
	data := make([]byte, 4+32+32)
	copy(data[:4], erc20TransferSelector[:])
	copy(data[4+12:36], recipient.Bytes())
	amount.FillBytes(data[36:68])
	return data
}
func ERC20BalanceSlot(owner common.Address) common.Hash {
	var encoded [64]byte
	copy(encoded[12:32], owner.Bytes())
	return crypto.Keccak256Hash(encoded[:])
}
