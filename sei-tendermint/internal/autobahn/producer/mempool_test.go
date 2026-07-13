package producer

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/memblock"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

type txSpec struct {
	Address         common.Address
	Nonce           uint64
	GasWanted       uint64
	GasEstimated    uint64
	RequiredBalance uint64
	ShouldFail      bool
	EVMHash         common.Hash
	Payload         [32]byte
}

func newTestDataState(registry *epoch.Registry) *data.State {
	s, err := data.NewState(&data.Config{Registry: registry}, memblock.NewBlockDB())
	if err != nil {
		panic(fmt.Sprintf("data.NewState: %v", err))
	}
	return s
}

func (tx *txSpec) encode() []byte {
	return utils.OrPanic1(binary.Append(nil, binary.BigEndian, tx))
}

func decodeTxSpec(data []byte) (*txSpec, error) {
	var tx txSpec
	n, err := binary.Decode(data, binary.BigEndian, &tx)
	if err != nil {
		return nil, err
	}
	if len(data) != n {
		return nil, fmt.Errorf("bad length")
	}
	return &tx, nil
}

func (tx *txSpec) asResponse() *abci.ResponseCheckTxV2 {
	return &abci.ResponseCheckTxV2{
		ResponseCheckTx: &abci.ResponseCheckTx{
			Code:         abci.CodeTypeOK,
			GasWanted:    int64(tx.GasWanted),
			GasEstimated: int64(tx.GasEstimated),
		},
		IsEVM:              true,
		EVMNonce:           tx.Nonce,
		EVMHash:            tx.EVMHash,
		EVMSenderAddress:   tx.Address,
		SeiSenderAddress:   tx.Address[:],
		EVMRequiredBalance: *uint256.NewInt(tx.RequiredBalance),
	}
}

func (env *testEnv) genTx(rng utils.Rng, addr common.Address, nonce uint64) *txSpec {
	gasBase := int(env.state.cfg.MaxGasWantedPerBlock / env.state.cfg.maxTxsPerBlock())
	gasJitter := min(gasBase, 10)
	return &txSpec{
		Address: addr,
		Nonce:   nonce,
		// We randomize the gas in a way that both gas wanted and tx count limit have a chance of being exercised.
		GasWanted:    min(env.state.cfg.MaxGasWantedPerBlock, uint64(gasBase-gasJitter+rng.Intn(2*gasJitter))),
		GasEstimated: uint64(rng.Int63n(int64(env.state.cfg.MaxGasEstimatedPerBlock))),
		EVMHash:      common.Hash(utils.GenBytes(rng, len(common.Hash{}))),
		Payload:      [32]byte(utils.GenBytes(rng, 32)),
	}
}

type testAppInner struct {
	nonces  map[common.Address]uint64
	appHash types.AppHash
}

// Application tracking evm nonces.
type testApp struct {
	abci.BaseApplication
	inner utils.Mutex[*testAppInner]
}

func newTestApp() *testApp {
	return &testApp{
		inner: utils.NewMutex(&testAppInner{
			nonces: map[common.Address]uint64{},
		}),
	}
}

func (a *testApp) NewAccount(rng utils.Rng) (common.Address, uint64) {
	addr := common.Address(utils.GenBytes(rng, len(common.Address{})))
	nonce := uint64(rng.Intn(10000))
	for inner := range a.inner.Lock() {
		inner.nonces[addr] = nonce
	}
	return addr, nonce
}

func (a *testApp) Cfg() *Config {
	return &Config{
		MaxGasWantedPerBlock:    1000000,
		MaxGasEstimatedPerBlock: 1000000,
		MaxTxsPerBlock:          types.MaxTxsPerBlock,
		BlockInterval:           time.Hour,
	}
}

func (a *testApp) Proxy() *proxy.Proxy {
	return proxy.New(a)
}

func (a *testApp) EvmNonce(addr common.Address) uint64 {
	for inner := range a.inner.Lock() {
		return inner.nonces[addr]
	}
	panic("unreachable")
}

