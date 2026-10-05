package evmonly

import (
	"crypto/ecdsa"
	"encoding/binary"
	"fmt"
	"math/big"
	"math/rand/v2"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
)

const (
	occScenarioSpareSenders = 128
	occScenarioHot          = 6
	occScenarioCounters     = 6
	occScenarioTxGas        = 150_000
	occScenarioFundedWei    = 1_000_000_000_000_000_000
)

// occScenarioProfile sets a scenario's block size and how often a drawn transaction is an
// independent transfer rather than one that may depend on another.
type occScenarioProfile struct {
	name               string
	blockTxs           int
	independentPercent int
}

// occScenarioProfiles holds a dense profile, where most transactions may conflict, and a sparse one,
// where conflicts are far enough apart that a parallel pass after a rerun accepts a long run again.
var occScenarioProfiles = []occScenarioProfile{
	{name: "dense", blockTxs: 320, independentPercent: 0},
	{name: "sparse", blockTxs: 640, independentPercent: 98},
}

// occScenario generates seeded blocks whose transactions depend on each other: hot recipients,
// read-modify-write storage slots, reads of hot balances, same-sender nonce chains, senders funded
// earlier in the same block, contract creation, a create-and-destroy child, reverts, logs, and
// stale or skipped nonces that the executor rejects.
type occScenario struct {
	profile   occScenarioProfile
	chainID   *big.Int
	rng       *rand.Rand
	seed      uint64
	senders   []*ecdsa.PrivateKey
	nonces    []uint64
	hot       []common.Address
	counters  []common.Address
	readers   []common.Address
	logger    common.Address
	reverter  common.Address
	destroyer common.Address
	storer    common.Address
	poorCount int
	// nextSender rotates through the senders for independent transfers, so none chains within a block.
	nextSender int
}

func newOCCScenario(t testing.TB, profile occScenarioProfile, seed uint64) *occScenario {
	s := &occScenario{
		profile: profile,
		chainID: big.NewInt(testChainID),
		rng:     rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15)), //nolint:gosec // deterministic test input.
		seed:    seed,
	}
	for i := range profile.blockTxs + occScenarioSpareSenders {
		s.senders = append(s.senders, s.key(t, "sender", i))
	}
	s.nonces = make([]uint64, len(s.senders))
	for i := range occScenarioHot {
		s.hot = append(s.hot, s.address("hot", i))
	}
	for i := range occScenarioCounters {
		s.counters = append(s.counters, s.address("counter", i))
		s.readers = append(s.readers, s.address("reader", i))
	}
	s.logger = s.address("logger", 0)
	s.reverter = s.address("reverter", 0)
	s.destroyer = s.address("destroyer", 0)
	s.storer = s.address("storer", 0)
	return s
}

func (s *occScenario) label(kind string, i int) []byte {
	return binary.BigEndian.AppendUint64(fmt.Appendf(nil, "occ-equivalence/%s/%d/", kind, i), s.seed)
}

func (s *occScenario) key(t testing.TB, kind string, i int) *ecdsa.PrivateKey {
	key, err := crypto.ToECDSA(crypto.Keccak256(s.label(kind, i)))
	require.NoError(t, err)
	return key
}

func (s *occScenario) address(kind string, i int) common.Address {
	return common.BytesToAddress(crypto.Keccak256(s.label(kind, i)))
}

func (s *occScenario) genesis() *MemoryState {
	state := NewMemoryState()
	for _, key := range s.senders {
		state.SetBalance(crypto.PubkeyToAddress(key.PublicKey), big.NewInt(occScenarioFundedWei))
	}
	for i, hot := range s.hot {
		state.SetBalance(hot, big.NewInt(int64(i)+1))
	}
	slot := testHash(0x01)
	for i, counter := range s.counters {
		state.SetCode(counter, counterRuntime(slot))
		state.SetCode(s.readers[i], balanceStoreCode(s.hot[i%len(s.hot)], slot))
	}
	state.SetCode(s.logger, logRuntime(testHash(0x0a), []common.Hash{testHash(0x0b)}))
	state.SetCode(s.reverter, revertRuntime())
	state.SetCode(s.destroyer, createAndDestroyChildRuntime(s.hot[0], 1))
	state.SetBalance(s.destroyer, big.NewInt(1_000_000))
	state.SetCode(s.storer, storeOrDestroyCode(slot, testHash(0x33), s.hot[1]))
	return state
}

