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

// CLOBPlaceWorkload sends CLOBv1 placeNext calls with one market per transaction slot in a block.
// A block's transaction i goes to market i+1, from an account that trades only in that market, so
// no two transactions in a block touch the same book or balances. Each market's accounts fill one
// another's orders as the contract walks its repeating flow.
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
	code, err := clob.RuntimeCode()
	if err != nil {
		return nil, err
	}
	state.SetCode(cfg.CLOBContract, code)
	return &CLOBPlaceWorkload{
		cfg:    cfg,
		state:  state,
		signer: ethtypes.LatestSignerForChainID(cfg.ChainID),
	}, nil
}

// SeedAccountPool satisfies PoolSeeder. It funds every pool account; the contract credits an
// account's trading balances on its first call in a market.
func (w *CLOBPlaceWorkload) SeedAccountPool(ctx context.Context) error {
	_, err := seedAccountPool(ctx, w.cfg, func(addr common.Address) {
		w.state.SetBalance(addr, w.cfg.SenderBalance)
	})
	return err
}

func (w *CLOBPlaceWorkload) BuildBlock(ctx context.Context, number uint64) (evmonly.BlockRequest, error) {
	txs := make([][]byte, w.cfg.TxsPerBlock)
	base := senderRangeBase(w.cfg, number, &w.accountCursor)
	for i := 0; i < w.cfg.TxsPerBlock; i++ {
		select {
		case <-ctx.Done():
			return evmonly.BlockRequest{}, ctx.Err()
		default:
		}
		slot := uint64FromNonNegativeInt(i)
		raw, err := w.buildPlaceTx(w.cfg.senderFor(base+slot), CLOBMarket(slot))
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

// CLOBMarket returns the market a block's transaction slot trades in. Markets count from one,
// since market 0 names the sender's own market.
func CLOBMarket(slot uint64) uint64 {
	return slot + 1
}

func (w *CLOBPlaceWorkload) buildPlaceTx(slot senderSlot, market uint64) ([]byte, error) {
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
		Data:     clob.PlaceNextCalldata(market),
	})
	signed, err := ethtypes.SignTx(tx, w.signer, key)
	if err != nil {
		return nil, err
	}
	return signed.MarshalBinary()
}

// CLOBTrader returns the address of the pool account that sends a transaction slot's call in a round.
func CLOBTrader(cfg Config, round, slot uint64) (common.Address, error) {
	key, err := DeterministicPrivateKey(cfg.senderFor(round*uint64(cfg.TxsPerBlock) + slot).account) //nolint:gosec // txsPerBlock is validated positive
	if err != nil {
		return common.Address{}, err
	}
	return crypto.PubkeyToAddress(key.PublicKey), nil
}