func (a *testApp) CheckTx(_ context.Context, req *abci.RequestCheckTxV2) *abci.ResponseCheckTxV2 {
	tx, err := decodeTxSpec(req.Tx)
	if err != nil {
		return &abci.ResponseCheckTxV2{
			ResponseCheckTx: &abci.ResponseCheckTx{
				Code:      1,
				Codespace: "some codespace",
				Log:       err.Error(),
			},
		}
	}
	return tx.asResponse()
}

func (a *testApp) FinalizeBlock(_ context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	for inner := range a.inner.Lock() {
		for _, txRaw := range req.Txs {
			tx, err := decodeTxSpec(txRaw)
			if err != nil {
				return nil, fmt.Errorf("decodeTxSpec(): %w", err)
			}
			if inner.nonces[tx.Address] == tx.Nonce && !tx.ShouldFail {
				inner.nonces[tx.Address] += 1
			}
		}
		h := sha256.Sum256(slices.Concat(req.Hash, inner.appHash[:]))
		inner.appHash = h[:]
		return &abci.ResponseFinalizeBlock{AppHash: inner.appHash}, nil
	}
	panic("unreachable")
}

type testEnvInner struct {
	sequenced map[common.Address][]*txSpec
}

// Single node consensus network for testing mempool behavior.
type testEnv struct {
	state     *State
	consensus *consensus.State
	data      *data.State
	app       *proxy.Proxy

	inner utils.Mutex[*testEnvInner]
}

func (env *testEnv) Run(ctx context.Context) error {
	return utils.IgnoreCancel(scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return env.data.Run(ctx) })
		s.Spawn(func() error { return env.consensus.Run(ctx) })
		s.Spawn(func() error { return consensus.RunTestNetwork(ctx, utils.Slice(env.consensus)) })
		s.Spawn(func() error { return env.state.Run(ctx) })
		// Process blocks.
		stats := blockStats{}
		firstBlock := env.data.Registry().FirstBlock()
		for i := firstBlock; ; i += 1 {
			// Wait for the next block to be finalized.
			b, err := env.data.GlobalBlock(ctx, i)
			if err != nil {
				return fmt.Errorf("env.data.GlobalBlock(): %w", err)
			}

			// Check that adding first transaction to the previous block would exceed the limit.
			if i > firstBlock {
				tx, err := decodeTxSpec(b.Payload.Txs()[0])
				if err != nil {
					return fmt.Errorf("decodeTxSpec(): %w", err)
				}
				if stats.Push(tx, env.state.cfg) {
					return fmt.Errorf("block sealed too early")
				}
			}

			// Check that block does not exceed limits.
			stats = blockStats{}
			for _, txRaw := range b.Payload.Txs() {
				tx, err := decodeTxSpec(txRaw)
				if err != nil {
					return fmt.Errorf("decodeTxSpec(): %w", err)
				}
				for inner := range env.inner.Lock() {
					inner.sequenced[tx.Address] = append(inner.sequenced[tx.Address], tx)
				}
				if !stats.Push(tx, env.state.cfg) {
					return fmt.Errorf("block sealed too late")
				}
			}

			// Mark block as executed.
			h := b.Header.Hash()
			resp, err := env.app.FinalizeBlock(ctx, &abci.RequestFinalizeBlock{Txs: b.Payload.Txs(), Hash: h[:]})
			if err != nil {
				return fmt.Errorf("app.FinalizeBlock(): %w", err)
			}
			if err := env.data.PushAppHash(ctx, i, resp.AppHash); err != nil {
				return err
			}
		}
	}))
}

func newTestEnv(rng utils.Rng, cfg *Config, app *proxy.Proxy) *testEnv {
	registry, keys := epoch.GenRegistry(rng, 1)
	dataState := newTestDataState(registry)
	consensusState := utils.OrPanic1(consensus.NewState(&consensus.Config{
		Key:                keys[0],
		ViewTimeout:        func(types.View) time.Duration { return time.Hour },
		PersistentStateDir: utils.None[string](),
	}, dataState))
	return &testEnv{
		data:      dataState,
		consensus: consensusState,
		state:     NewState(cfg, consensusState, app),
		app:       app,
		inner: utils.NewMutex(&testEnvInner{
			sequenced: map[common.Address][]*txSpec{},
		}),
	}
}