// block returns the transactions of block number, counted from 1.
func (s *occScenario) block(t testing.TB, number uint64) BlockRequest {
	ctx := blockContext(s.chainID)
	ctx.Number = number
	ctx.Time = number
	txs := make([][]byte, 0, s.profile.blockTxs)
	var deferred [][]byte
	for len(txs) < s.profile.blockTxs {
		if len(deferred) > 0 && s.rng.IntN(4) == 0 {
			txs, deferred = append(txs, deferred[0]), deferred[1:]
			continue
		}
		var later []byte
		txs, later = s.appendTx(t, txs, ctx.Coinbase)
		if later != nil {
			deferred = append(deferred, later)
		}
	}
	return BlockRequest{Context: ctx, Txs: append(txs, deferred...)}
}

// appendTx appends one drawn transaction, or a short run of them, and returns any transaction that
// has to land later in the block.
func (s *occScenario) appendTx(t testing.TB, txs [][]byte, coinbase common.Address) ([][]byte, []byte) {
	sender := s.rng.IntN(len(s.senders))
	independent := s.rng.IntN(100) < s.profile.independentPercent
	if independent {
		sender = s.nextSender
		s.nextSender = (s.nextSender + 1) % len(s.senders)
	}
	send := func(to *common.Address, value int64, data []byte) [][]byte {
		raw := signLegacyTxWithGas(t, s.senders[sender], s.chainID, s.nonces[sender], to, big.NewInt(value), data, occScenarioTxGas)
		s.nonces[sender]++
		return append(txs, raw)
	}
	if independent {
		to := s.address(fmt.Sprintf("fresh-%d", sender), int(s.nonces[sender])) //nolint:gosec // nonce is small.
		return send(&to, 1+s.rng.Int64N(1000), nil), nil
	}
	switch draw := s.rng.IntN(100); {
	case draw < 28:
		to := s.address(fmt.Sprintf("fresh-%d", sender), int(s.nonces[sender])) //nolint:gosec // nonce is small.
		return send(&to, 1+s.rng.Int64N(1000), nil), nil
	case draw < 42:
		return send(&s.hot[s.rng.IntN(len(s.hot))], 1+s.rng.Int64N(1000), nil), nil
	case draw < 56:
		return send(&s.counters[s.rng.IntN(len(s.counters))], 0, nil), nil
	case draw < 64:
		return send(&s.readers[s.rng.IntN(len(s.readers))], 0, nil), nil
	case draw < 68:
		return send(&s.logger, 0, nil), nil
	case draw < 71:
		return send(&s.reverter, 0, nil), nil
	case draw < 74:
		return send(&s.destroyer, 0, nil), nil
	case draw < 78:
		var data []byte
		if s.rng.IntN(3) == 0 {
			data = []byte{0x01}
		}
		return send(&s.storer, s.rng.Int64N(50), data), nil
	case draw < 82:
		return send(nil, 0, initCode(storeCode(testHash(0x02), testHash(byte(1+s.rng.IntN(255)))))), nil
	case draw < 84:
		return send(&coinbase, 1+s.rng.Int64N(1000), nil), nil
	case draw < 90:
		to := s.hot[s.rng.IntN(len(s.hot))]
		for range 2 + s.rng.IntN(3) {
			txs = send(&to, 1, nil)
		}
		return txs, nil
	case draw < 93:
		txs = send(&s.hot[0], 1, nil)
		stale := signLegacyTxWithGas(t, s.senders[sender], s.chainID, s.nonces[sender]-1, &s.hot[1], big.NewInt(1), nil, occScenarioTxGas)
		return append(txs, stale), nil
	case draw < 95:
		skipped := signLegacyTxWithGas(t, s.senders[sender], s.chainID, s.nonces[sender]+1, &s.hot[2], big.NewInt(1), nil, occScenarioTxGas)
		return append(txs, skipped), nil
	default:
		return s.appendFundedSpend(t, txs, sender)
	}
}

