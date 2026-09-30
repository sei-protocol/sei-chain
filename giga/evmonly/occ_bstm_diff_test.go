package evmonly

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/big"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// bstmDiffTimeout bounds one block's execution; a block that has not returned by then is a phase that
// does not terminate.
const bstmDiffTimeout = 5 * time.Minute

// bstmDiffWorkers are the worker counts each block runs at under the Block-STM phase. A single worker
// takes the sequential path, which is the reference.
var bstmDiffWorkers = []int{2, 4, 16}

// bstmDiffAsm assembles EVM code with one-byte jump labels.
type bstmDiffAsm struct {
	code   []byte
	labels map[string]byte
	fixups map[int]string
}

func newBSTMDiffAsm() *bstmDiffAsm {
	return &bstmDiffAsm{labels: map[string]byte{}, fixups: map[int]string{}}
}

func (a *bstmDiffAsm) op(bytes ...byte) *bstmDiffAsm {
	a.code = append(a.code, bytes...)
	return a
}

// jumpTo pushes the offset of label.
func (a *bstmDiffAsm) jumpTo(label string) *bstmDiffAsm {
	a.code = append(a.code, 0x60, 0x00)
	a.fixups[len(a.code)-1] = label
	return a
}

// mark places label here as a JUMPDEST.
func (a *bstmDiffAsm) mark(label string) *bstmDiffAsm {
	if len(a.code) > 0xff {
		panic("code too long for one-byte labels")
	}
	a.labels[label] = byte(len(a.code))
	a.code = append(a.code, 0x5b)
	return a
}

func (a *bstmDiffAsm) bytes() []byte {
	for at, label := range a.fixups {
		offset, ok := a.labels[label]
		if !ok {
			panic("unknown label " + label)
		}
		a.code[at] = offset
	}
	return a.code
}

// linkCode returns its slot 0 when a contract calls it. Called by an account, it reads up's slot 0 through
// a static call and stores that value plus its own slot 0 plus one at its slot 0, so a call to it reads
// what the last call to up wrote.
func linkCode(up common.Address) []byte {
	a := newBSTMDiffAsm()
	a.op(0x32, 0x33, 0x14).jumpTo("store").op(0x57)                        // ORIGIN CALLER EQ -> store
	a.op(0x60, 0x00, 0x54, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3) // RETURN(SLOAD(0))
	a.mark("store")
	a.op(0x60, 0x20, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x73).op(up.Bytes()...).op(0x5a, 0xfa, 0x50) // STATICCALL up
	a.op(0x60, 0x00, 0x51, 0x60, 0x00, 0x54, 0x01, 0x60, 0x01, 0x01, 0x60, 0x00, 0x55, 0x00)          // SSTORE(0, up + SLOAD(0) + 1)
	return a.bytes()
}

// Modes of refundCode, chosen by the fifth calldata byte.
const (
	refundFlip = iota
	refundClear
	refundSetRestore
	refundRevert
	refundInvalidIfOdd
	refundModes
)

// refundCode works slot 0 in the mode its fifth calldata byte names: flip stores zero and then the
// value it read plus one, so the refund counter gains and loses the clearing refund when the slot started
// nonzero; clear stores zero; set-restore stores the value plus seven and then the value again; revert
// increments the slot and reverts; invalid-if-odd increments the slot when it is even and executes an
// invalid opcode, which consumes all gas, when it is odd.
func refundCode() []byte {
	a := newBSTMDiffAsm()
	a.op(0x60, 0x04, 0x35, 0x60, 0xf8, 0x1c) // CALLDATALOAD(4) >> 248
	for mode, label := range []string{"clear", "setRestore", "revert", "invalidIfOdd"} {
		a.op(0x80, 0x60, byte(mode+1), 0x14).jumpTo(label).op(0x57) // DUP1 PUSH1 mode EQ -> label
	}
	a.op(0x60, 0x00, 0x54, 0x60, 0x00, 0x60, 0x00, 0x55, 0x60, 0x01, 0x01, 0x60, 0x00, 0x55, 0x00)
	a.mark("clear").op(0x60, 0x00, 0x60, 0x00, 0x55, 0x00)
	a.mark("setRestore").op(0x60, 0x00, 0x54, 0x80, 0x60, 0x07, 0x01, 0x60, 0x00, 0x55, 0x60, 0x00, 0x55, 0x00)
	a.mark("revert").op(0x60, 0x00, 0x54, 0x60, 0x01, 0x01, 0x60, 0x00, 0x55, 0x60, 0x00, 0x60, 0x00, 0xfd)
	a.mark("invalidIfOdd").op(0x60, 0x00, 0x54, 0x80, 0x60, 0x01, 0x16).jumpTo("odd").op(0x57)
	a.op(0x60, 0x01, 0x01, 0x60, 0x00, 0x55, 0x00)
	a.mark("odd").op(0xfe)
	return a.bytes()
}

