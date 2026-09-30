package evmonly

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"runtime"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

const (
	// routeBlockTxs is how many transactions each block in the route benchmarks carries.
	routeBlockTxs = 2000
	// routeTxGas is each route transaction's gas limit; a block's gas limit fits all of them.
	routeTxGas = 150_000
)

// routeWork returns code that counts down from 2048 before the increment that follows it, so each call does
// about as much work as a typical contract call.
func routeWork() []byte {
	// PUSH2 0x0800, JUMPDEST, PUSH1 1, SWAP1, SUB, DUP1, PUSH1 3, JUMPI, POP
	return []byte{0x61, 0x08, 0x00, 0x5b, 0x60, 0x01, 0x90, 0x03, 0x80, 0x60, 0x03, 0x57, 0x50}
}

var (
	// routeSenderCounter does routeWork, then increments the caller's own slot: SSTORE(CALLER, SLOAD(CALLER) + 1).
	routeSenderCounter = testAddress(0xa1)
	// routeSlotCounter does routeWork, then increments the slot its calldata names after a four-byte selector:
	// SSTORE(CALLDATALOAD(4), SLOAD(CALLDATALOAD(4)) + 1).
	routeSlotCounter  = testAddress(0xa2)
	routeIncrementSel = []byte{0xd0, 0x9d, 0xe0, 0x8a}
)

// routeTx is one transaction of a route benchmark block: a sender, the contract it calls and the slot it
// names, which only routeSlotCounter reads.
type routeTx struct {
	sender int
	to     common.Address
	slot   uint64
}

// routeShape is a block the route benchmarks and tests run: every transaction calls the same increment
// function, and the shape decides whose slot it lands on.
type routeShape struct {
	name string
	txs  func(n int) []routeTx
}

func routeShapes() []routeShape {
	independent := func(i int) routeTx { return routeTx{sender: i, to: routeSenderCounter} }
	shapes := []routeShape{
		{"independent", func(n int) []routeTx {
			txs := make([]routeTx, n)
			for i := range txs {
				txs[i] = independent(i)
			}
			return txs
		}},
		{"one_hot_slot", func(n int) []routeTx {
			txs := make([]routeTx, n)
			for i := range txs {
				txs[i] = routeTx{sender: i, to: routeSlotCounter}
			}
			return txs
		}},
	}
	for _, k := range []int{2, 4, 16, 64, 256} {
		shapes = append(shapes, routeShape{fmt.Sprintf("hot_slots_%d", k), func(n int) []routeTx {
			txs := make([]routeTx, n)
			for i := range txs {
				txs[i] = routeTx{sender: i, to: routeSlotCounter, slot: uint64(i % k)} //nolint:gosec // i is non-negative.
			}
			return txs
		}})
	}
	shapes = append(shapes,
		routeShape{"one_sender", func(n int) []routeTx {
			txs := make([]routeTx, n)
			for i := range txs {
				txs[i] = routeTx{sender: 0, to: routeSenderCounter}
			}
			return txs
		}},
		routeShape{"independent_eighth_hot", func(n int) []routeTx {
			txs := make([]routeTx, n)
			for i := range txs {
				txs[i] = independent(i)
				if i%8 == 7 {
					txs[i] = routeTx{sender: i, to: routeSlotCounter}
				}
			}
			return txs
		}},
		// hot_prefix puts the first occDependencySample transactions on one hot slot and the rest on their
		// senders' own slots.
		routeShape{"hot_prefix", func(n int) []routeTx {
			txs := make([]routeTx, n)
			for i := range txs {
				txs[i] = independent(i)
				if i < occDependencySample {
					txs[i] = routeTx{sender: i, to: routeSlotCounter}
				}
			}
			return txs
		}},
		// hot_tail is hot_prefix reversed: the first occDependencySample transactions are independent and the
		// rest share one hot slot.
		routeShape{"hot_tail", func(n int) []routeTx {
			txs := make([]routeTx, n)
			for i := range txs {
				txs[i] = independent(i)
				if i >= occDependencySample {
					txs[i] = routeTx{sender: i, to: routeSlotCounter}
				}
			}
			return txs
		}},
	)
	return shapes
}

// buildRouteBlock signs the shape's transactions and returns the prepared block with its start state.
func buildRouteBlock(tb testing.TB, shape routeShape, n int) (PreparedBlock, *MemoryState) {
	tb.Helper()
	chainID := big.NewInt(testChainID)
	txs := shape.txs(n)
	keys := map[int]*ecdsa.PrivateKey{}
	nonces := map[int]uint64{}
	state := NewMemoryState()
	state.SetCode(routeSenderCounter, append(routeWork(), 0x33, 0x54, 0x60, 0x01, 0x01, 0x33, 0x55, 0x00))
	state.SetCode(routeSlotCounter, append(routeWork(), 0x60, 0x04, 0x35, 0x54, 0x60, 0x01, 0x01, 0x60, 0x04, 0x35, 0x55, 0x00))
	raw := make([][]byte, len(txs))
	for i, tx := range txs {
		key, ok := keys[tx.sender]
		if !ok {
			var err error
			key, err = crypto.GenerateKey()
			require.NoError(tb, err)
			keys[tx.sender] = key
			state.SetBalance(crypto.PubkeyToAddress(key.PublicKey), big.NewInt(1_000_000_000_000))
		}
		data := append(append([]byte(nil), routeIncrementSel...), common.BigToHash(new(big.Int).SetUint64(tx.slot)).Bytes()...)
		raw[i] = signLegacyTxWithGasPrice(tb, key, chainID, nonces[tx.sender], &tx.to, big.NewInt(0), data, routeTxGas, big.NewInt(1))
		nonces[tx.sender]++
	}
	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0)})
	defer executor.Close()
	blockCtx := blockContext(chainID)
	blockCtx.GasLimit = uint64(n) * routeTxGas //nolint:gosec // n is non-negative.
	prepared, err := executor.PrepareBlock(tb.Context(), BlockRequest{Context: blockCtx, Txs: raw})
	require.NoError(tb, err)
	return prepared, state
}

// BenchmarkOCCRoute times one block of each shape on every path a block can take after speculation, and on
// the path the executor chooses.
func BenchmarkOCCRoute(b *testing.B) {
	paths := []struct {
		name string
		path occPath
	}{
		{"auto", occPathAuto},
		{"frontier", occPathFrontier},
		{"sequential", occPathSequential},
		{"blockstm", occPathBlockSTM},
	}
	for _, shape := range routeShapes() {
		req, state := buildRouteBlock(b, shape, routeBlockTxs)
		for _, p := range paths {
			b.Run(shape.name+"/"+p.name, func(b *testing.B) {
				executor := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: runtime.GOMAXPROCS(0)})
				defer executor.Close()
				executor.occPath = p.path
				// Start each path without the garbage the previous one left.
				runtime.GC()
				b.ResetTimer()
				for range b.N {
					result, err := executor.executePreparedBlock(b.Context(), req, state)
					if err != nil {
						b.Fatal(err)
					}
					result.Release()
				}
			})
		}
	}
}
