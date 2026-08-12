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

type SnapshotRevertWorkload struct {
	cfg           Config
	state         State
	signer        ethtypes.Signer
	accountCursor atomic.Uint64
}

var (
	// The outer runtime stores 1 at storage[CALLER], delegatecalls the helper
	// address encoded in calldata, ignores the helper's reverted status, and
	// returns successfully.
	snapshotRevertOuterRuntimeCode = common.FromHex("0x6001335560006000600060006000355af450600160005260206000f3")
	// The helper runtime runs under DELEGATECALL, overwrites storage[CALLER]
	// with 2 in the outer storage context, then executes REVERT.
	snapshotRevertHelperRuntimeCode = common.FromHex("0x6002335560006000fd")
)

func NewSnapshotRevertWorkload(cfg Config, state State) *SnapshotRevertWorkload {
	state.SetCode(cfg.SnapshotRevertContract, snapshotRevertOuterRuntimeCode)
	state.SetCode(cfg.SnapshotRevertHelper, snapshotRevertHelperRuntimeCode)
	return &SnapshotRevertWorkload{
		cfg:    cfg,
		state:  state,
		signer: ethtypes.LatestSignerForChainID(cfg.ChainID),
	}
}

func (w *SnapshotRevertWorkload) BuildBlock(ctx context.Context, number uint64) (evmonly.BlockRequest, error) {
	txs := make([][]byte, w.cfg.TxsPerBlock)
	for i := 0; i < w.cfg.TxsPerBlock; i++ {
		select {
		case <-ctx.Done():
			return evmonly.BlockRequest{}, ctx.Err()
		default:
		}
		accountIndex := w.accountCursor.Add(1)
		raw, sender, err := w.buildCallTx(accountIndex)
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

func (w *SnapshotRevertWorkload) buildCallTx(accountIndex uint64) ([]byte, common.Address, error) {
	key, err := DeterministicPrivateKey(accountIndex)
	if err != nil {
		return nil, common.Address{}, err
	}
	sender := crypto.PubkeyToAddress(key.PublicKey)
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    0,
		GasPrice: new(big.Int).Set(w.cfg.GasPrice),
		Gas:      w.cfg.TxGasLimit,
		To:       &w.cfg.SnapshotRevertContract,
		Value:    new(big.Int),
		Data:     addressCalldata(w.cfg.SnapshotRevertHelper),
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
func addressCalldata(addr common.Address) []byte {
	data := make([]byte, 32)
	copy(data[12:], addr.Bytes())
	return data
}
func SnapshotRevertStorageSlot(sender common.Address) common.Hash {
	return common.BytesToHash(sender.Bytes())
}
