package clob_test

import (
	"crypto/ecdsa"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	"github.com/sei-protocol/sei-chain/giga/evmonly/cmd/evmonly-loadtest/scenarios/clob"
)

var (
	contractAddr = common.HexToAddress("0x000000000000000000000000000000000000c10b")
	chainID      = big.NewInt(1337)
)

type trader struct {
	key   *ecdsa.PrivateKey
	addr  common.Address
	nonce uint64
}

type harness struct {
	t        *testing.T
	state    *evmonly.MemoryState
	executor *evmonly.Executor
	store    *evmonly.MemoryStore
	signer   ethtypes.Signer
	height   uint64
	abi      abi.ABI
}

func newHarness(t *testing.T) *harness {
	state := evmonly.NewMemoryState()
	state.SetCode(contractAddr, clob.RuntimeCode())
	store := evmonly.NewMemoryStore(state)
	executor := evmonly.NewExecutor(evmonly.Config{MinGasPrice: big.NewInt(0), OCCWorkers: 4},
		evmonly.WithStore(store, store.EncodeChangeSet),
		evmonly.WithReceiptStore(evmonly.NewMemoryReceiptStore()))
	t.Cleanup(executor.Close)
	parsed, err := abi.JSON(strings.NewReader(clob.ABI()))
	require.NoError(t, err)
	return &harness{t: t, state: state, executor: executor, store: store,
		signer: ethtypes.LatestSignerForChainID(chainID), height: 1, abi: parsed}
}

func (h *harness) newTrader(seed byte) *trader {
	key, err := crypto.ToECDSA(crypto.Keccak256([]byte{seed}))
	require.NoError(h.t, err)
	addr := crypto.PubkeyToAddress(key.PublicKey)
	h.state.SetBalance(addr, big.NewInt(1e18))
	return &trader{key: key, addr: addr}
}

type call struct {
	from *trader
	data []byte
}

// execute runs calls, in order, as one block.
func (h *harness) execute(calls ...call) *evmonly.BlockResult {
	txs := make([][]byte, len(calls))
	for i, c := range calls {
		tx, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{
			Nonce: c.from.nonce, GasPrice: big.NewInt(0), Gas: 1_000_000, To: &contractAddr, Data: c.data,
		}), h.signer, c.from.key)
		require.NoError(h.t, err)
		c.from.nonce++
		txs[i], err = tx.MarshalBinary()
		require.NoError(h.t, err)
	}
	h.height++
	result, err := h.executor.ExecuteBlock(h.t.Context(), evmonly.BlockRequest{
		Context: evmonly.BlockContext{
			Number: h.height, Time: 1_700_000_000 + h.height, GasLimit: 1 << 40, ChainID: chainID,
			BaseFee: big.NewInt(0), BlobBaseFee: big.NewInt(0),
		},
		Txs: txs,
	})
	require.NoError(h.t, err)
	return result
}

func (h *harness) balance(market uint64, owner common.Address) [4]uint64 {
	view := h.store.OpenView()
	defer view.Close()
	baseSlot, quoteSlot := clob.BalanceSlots(market, owner)
	baseAvail, baseLocked := clob.UnpackBalance(view.GetStorage(contractAddr, baseSlot))
	quoteAvail, quoteLocked := clob.UnpackBalance(view.GetStorage(contractAddr, quoteSlot))
	return [4]uint64{baseAvail.Uint64(), baseLocked.Uint64(), quoteAvail.Uint64(), quoteLocked.Uint64()}
}

type fill struct {
	taker, maker      uint64
	price, qty        uint64
	takerLeft, makerL uint64
}

func (h *harness) fills(result *evmonly.BlockResult) []fill {
	var out []fill
	event := h.abi.Events["Fill"]
	for _, receipt := range result.Receipts {
		for _, log := range receipt.Logs {
			if log.Topics[0] != event.ID {
				continue
			}
			values, err := event.Inputs.NonIndexed().Unpack(log.Data)
			require.NoError(h.t, err)
			out = append(out, fill{
				taker:     log.Topics[2].Big().Uint64(),
				maker:     log.Topics[3].Big().Uint64(),
				price:     values[3].(*big.Int).Uint64(),
				qty:       values[4].(*big.Int).Uint64(),
				takerLeft: values[5].(*big.Int).Uint64(),
				makerL:    values[6].(*big.Int).Uint64(),
			})
		}
	}
	return out
}