// bstmDiffKind is one kind of transaction a differential block is built from.
type bstmDiffKind int

const (
	diffHotSlot bstmDiffKind = iota
	diffLink
	diffRefund
	diffWithdrawThroughCaller
	diffWithdrawDirect
	diffDeposit
	diffBalanceRead
	diffTransfer
	diffKinds
)

// bstmDiffWorld is the contracts every differential block can call.
type bstmDiffWorld struct {
	hot, refund, vault, reader common.Address
	links                      [][]common.Address
	callers                    []common.Address
}

func newBSTMDiffWorld() bstmDiffWorld {
	w := bstmDiffWorld{hot: testAddress(0x41), refund: testAddress(0x42), vault: testAddress(0x43), reader: testAddress(0x44)}
	for chain := range 2 {
		var links []common.Address
		for k := range 12 {
			links = append(links, testAddress(byte(0x50+chain*0x10+k)))
		}
		w.links = append(w.links, links)
	}
	for i := range 8 {
		w.callers = append(w.callers, testAddress(byte(0x70+i)))
	}
	return w
}

func (w bstmDiffWorld) install(state *MemoryState, vaultBalance int64) {
	state.SetCode(w.hot, counterCode(testHash(0x01)))
	state.SetCode(w.refund, refundCode())
	state.SetState(w.refund, common.Hash{}, common.BigToHash(big.NewInt(3)))
	state.SetCode(w.vault, vaultCode())
	state.SetBalance(w.vault, big.NewInt(vaultBalance))
	state.SetCode(w.reader, balanceReaderCode(w.vault))
	for _, links := range w.links {
		up := testAddress(0x9f) // no code: the first link reads zero
		for _, link := range links {
			state.SetCode(link, linkCode(up))
			up = link
		}
	}
	for _, caller := range w.callers {
		state.SetCode(caller, countingWithdrawCode(w.vault))
	}
}

// bstmDiffBlock is a block and the state it starts from.
type bstmDiffBlock struct {
	newState func() *MemoryState
	req      BlockRequest
}

// bstmDiffShape says how to build a block: its length, the weight of each kind, the length of one
// sender's nonce chain, and the vault's starting balance.
type bstmDiffShape struct {
	txs          int
	weights      [diffKinds]int
	chain        int
	vaultBalance int64
}

