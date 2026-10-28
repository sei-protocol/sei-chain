package scenarios

import (
	"context"
	"fmt"
	"math/big"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/giga/evmonly/cmd/evmonly-loadtest/scenarios/clob"
)

// CLOBCredit is the base and quote every pool account is credited with, in its market, at genesis.
const CLOBCredit = uint64(1_000_000_000_000_000)

// clobStep is one place call in a market's repeating order flow.
type clobStep struct {
	isBuy bool
	price uint64
	size  uint64
}

// clobCycle is the order flow every market repeats. Four makers rest asks across both levels, then
// a taker walks both levels, filling three of them and part of the fourth, and a second taker
// finishes the fourth. The book returns to empty, so it stays bounded however long a run is.
var clobCycle = []clobStep{
	{isBuy: false, price: clob.HighPrice, size: 3},
	{isBuy: false, price: clob.HighPrice, size: 3},
	{isBuy: false, price: clob.LowPrice, size: 4},
	{isBuy: false, price: clob.LowPrice, size: 4},
	{isBuy: true, price: clob.HighPrice, size: 12},
	{isBuy: true, price: clob.HighPrice, size: 2},
}

// CLOBPlaceWorkload sends place calls to a ShardedCLOB with one market per transaction slot in a
// block. Account a trades only in market a mod TxsPerBlock, and a block's transaction i goes to
// market i, so no two transactions in a block touch the same market's book or balances.
type CLOBPlaceWorkload struct {
	cfg           Config
	state         State
	signer        ethtypes.Signer
	accountCursor atomic.Uint64
}

func NewCLOBPlaceWorkload(cfg Config, state State) (*CLOBPlaceWorkload, error) {
	if cfg.Accounts == 0 || cfg.Accounts%uint64(cfg.TxsPerBlock) != 0 { //nolint:gosec // txsPerBlock is validated positive
		return nil, fmt.Errorf("clob workload needs an account pool that is a multiple of txs-per-block")
	}
	state.SetCode(cfg.CLOBContract, clob.RuntimeCode())
	return &CLOBPlaceWorkload{
		cfg:    cfg,
		state:  state,
		signer: ethtypes.LatestSignerForChainID(cfg.ChainID),
	}, nil
}

// SeedAccountPool satisfies PoolSeeder. It funds every pool account and credits it in its market.
func (w *CLOBPlaceWorkload) SeedAccountPool(ctx context.Context) error {
	markets := uint64(w.cfg.TxsPerBlock) //nolint:gosec // txsPerBlock is validated positive
	var account uint64
	_, err := seedAccountPool(ctx, w.cfg, func(addr common.Address) {
		w.state.SetBalance(addr, w.cfg.SenderBalance)
		base, quote := clob.BalanceSlots(account%markets, addr)
		w.state.SetState(w.cfg.CLOBContract, base, clob.PackBalance(CLOBCredit, 0))
		w.state.SetState(w.cfg.CLOBContract, quote, clob.PackBalance(CLOBCredit, 0))
		account++
	})
	return err
}

func (w *CLOBPlaceWorkload) BuildBlock(ctx context.Context, number uint64) (evmonly.BlockRequest, error) {
	txs := make([][]byte, w.cfg.TxsPerBlock)
	base := senderRangeBase(w.cfg, number, &w.accountCursor)
	round := number - w.firstBlockHeight()
	for i := 0; i < w.cfg.TxsPerBlock; i++ {
		select {
		case <-ctx.Done():
			return evmonly.BlockRequest{}, ctx.Err()
		default:
		}
		market := uint64FromNonNegativeInt(i)
		isBuy, price, size := CLOBStep(round, market)
		raw, err := w.buildPlaceTx(w.cfg.senderFor(base+market), market, isBuy, price, size)
		if err != nil {
			return evmonly.BlockRequest{}, err
		}
		txs[i] = raw
	}
	return evmonly.BlockRequest{
		Context: BlockContext(w.cfg, number),
		Txs:     txs,
	}, nil
}

// CLOBStep returns the place call market sends in the given round of a run. Markets are staggered
// through the cycle so every block carries the same mix of makers and takers.
func CLOBStep(round, market uint64) (isBuy bool, price, size uint64) {
	step := clobCycle[(round+market)%uint64(len(clobCycle))]
	return step.isBuy, step.price, step.size
}

func (w *CLOBPlaceWorkload) firstBlockHeight() uint64 {
	if w.cfg.FirstBlockHeight == 0 {
		return 1
	}
	return w.cfg.FirstBlockHeight
}

func (w *CLOBPlaceWorkload) buildPlaceTx(slot senderSlot, market uint64, isBuy bool, price, size uint64) ([]byte, error) {
	key, err := DeterministicPrivateKey(slot.account)
	if err != nil {
		return nil, err
	}
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    slot.nonce,
		GasPrice: new(big.Int).Set(w.cfg.GasPrice),
		Gas:      w.cfg.TxGasLimit,
		To:       &w.cfg.CLOBContract,
		Value:    new(big.Int),
		Data:     clob.PlaceCalldata(market, isBuy, price, size),
	})
	signed, err := ethtypes.SignTx(tx, w.signer, key)
	if err != nil {
		return nil, err
	}
	return signed.MarshalBinary()
}

// CLOBTrader returns the address of the pool account that sends market's transaction in a round.
func CLOBTrader(cfg Config, round, market uint64) (common.Address, error) {
	key, err := DeterministicPrivateKey(cfg.senderFor(round*uint64(cfg.TxsPerBlock) + market).account) //nolint:gosec // txsPerBlock is validated positive
	if err != nil {
		return common.Address{}, err
	}
	return crypto.PubkeyToAddress(key.PublicKey), nil
}