func TestShardedCLOBOrderedSequence(t *testing.T) {
	const market = 7
	h := newHarness(t)
	a, b, c, d := h.newTrader(1), h.newTrader(2), h.newTrader(3), h.newTrader(4)

	credits := h.execute(
		call{a, clob.CreditCalldata(market, 100, 10_000)},
		call{b, clob.CreditCalldata(market, 100, 10_000)},
		call{c, clob.CreditCalldata(market, 100, 10_000)},
		call{d, clob.CreditCalldata(market, 100, 10_000)},
	)
	for _, tx := range credits.Txs {
		require.Equal(t, ethtypes.ReceiptStatusSuccessful, tx.Status)
	}

	result := h.execute(
		call{a, clob.PlaceCalldata(market, false, clob.LowPrice, 4)},   // id 1 rests ask 4@90
		call{a, clob.PlaceCalldata(market, false, clob.HighPrice, 3)},  // id 2 rests ask 3@100
		call{b, clob.PlaceCalldata(market, true, clob.HighPrice, 10)},  // id 3 takes 4@90 and 3@100, rests bid 3@100
		call{c, clob.PlaceCalldata(market, false, clob.LowPrice, 1)},   // id 4 takes 1 of id 3 at 100
		call{d, clob.PlaceCalldata(market, false, clob.HighPrice, 2)},  // id 5 takes the last 2 of id 3
		call{a, clob.PlaceCalldata(market, false, clob.HighPrice, 2)},  // id 6 rests ask 2@100
		call{c, clob.PlaceCalldata(market, false, clob.HighPrice, 2)},  // id 7 rests ask 2@100 behind id 6
		call{d, clob.PlaceCalldata(market, true, clob.HighPrice, 3)},   // id 8 takes id 6, then 1 of id 7
		call{b, clob.PlaceCalldata(market, true, clob.HighPrice, 1e6)}, // reverts: quote cannot cover the lock
		call{c, clob.CancelCalldata(market, 7)},                        // releases the last 1 of id 7
		call{d, clob.PlaceCalldata(market, true, clob.LowPrice, 1)},    // id 9 rests bid 1@90
	)
	for i, tx := range result.Txs {
		want := ethtypes.ReceiptStatusSuccessful
		if i == 8 {
			want = ethtypes.ReceiptStatusFailed
		}
		require.Equal(t, want, tx.Status, "tx %d", i)
	}
	require.Empty(t, result.Receipts[8].Logs)

	require.Equal(t, []fill{
		{taker: 3, maker: 1, price: 90, qty: 4, takerLeft: 6, makerL: 0},
		{taker: 3, maker: 2, price: 100, qty: 3, takerLeft: 3, makerL: 0},
		{taker: 4, maker: 3, price: 100, qty: 1, takerLeft: 0, makerL: 2},
		{taker: 5, maker: 3, price: 100, qty: 2, takerLeft: 0, makerL: 0},
		{taker: 8, maker: 6, price: 100, qty: 2, takerLeft: 1, makerL: 0},
		{taker: 8, maker: 7, price: 100, qty: 1, takerLeft: 0, makerL: 1},
	}, h.fills(result))

	// {base available, base locked, quote available, quote locked}
	require.Equal(t, [4]uint64{91, 0, 10_860, 0}, h.balance(market, a.addr))
	require.Equal(t, [4]uint64{110, 0, 9_040, 0}, h.balance(market, b.addr))
	require.Equal(t, [4]uint64{98, 0, 10_200, 0}, h.balance(market, c.addr))
	require.Equal(t, [4]uint64{101, 0, 9_810, 90}, h.balance(market, d.addr))

	view := h.store.OpenView()
	defer view.Close()
	require.Equal(t, common.BigToHash(big.NewInt(9)), view.GetStorage(contractAddr, clob.LastOrderIDSlot(market)))
}

// TestShardedCLOBMarketsAreIsolated checks that a place in one market neither sees nor writes
// another market's book or balances.
func TestShardedCLOBMarketsAreIsolated(t *testing.T) {
	h := newHarness(t)
	a, b := h.newTrader(1), h.newTrader(2)
	h.execute(call{a, clob.CreditCalldata(1, 10, 10_000)}, call{b, clob.CreditCalldata(2, 10, 10_000)})

	result := h.execute(
		call{a, clob.PlaceCalldata(1, false, clob.LowPrice, 5)},
		call{b, clob.PlaceCalldata(2, true, clob.HighPrice, 5)},
	)
	require.Empty(t, h.fills(result))
	require.Zero(t, result.OCCStats.RerunCount)
	require.Equal(t, [4]uint64{5, 5, 10_000, 0}, h.balance(1, a.addr))
	require.Equal(t, [4]uint64{10, 0, 9_500, 500}, h.balance(2, b.addr))
}