// buildBSTMDiffBlock builds a block of shape from seed. Every transaction but the chain sender's comes
// from a sender of its own; the chain sender's transactions are spread through the block in nonce order.
func buildBSTMDiffBlock(t *testing.T, seed uint64, shape bstmDiffShape) bstmDiffBlock {
	t.Helper()
	chainID := big.NewInt(testChainID)
	world := newBSTMDiffWorld()
	rng := rand.New(rand.NewPCG(seed, 0x5eed))
	key := func() *ecdsa.PrivateKey {
		for {
			var raw [32]byte
			for i := range raw {
				raw[i] = byte(rng.Uint32())
			}
			if k, err := crypto.ToECDSA(raw[:]); err == nil {
				return k
			}
		}
	}
	total := 0
	for _, w := range shape.weights {
		total += w
	}
	selector := func() []byte {
		return []byte{0x10 + byte(rng.IntN(16)), 0xa0, 0x0b, 0x0c}
	}
	nextLink := make([]int, len(world.links))
	chainKey := key()
	chainAt := map[int]bool{}
	for len(chainAt) < shape.chain {
		chainAt[rng.IntN(shape.txs)] = true
	}
	var senders []common.Address
	senders = append(senders, crypto.PubkeyToAddress(chainKey.PublicKey))
	var rawTxs [][]byte
	chainNonce := uint64(0)
	for i := range shape.txs {
		from, nonce := chainKey, chainNonce
		if chainAt[i] {
			chainNonce++
		} else {
			from, nonce = key(), 0
			senders = append(senders, crypto.PubkeyToAddress(from.PublicKey))
		}
		pick := rng.IntN(total)
		kind := bstmDiffKind(0)
		for pick >= shape.weights[kind] {
			pick -= shape.weights[kind]
			kind++
		}
		var to common.Address
		value := big.NewInt(0)
		var data []byte
		switch kind {
		case diffHotSlot:
			to, data = world.hot, selector()
		case diffLink:
			c := rng.IntN(len(world.links))
			to, data = world.links[c][nextLink[c]], selector()
			nextLink[c] = (nextLink[c] + 1) % len(world.links[c])
		case diffRefund:
			to, data = world.refund, append(selector(), byte(rng.IntN(refundModes)))
		case diffWithdrawThroughCaller:
			to, data = world.callers[rng.IntN(len(world.callers))], common.BigToHash(big.NewInt(100+rng.Int64N(900))).Bytes()
		case diffWithdrawDirect:
			to, data = world.vault, common.BigToHash(big.NewInt(100+rng.Int64N(900))).Bytes()
		case diffDeposit:
			to, value = world.vault, big.NewInt(50+rng.Int64N(400))
		case diffBalanceRead:
			to = world.reader
		case diffTransfer:
			to, value = common.BigToAddress(big.NewInt(int64(0x7000_0000+i))), big.NewInt(1+rng.Int64N(1000))
		}
		rawTxs = append(rawTxs, signLegacyTxWithGasPrice(t, from, chainID, nonce, &to, value, data, 200_000, big.NewInt(1)))
	}
	newState := func() *MemoryState {
		state := NewMemoryState()
		world.install(state, shape.vaultBalance)
		for _, sender := range senders {
			state.SetBalance(sender, big.NewInt(1_000_000_000_000_000))
		}
		return state
	}
	return bstmDiffBlock{newState: newState, req: BlockRequest{Context: blockContext(chainID), Txs: rawTxs}}
}

// bstmDiffAppHash hashes the gas used and the encoded change set, the parts of the EVM-only app hash
// the executor decides.
func bstmDiffAppHash(t *testing.T, result *BlockResult) common.Hash {
	t.Helper()
	changesets, err := EncodeMemoryStoreChangeSet(result.ChangeSet)
	require.NoError(t, err)
	h := sha256.New()
	var word [8]byte
	sized := func(value []byte) {
		binary.BigEndian.PutUint64(word[:], uint64(len(value)))
		h.Write(word[:])
		h.Write(value)
	}
	binary.BigEndian.PutUint64(word[:], result.GasUsed)
	h.Write(word[:])
	for _, changeset := range changesets {
		sized([]byte(changeset.Name))
		for _, kv := range changeset.Changeset.Pairs {
			sized(kv.Key)
			if kv.Delete {
				h.Write([]byte{1})
			} else {
				h.Write([]byte{0})
			}
			sized(kv.Value)
		}
	}
	return common.BytesToHash(h.Sum(nil))
}

// executeBSTMDiffBlock executes block at workers on path, failing the test if it does not return within
// bstmDiffTimeout.
func executeBSTMDiffBlock(t *testing.T, block bstmDiffBlock, workers int, path occPath, withState func(StateReader) Option) *BlockResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), bstmDiffTimeout)
	defer cancel()
	type outcome struct {
		result *BlockResult
		err    error
	}
	done := make(chan outcome, 1)
	executor := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: workers}, withState(block.newState()))
	executor.occPath = path
	go func() {
		result, err := executor.ExecuteBlock(ctx, block.req)
		done <- outcome{result, err}
	}()
	select {
	case got := <-done:
		executor.Close()
		require.NoError(t, got.err)
		return got.result
	case <-time.After(bstmDiffTimeout + time.Minute):
		t.Fatalf("block at %d workers did not return within %s", workers, bstmDiffTimeout)
		return nil
	}
}