// appendFundedSpend funds a fresh account and returns its spend, which only succeeds once the
// funding transaction ahead of it has landed.
func (s *occScenario) appendFundedSpend(t testing.TB, txs [][]byte, sender int) ([][]byte, []byte) {
	poor := s.key(t, "poor", s.poorCount)
	s.poorCount++
	poorAddr := crypto.PubkeyToAddress(poor.PublicKey)
	fund := big.NewInt(occScenarioTxGas*testGasPriceWei + 1_000)
	txs = append(txs, signLegacyTxWithGas(t, s.senders[sender], s.chainID, s.nonces[sender], &poorAddr, fund, nil, occScenarioTxGas))
	s.nonces[sender]++
	spend := signLegacyTxWithGas(t, poor, s.chainID, 0, &s.hot[s.rng.IntN(len(s.hot))], big.NewInt(1_000), nil, occScenarioTxGas)
	return txs, spend
}

// counterRuntime increments the value at slot.
func counterRuntime(slot common.Hash) []byte {
	code := appendPush32(nil, slot)
	code = append(code, 0x54)
	code = appendPush1(code, 1)
	code = append(code, 0x01)
	code = appendStoreTop(code, slot)
	return append(code, 0x00)
}

// occScenarioRun executes a scenario's blocks in order on one executor configuration, carrying the
// state forward, and returns each block's result.
func occScenarioRun(t *testing.T, profile occScenarioProfile, seed uint64, blocks int, workers int) []*BlockResult {
	scenario := newOCCScenario(t, profile, seed)
	state := scenario.genesis()
	results := make([]*BlockResult, 0, blocks)
	for number := range uint64(blocks) { //nolint:gosec // blocks is small and positive.
		req := scenario.block(t, number+1)
		executor := NewExecutor(Config{MinGasPrice: big.NewInt(0), OCCWorkers: workers, RejectUnappliableTxs: true}, withTestState(state))
		result, err := executor.ExecuteBlock(t.Context(), req)
		executor.Close()
		require.NoError(t, err, "%s seed %d block %d workers %d", profile.name, seed, number+1, workers)
		state.ApplyChangeSet(result.ChangeSet)
		results = append(results, result)
	}
	return results
}

// requireSameStateChanges asserts that two changesets hold the same values in the same order and
// encode to the same FlatKV pairs. Balances compare by value, since a zero balance may be held with
// or without backing words.
func requireSameStateChanges(t *testing.T, store *flatkv.CommitStore, want StateChangeSet, got StateChangeSet, name string) {
	t.Helper()
	require.Equal(t, withCanonicalBalances(want), withCanonicalBalances(got), name)
	require.Equal(t, flatKVPairListing(t, store, want), flatKVPairListing(t, store, got), name)
}

func withCanonicalBalances(changes StateChangeSet) StateChangeSet {
	balances := make([]BalanceChange, len(changes.Balances))
	for i, change := range changes.Balances {
		balances[i] = change
		if change.Balance != nil {
			balances[i].Balance = new(big.Int).Set(change.Balance)
		}
	}
	changes.Balances = balances
	return changes
}

// Blocks with every kind of dependency the OCC path validates must produce exactly what the
// sequential executor produces, for several worker counts, so the shard ownership each count implies
// never shows in the output.
func TestOCCRandomizedConflictingBlocksMatchSequential(t *testing.T) {
	const seeds, blocks = 4, 3
	cfg := flatkvconfig.DefaultConfig()
	cfg.DataDir = t.TempDir()
	store, err := openFlatKVTestStore(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	for _, profile := range occScenarioProfiles {
		var reruns uint64
		for seed := range uint64(seeds) {
			sequential := occScenarioRun(t, profile, seed, blocks, 1)
			for _, workers := range []int{3, 8} {
				occ := occScenarioRun(t, profile, seed, blocks, workers)
				for i := range blocks {
					name := fmt.Sprintf("%s seed %d block %d workers %d", profile.name, seed, i+1, workers)
					want, got := sequential[i], occ[i]
					require.True(t, got.OCCStats.Attempted, name)
					require.False(t, got.OCCStats.Fallback, "%s: %s", name, got.OCCStats.FallbackReason)
					require.Equal(t, want.GasUsed, got.GasUsed, name)
					require.Equal(t, want.Txs, got.Txs, name)
					require.Equal(t, want.Receipts, got.Receipts, name)
					requireSameStateChanges(t, store, want.ChangeSet, got.ChangeSet, name)
					reruns += got.OCCStats.RerunCount
				}
			}
		}
		require.Positive(t, reruns, "%s: the scenario must make the OCC path rerun transactions", profile.name)
	}
}