func TestInsertTx_TooLargeTx(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	app := newTestApp()
	env := newTestEnv(rng, app.Cfg(), app.Proxy())
	// Tx with size exceeding block limit.
	tx := utils.GenBytes(rng, int(types.MaxTxsBytesPerBlock+1))
	// Should be rejected by mempool.
	_, err := env.state.InsertTx(ctx, tx)
	require.ErrorIs(t, err, errTooLarge)
	require.Empty(t, env.state.UnconfirmedTxs())
}

func TestInsertTx_GasWantedExceeded(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	app := newTestApp()
	cfg := app.Cfg()
	env := newTestEnv(rng, cfg, app.Proxy())
	// Tx with gas wanted exceeding block limit
	addr, nonce := app.NewAccount(rng)
	tx := env.genTx(rng, addr, nonce)
	tx.GasWanted = cfg.MaxGasWantedPerBlock + 1
	// Should be rejected by mempool.
	_, err := env.state.InsertTx(ctx, tx.encode())
	require.ErrorIs(t, err, errTooLarge)
	require.Empty(t, env.state.UnconfirmedTxs())
}

func TestInsertTx_GasEstimatedExceeded(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	app := newTestApp()
	cfg := app.Cfg()
	cfg.MaxGasEstimatedPerBlock = 10000
	cfg.MaxGasWantedPerBlock = cfg.MaxGasEstimatedPerBlock * 2
	env := newTestEnv(rng, cfg, app.Proxy())
	// Tx with gas wanted exceeding block limit
	addr, nonce := app.NewAccount(rng)
	tx := env.genTx(rng, addr, nonce)
	tx.GasEstimated = cfg.MaxGasEstimatedPerBlock + 1
	tx.GasWanted = tx.GasEstimated
	// Should be rejected by mempool.
	_, err := env.state.InsertTx(ctx, tx.encode())
	require.ErrorIs(t, err, errTooLarge)
	require.Empty(t, env.state.UnconfirmedTxs())
}

func TestInsertTx_AppRejectsTx(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	app := newTestApp()
	env := newTestEnv(rng, app.Cfg(), app.Proxy())
	// Construct tx with invalid encoding.
	tx := utils.GenBytes(rng, 1)
	_, err := decodeTxSpec(tx)
	require.Error(t, err)
	// Should be rejected by app.
	resp, err := env.state.InsertTx(ctx, tx)
	require.NoError(t, err)
	require.NotEqual(t, resp.Code, abci.CodeTypeOK)
	require.Empty(t, env.state.UnconfirmedTxs())
}

func TestMempool_BadNonce(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	app := newTestApp()
	env := newTestEnv(rng, app.Cfg(), app.Proxy())
	// Initialize nonce for random account.
	addr := common.Address(utils.GenBytes(rng, len(common.Address{})))
	nonce := uint64(rng.Intn(10000))
	for inner := range app.inner.Lock() {
		inner.nonces[addr] = nonce
	}
	// Try to insert tx with bad nonces.
	for _, nonce := range utils.Slice(nonce-1, nonce+1) {
		tx := env.genTx(rng, addr, nonce)
		_, err := env.state.InsertTx(ctx, tx.encode())
		require.ErrorIs(t, err, errBadNonce)
	}
	// Try to insert tx with correct nonce.
	tx := env.genTx(rng, addr, nonce)
	_, err := env.state.InsertTx(ctx, tx.encode())
	require.NoError(t, err)
}

type blockStats struct {
	count        uint64
	sizeBytes    uint64
	gasWanted    uint64
	gasEstimated uint64
}

// Push increments the block stats.
// Returns true iff the block stats are within block limits.
func (s *blockStats) Push(tx *txSpec, cfg *Config) bool {
	s.count += 1
	s.sizeBytes += uint64(len(tx.encode()))
	s.gasWanted += tx.GasWanted
	return s.count <= cfg.MaxTxsPerBlock &&
		s.sizeBytes <= types.MaxTxsBytesPerBlock &&
		s.gasWanted <= cfg.MaxGasWantedPerBlock &&
		s.gasEstimated <= cfg.MaxGasEstimatedPerBlock
}