// TestExecutorOCCBlockSTMDifferential runs randomized and adverse blocks under the Block-STM phase and on
// the path the executor chooses at 2, 4 and 16 workers, and on the sequential path at 0 and 1 workers, over
// many seeds, and requires the same change set, transaction results, receipts, gas and app hash from every
// run, and every execution to return. The blocks are: every transaction writing one
// hot slot; one sender's long nonce chain among other calls; read-after-write chains across twelve
// contracts, each call reading what the last call to the contract below it wrote; calls that revert,
// execute an invalid opcode, and gain and lose storage-clearing refunds on one contended slot; native-coin
// withdrawals from a contract whose balance runs short partway through the block, beside deposits and
// readers of its balance; and mixes of all of these with conflict-free transfers.
func TestExecutorOCCBlockSTMDifferential(t *testing.T) {
	var phases atomic.Int64
	var lastMu sync.Mutex
	var last bstmCounts
	bstmObserve = func(counts bstmCounts) {
		lastMu.Lock()
		last = counts
		lastMu.Unlock()
		phases.Add(1)
	}
	t.Cleanup(func() { bstmObserve = nil })

	only := func(kind bstmDiffKind) (w [diffKinds]int) {
		w[kind] = 1
		return w
	}
	shapes := []struct {
		name  string
		seeds uint64
		// contended shapes must make the phase abort at sixteen workers.
		contended bool
		shape     func(rng *rand.Rand) bstmDiffShape
	}{
		{"hot slot", 16, true, func(*rand.Rand) bstmDiffShape {
			return bstmDiffShape{txs: 300, weights: only(diffHotSlot), vaultBalance: 1}
		}},
		{"nonce chain", 16, true, func(rng *rand.Rand) bstmDiffShape {
			return bstmDiffShape{txs: 300, chain: 150 + rng.IntN(100), weights: [diffKinds]int{diffHotSlot: 2, diffLink: 2, diffRefund: 2, diffTransfer: 2, diffDeposit: 1}, vaultBalance: 1}
		}},
		{"read-after-write chains", 16, true, func(*rand.Rand) bstmDiffShape {
			return bstmDiffShape{txs: 300, weights: only(diffLink), vaultBalance: 1}
		}},
		{"reverts, invalid opcodes and refunds", 24, true, func(*rand.Rand) bstmDiffShape {
			return bstmDiffShape{txs: 300, weights: only(diffRefund), vaultBalance: 1}
		}},
		{"vault runs short mid-block", 24, true, func(rng *rand.Rand) bstmDiffShape {
			return bstmDiffShape{txs: 300, weights: [diffKinds]int{diffWithdrawThroughCaller: 5, diffWithdrawDirect: 2, diffDeposit: 2, diffBalanceRead: 1}, vaultBalance: 20_000 + rng.Int64N(40_000)}
		}},
		{"mixed with conflict-free transfers", 64, false, func(rng *rand.Rand) bstmDiffShape {
			var w [diffKinds]int
			for k := range w {
				w[k] = rng.IntN(4)
			}
			w[diffTransfer] += 2 + rng.IntN(6)
			// Keep calls to contracts at least half the block.
			calls := w[diffHotSlot] + w[diffLink] + w[diffRefund] + w[diffWithdrawThroughCaller] + w[diffWithdrawDirect]
			w[diffLink] += max(1, w[diffTransfer]+w[diffDeposit]+w[diffBalanceRead]-calls)
			chain := 0
			if rng.IntN(2) == 0 {
				chain = 20 + rng.IntN(120)
			}
			return bstmDiffShape{txs: 200 + rng.IntN(300), chain: chain, weights: w, vaultBalance: 5_000 + rng.Int64N(80_000)}
		}},
	}
	for i, s := range shapes {
		seeds := s.seeds
		if testing.Short() {
			seeds = 2
		}
		for seed := range seeds {
			seed := uint64(i)<<32 | seed
			withState := withTestState
			if seed%2 == 1 {
				withState = withRowReadingTestState
			}
			t.Run(fmt.Sprintf("%s/seed=%d", s.name, seed&0xffffffff), func(t *testing.T) {
				shape := s.shape(rand.New(rand.NewPCG(seed, 0x5a4e)))
				block := buildBSTMDiffBlock(t, seed, shape)
				want := executeBSTMDiffBlock(t, block, 0, occPathAuto, withState)
				wantHash := bstmDiffAppHash(t, want)
				one := executeBSTMDiffBlock(t, block, 1, occPathAuto, withState)
				requireBSTMDiffEqual(t, want, wantHash, one, "sequential at 1 worker")
				for _, workers := range bstmDiffWorkers {
					routed := executeBSTMDiffBlock(t, block, workers, occPathAuto, withState)
					requireBSTMDiffEqual(t, want, wantHash, routed, fmt.Sprintf("the chosen path at %d workers", workers))
					before := phases.Load()
					got := executeBSTMDiffBlock(t, block, workers, occPathBlockSTM, withState)
					require.Equal(t, before+1, phases.Load(), "the block runs under Block-STM at %d workers", workers)
					requireBSTMDiffEqual(t, want, wantHash, got, fmt.Sprintf("Block-STM at %d workers", workers))
					lastMu.Lock()
					aborts := last.estimateAborts + last.validationAborts
					lastMu.Unlock()
					if s.contended && workers == 16 {
						require.NotZero(t, aborts, "the block contends under Block-STM")
					}
				}
			})
		}
	}
}

func requireBSTMDiffEqual(t *testing.T, want *BlockResult, wantHash common.Hash, got *BlockResult, what string) {
	t.Helper()
	require.Equal(t, want.GasUsed, got.GasUsed, what)
	require.Equal(t, want.Txs, got.Txs, what)
	require.Equal(t, want.Receipts, got.Receipts, what)
	requireChangeSetsEqualByValue(t, want.ChangeSet, got.ChangeSet)
	require.Equal(t, wantHash, bstmDiffAppHash(t, got), what)
}

// vaultCode keeps any value sent to it with empty calldata and, when called with a word of calldata,
// sends its caller that many wei, as a wrapped native token's withdraw does. A send it cannot cover
// fails without reverting the call.
func vaultCode() []byte {
	return []byte{
		0x36, 0x15, 0x60, 0x15, 0x57, // CALLDATASIZE ISZERO PUSH1 0x15 JUMPI
		0x60, 0x00, 0x60, 0x00, 0x60, 0x00, 0x60, 0x00, // retSize retOffset argsSize argsOffset
		0x60, 0x00, 0x35, 0x33, 0x5a, 0xf1, 0x50, 0x00, // CALLDATALOAD(0) CALLER GAS CALL POP STOP
		0x5b, 0x00, // JUMPDEST STOP
	}
}

// balanceReaderCode stores hub's balance at the slot named by its caller.
func balanceReaderCode(hub common.Address) []byte {
	code := append([]byte{0x73}, hub.Bytes()...)
	return append(code, 0x31, 0x33, 0x55, 0x00)
}

// countingWithdrawCode increments its own slot 0 and then passes its calldata word to vault as a
// withdrawal, keeping the value vault sends back.
func countingWithdrawCode(vault common.Address) []byte {
	code := []byte{0x34, 0x60, 0x37, 0x57}                                    // CALLVALUE PUSH1 0x37 JUMPI: a send from vault stops at once
	code = append(code, 0x60, 0x00, 0x54, 0x60, 0x01, 0x01, 0x60, 0x00, 0x55) // SSTORE(0, SLOAD(0) + 1)
	code = append(code, 0x60, 0x20, 0x60, 0x00, 0x60, 0x00, 0x37)             // CALLDATACOPY(0, 0, 32)
	code = append(code, 0x60, 0x00, 0x60, 0x00, 0x60, 0x20, 0x60, 0x00, 0x60, 0x00, 0x73)
	code = append(code, vault.Bytes()...)
	code = append(code, 0x5a, 0xf1, 0x50) // GAS CALL POP
	return append(code, 0x00, 0x5b, 0x00) // STOP JUMPDEST STOP
}

// requireChangeSetsEqualByValue requires two change sets to be equal, comparing balances by value: a
// balance that ends at zero can be held with or without backing words.
func requireChangeSetsEqualByValue(t *testing.T, want StateChangeSet, got StateChangeSet) {
	t.Helper()
	require.Len(t, got.Balances, len(want.Balances))
	for i := range want.Balances {
		require.Equal(t, want.Balances[i].Address, got.Balances[i].Address)
		require.Zerof(t, want.Balances[i].Balance.Cmp(got.Balances[i].Balance), "balance of %s", want.Balances[i].Address)
	}
	want.Balances, got.Balances = nil, nil
	require.Equal(t, want, got)
}