func TestMempool_HappyPath(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	app := newTestApp()
	cfg := app.Cfg()
	cfg.MaxTxsPerBlock = 20
	cfg.MaxGasWantedPerBlock = 100
	cfg.MaxGasEstimatedPerBlock = 100
	env := newTestEnv(rng, cfg, app.Proxy())
	want := utils.NewMutex(map[common.Address][]*txSpec{})
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("env", func() error { return env.Run(ctx) })
		for range 10 {
			// Independent tasks submitting txs for some account.
			s.Spawn(func() error {
				// Initialize nonce for random address.
				addr := common.Address(utils.GenBytes(rng, len(common.Address{})))
				nonce := uint64(rng.Intn(10000))
				for inner := range app.inner.Lock() {
					inner.nonces[addr] = nonce
				}
				for range 5 {
					// Submit a sequence of txs with 1 which will fail to increment nonce.
					failAt := nonce + uint64(rng.Intn(20))
					for ; ; nonce += 1 {
						// Generate tx.
						tx := env.genTx(rng, addr, nonce)
						tx.ShouldFail = nonce == failAt

						if nonce <= failAt {
							// Check that nonce is as expected.
							if got, want := env.state.EvmNextPendingNonce(tx.Address), nonce; got != want {
								return fmt.Errorf("EvmNextPendingNonce() = %v, want %v", got, want)
							}
							// Insert tx and check response.
							resp, err := env.state.InsertTx(ctx, tx.encode())
							if err != nil {
								return fmt.Errorf("env.state.InsertTx(): %w", err)
							}
							if err := utils.TestDiff(tx.asResponse().ResponseCheckTx, resp); err != nil {
								return err
							}
						} else {
							// As soon as execution reaches failAt tx (which may be multiple blocks after sequencing it)
							// the mempool will reset the expected nonce back to the current app state (i.e. to failAt).
							resp, err := env.state.InsertTx(ctx, tx.encode())
							if errors.Is(err, errBadNonce) {
								// Check that nonce was reverted to the last executed tx.
								nonce = failAt
								if got, want := env.state.EvmNextPendingNonce(tx.Address), nonce; got != want {
									return fmt.Errorf("EvmNextPendingNonce() = %v, want %v", got, want)
								}
								break
							}
							if err != nil {
								return fmt.Errorf("env.state.InsertTx(): %w", err)
							}
							if err := utils.TestDiff(tx.asResponse().ResponseCheckTx, resp); err != nil {
								return err
							}
						}
						// At this point the tx is expected to be sequenced, no matter if it is executed or not.
						for want := range want.Lock() {
							want[addr] = append(want[addr], tx)
						}
					}
				}
				return nil
			})
		}
		return nil
	}))
	// Check that all the expected txs are sequenced:
	// txs after the last failed tx of each account are not guaranteed to be sequenced yet,
	// because we terminate the processing task as soon as insertion tasks stop.
	for want := range want.Lock() {
		for inner := range env.inner.Lock() {
			for addr, got := range inner.sequenced {
				require.Equal(t, want[addr][:len(got)], got)
			}
		}
	}
}

func TestMempool_EvmTxByHash(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	app := newTestApp()
	cfg := app.Cfg()
	cfg.BlockInterval = time.Millisecond
	env := newTestEnv(rng, cfg, app.Proxy())
	addr, nonce := app.NewAccount(rng)

	txs := utils.Slice(
		env.genTx(rng, addr, nonce),
		env.genTx(rng, addr, nonce+1),
	)

	for _, tx := range txs {
		_, err := env.state.InsertTx(ctx, tx.encode())
		require.NoError(t, err)
		got, ok := env.state.EvmTxByHash(tx.EVMHash)
		require.True(t, ok)
		require.Equal(t, tx.encode(), got)
	}

	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("env", func() error { return env.Run(ctx) })
		for m, ctrl := range env.state.mempool.Lock() {
			if err := ctrl.WaitUntil(ctx, func() bool {
				for _, tx := range txs {
					if _, ok := m.evmTxs[tx.EVMHash]; ok {
						return false
					}
				}
				return true
			}); err != nil {
				return err
			}
		}
		return nil
	}))

	for _, tx := range txs {
		_, ok := env.state.EvmTxByHash(tx.EVMHash)
		require.False(t, ok)
	}
	require.Equal(t, nonce+uint64(len(txs)), app.EvmNonce(addr))
}