// coinbaseBranchCode stores the coinbase's balance at the slot named by its caller, and increments slot 1
// when the balance is odd and slot 2 when it is even.
func coinbaseBranchCode(coinbase common.Address) []byte {
	a := newBSTMDiffAsm()
	a.op(0x73).op(coinbase.Bytes()...).op(0x31, 0x80, 0x33, 0x55)    // BALANCE(coinbase) DUP1 SSTORE(CALLER, balance)
	a.op(0x60, 0x01, 0x16).jumpTo("odd").op(0x57)                    // balance & 1 -> odd
	a.op(0x60, 0x02, 0x54, 0x60, 0x01, 0x01, 0x60, 0x02, 0x55, 0x00) // SSTORE(2, SLOAD(2) + 1)
	a.mark("odd").op(0x60, 0x01, 0x54, 0x60, 0x01, 0x01, 0x60, 0x01, 0x55, 0x00)
	return a.bytes()
}

// TestExecutorOCCBlockSTMCoinbaseTransferAndFee runs blocks whose first transaction, and every seventh
// after it, sends value to the coinbase and pays a fee, so it writes the coinbase's balance and credits it a
// fee in one transaction, while every other transaction pays a fee and reads the coinbase's balance and
// branches and stores on it. The coinbase is the zero address, as on Sei, or the tests' usual one. It
// requires Block-STM at 2, 4 and 16 workers to give the sequential path's results, so a reader never counts
// the writer's fee on top of the balance the writer wrote.
func TestExecutorOCCBlockSTMCoinbaseTransferAndFee(t *testing.T) {
	var phases atomic.Int64
	bstmObserve = func(bstmCounts) { phases.Add(1) }
	t.Cleanup(func() { bstmObserve = nil })
	chainID := big.NewInt(testChainID)
	for _, coinbase := range []common.Address{{}, blockContext(chainID).Coinbase} {
		t.Run(coinbase.Hex(), func(t *testing.T) {
			reader := testAddress(0x45)
			rng := rand.New(rand.NewPCG(uint64(coinbase[19]), 0xc0))
			var senders []common.Address
			var rawTxs [][]byte
			for i := range 200 {
				key, err := crypto.GenerateKey()
				require.NoError(t, err)
				senders = append(senders, crypto.PubkeyToAddress(key.PublicKey))
				to, value := reader, big.NewInt(0)
				if i%7 == 0 {
					to, value = coinbase, big.NewInt(1+rng.Int64N(1000))
				}
				rawTxs = append(rawTxs, signLegacyTxWithGasPrice(t, key, chainID, 0, &to, value, nil, 200_000, big.NewInt(1+rng.Int64N(3))))
			}
			ctx := blockContext(chainID)
			ctx.Coinbase = coinbase
			block := bstmDiffBlock{
				newState: func() *MemoryState {
					state := NewMemoryState()
					state.SetCode(reader, coinbaseBranchCode(coinbase))
					state.SetBalance(coinbase, big.NewInt(1_000_001))
					for _, sender := range senders {
						state.SetBalance(sender, big.NewInt(1_000_000_000_000_000))
					}
					return state
				},
				req: BlockRequest{Context: ctx, Txs: rawTxs},
			}

			// The first transaction both writes the coinbase's balance and credits it a fee.
			executor := NewExecutor(Config{MinGasPrice: big.NewInt(0)})
			prepared, err := executor.PrepareBlock(t.Context(), block.req)
			require.NoError(t, err)
			executor.Close()
			first := speculate(t, prepared, block.newState())[0]
			require.NoError(t, first.err)
			require.Contains(t, first.writeSet, stateAccessKey{kind: stateAccessBalance, address: coinbase})
			require.Positive(t, first.commutativeBalanceDeltas[coinbase].Sign())

			want := executeBSTMDiffBlock(t, block, 0, occPathAuto, withTestState)
			wantHash := bstmDiffAppHash(t, want)
			for _, workers := range bstmDiffWorkers {
				before := phases.Load()
				got := executeBSTMDiffBlock(t, block, workers, occPathBlockSTM, withTestState)
				require.Equal(t, before+1, phases.Load(), "the block runs under Block-STM at %d workers", workers)
				requireBSTMDiffEqual(t, want, wantHash, got, fmt.Sprintf("Block-STM at %d workers", workers))
			}
		})
	}
}
