package producer

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"golang.org/x/time/rate"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/avail"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

type txSpec struct {
	SigningKey      [32]byte
	Address         common.Address
	Nonce           uint64
	GasWanted       uint64
	GasEstimated    uint64
	RequiredBalance uint64
	ShouldFail      bool
	ID              common.Hash
	Payload         [32]byte
}

func (tx *txSpec) transaction() *ethtypes.Transaction {
	encoded := utils.OrPanic1(binary.Append(nil, binary.BigEndian, tx))
	unsigned := ethtypes.NewTx(&ethtypes.LegacyTx{Nonce: tx.Nonce, Gas: tx.GasWanted, Data: encoded})
	key := utils.OrPanic1(crypto.ToECDSA(tx.SigningKey[:]))
	return utils.OrPanic1(ethtypes.SignTx(unsigned, ethtypes.LatestSignerForChainID(big.NewInt(1)), key))
}

func (tx *txSpec) encode() []byte { return utils.OrPanic1(tx.transaction().MarshalBinary()) }

func decodeTxSpec(raw []byte) (*txSpec, error) {
	tx := new(ethtypes.Transaction)
	if err := tx.UnmarshalBinary(raw); err != nil {
		return nil, err
	}
	return decodeTxSpecData(tx.Data())
}

func decodeTxSpecData(data []byte) (*txSpec, error) {
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

func (tx *txSpec) asResponse() abevm.AdmittedTx {
	return abevm.AdmittedTx{GasEstimated: tx.GasEstimated}
}

func (env *testEnv) genTx(rng utils.Rng, addr common.Address, nonce uint64) abevm.EvmTxRaw {
	return env.parseTx(env.genTxSpec(rng, addr, nonce).encode())
}

func (env *testEnv) genTxSpec(rng utils.Rng, addr common.Address, nonce uint64) *txSpec {
	gasBase := int(env.state.cfg.MaxGasWantedPerBlock / env.state.cfg.maxTxsPerBlock())
	gasJitter := min(gasBase, 10)
	return &txSpec{
		Address:    addr,
		SigningKey: env.accounts.SigningKey(addr),
		Nonce:      nonce,
		// We randomize the gas in a way that both gas wanted and tx count limit have a chance of being exercised.
		GasWanted:    min(env.state.cfg.MaxGasWantedPerBlock, uint64(gasBase-gasJitter+rng.Intn(2*gasJitter))),
		GasEstimated: uint64(rng.Int63n(int64(env.state.cfg.MaxGasEstimatedPerBlock))),
		ID:           common.Hash(utils.GenBytes(rng, len(common.Hash{}))),
		Payload:      [32]byte(utils.GenBytes(rng, 32)),
	}
}

type testAppInner struct {
	keys    map[common.Address][32]byte
	nonces  map[common.Address]uint64
	appHash types.AppHash
}

// Application tracking evm nonces.
type testApp struct {
	abevm.Application
	inner   utils.Mutex[*testAppInner]
	checked utils.AtomicSend[uint64]
}

func newTestApp() *testApp {
	return &testApp{
		checked: utils.NewAtomicSend[uint64](0),
		inner: utils.NewMutex(&testAppInner{
			keys:   map[common.Address][32]byte{},
			nonces: map[common.Address]uint64{},
		}),
	}
}

func (a *testApp) NewAccount(rng utils.Rng) (common.Address, uint64) {
	key := utils.OrPanic1(crypto.ToECDSA(utils.GenBytes(rng, 32)))
	addr := crypto.PubkeyToAddress(key.PublicKey)
	nonce := uint64(rng.Intn(10000))
	for inner := range a.inner.Lock() {
		inner.nonces[addr] = nonce
		inner.keys[addr] = [32]byte(crypto.FromECDSA(key))
	}
	return addr, nonce
}

func (a *testApp) SigningKey(addr common.Address) [32]byte {
	for inner := range a.inner.Lock() {
		return inner.keys[addr]
	}
	panic("unreachable")
}

type accountApp interface {
	abevm.Application
	SigningKey(common.Address) [32]byte
}

func (a *testApp) Cfg() *Config {
	return &Config{
		MaxGasWantedPerBlock:    1000000,
		MaxGasEstimatedPerBlock: 1000000,
		MaxTxsPerBlock:          types.MaxTxsPerLaneBlock,
		BlockInterval:           time.Hour,
	}
}

func (a *testApp) EvmChainID() uint64 { return 1 }

func (a *testApp) EvmNonce(addr common.Address) uint64 {
	for inner := range a.inner.Lock() {
		return inner.nonces[addr]
	}
	panic("unreachable")
}

// EvmCommittedNonce matches EvmNonce: the test app has no commit in flight.
func (a *testApp) EvmCommittedNonce(addr common.Address) uint64 {
	return a.EvmNonce(addr)
}

func (a *testApp) CheckTx(tx abevm.EvmTx) (abevm.AdmittedTx, error) {
	defer func() {
		for range a.inner.Lock() {
			a.checked.Store(a.checked.Load() + 1)
		}
	}()
	spec, err := decodeTxSpecData(tx.Data())
	if err != nil {
		return abevm.AdmittedTx{}, err
	}
	return spec.asResponse(), nil
}

func (a *testApp) FinalizeBlock(_ context.Context, req abevm.FinalizeBlockReq) (abevm.FinalizeBlockResp, error) {
	for inner := range a.inner.Lock() {
		for _, txRaw := range req.Txs {
			tx, err := decodeTxSpecData(txRaw.Data())
			if err != nil {
				return abevm.FinalizeBlockResp{}, fmt.Errorf("decodeTxSpec(): %w", err)
			}
			if inner.nonces[tx.Address] == tx.Nonce && !tx.ShouldFail {
				inner.nonces[tx.Address] += 1
			}
		}
		h := sha256.Sum256(slices.Concat(req.Hash[:], inner.appHash[:]))
		inner.appHash = h[:]
		return abevm.FinalizeBlockResp{}, nil
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
	app       *abevm.Proxy
	signer    *abevm.Signer
	accounts  accountApp

	inner           utils.Mutex[*testEnvInner]
	executionPaused utils.Watch[*bool]
}

func (env *testEnv) Run(ctx context.Context) error {
	return utils.IgnoreCancel(scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return env.data.Run(ctx) })
		s.Spawn(func() error { return env.consensus.Run(ctx) })
		s.Spawn(func() error { return consensus.RunTestNetwork(ctx, utils.Slice(env.consensus)) })
		s.Spawn(func() error { return env.state.Run(ctx) })
		// Process blocks.
		stats := blockStats{}
		laneBlocks := 0
		firstBlock := env.data.Anchor().Load().Epochs[0].RoadRange().First
		for i := firstBlock; ; i += 1 {
			// Wait for the next block to be finalized.
			b, err := env.data.SuperBlock(ctx, i)
			if err != nil {
				return fmt.Errorf("env.data.SuperBlock(): %w", err)
			}

			qc, err := env.data.QC(ctx, i)
			if err != nil {
				return err
			}
			if len(qc.Headers()) > 0 {
				for paused, ctrl := range env.executionPaused.Lock() {
					if err := ctrl.WaitUntil(ctx, func() bool { return !*paused }); err != nil {
						return err
					}
				}
			}
			for offset := range qc.Headers() {
				block, err := env.data.Block(ctx, types.LaneBlockPos{Height: i, Offset: uint64(offset)})
				if err != nil {
					return err
				}
				// Check that adding first transaction to the previous block would exceed the limit.
				if laneBlocks > 0 {
					tx, err := decodeTxSpec(block.Payload().Txs()[0])
					if err != nil {
						return fmt.Errorf("decodeTxSpec(): %w", err)
					}
					if stats.Push(tx, env.state.cfg) {
						return fmt.Errorf("block sealed too early")
					}
				}

				// Check that block does not exceed limits.
				stats = blockStats{}
				for _, txRaw := range block.Payload().Txs() {
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

				laneBlocks++
			}

			// Mark block as executed.
			raw := make([]abevm.RawTx, len(b.Txs))
			for i, tx := range b.Txs {
				raw[i] = tx
			}
			txs := utils.OrPanic1(env.signer.ParseTxs(raw))
			env.signer.ForgetTxs(txs)
			_, err = env.app.FinalizeBlock(ctx, abevm.FinalizeBlockReq{Txs: txs, Hash: common.BytesToHash(b.Hash[:])})
			if err != nil {
				return fmt.Errorf("app.FinalizeBlock(): %w", err)
			}
			appHash := env.app.Info().LastBlockAppHash
			if err := env.data.PushAppHash(ctx, i, appHash); err != nil {
				return err
			}
		}
	}))
}

func newTestEnv(t testing.TB, rng utils.Rng, cfg *Config, app accountApp) *testEnv {
	env, _, _ := newTestEnvN(t, rng, 1, cfg, app)
	return env
}

func newTestEnvN(t testing.TB, rng utils.Rng, n int, cfg *Config, app accountApp) (*testEnv, *epoch.TestRegistry, []types.SecretKey) {
	registry, keys := epoch.GenRegistry(rng, n)
	store := blockstore.NewTestStore(t)
	dataState := utils.OrPanic1(data.NewState(&data.Config{App: abevm.TestEpochApp(registry)}, store))
	consensusState := utils.OrPanic1(consensus.NewState(&consensus.Config{
		Key:                keys[0],
		ViewTimeout:        func(types.View) time.Duration { return time.Hour },
		ProposalTimeout:    time.Hour,
		MaxRate:            rate.Inf,
		PersistentStateDir: utils.None[string](),
	}, dataState))
	evm := abevm.NewProxy(app)
	signer := newTestSigner(app.EvmChainID(), 0)
	return &testEnv{
		executionPaused: utils.NewWatch(new(bool)),
		data:            dataState,
		consensus:       consensusState,
		state:           NewState(cfg, consensusState, evm),
		app:             evm,
		signer:          signer,
		accounts:        app,
		inner: utils.NewMutex(&testEnvInner{
			sequenced: map[common.Address][]*txSpec{},
		}),
	}, registry, keys
}

// waitForMempool returns the next active session mempool.
func (env *testEnv) waitForMempool(ctx context.Context) (*mempool, error) {
	mp, err := env.state.mempool.Wait(ctx, func(mp utils.Option[*mempool]) bool { return mp.IsPresent() })
	if err != nil {
		return nil, err
	}
	return mp.OrPanic("active session"), nil
}

func TestInsertTx_TooLargeTx(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	app := newTestApp()
	env := newTestEnv(t, rng, app.Cfg(), app)
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return env.Run(ctx) })
		if _, err := env.waitForMempool(ctx); err != nil {
			return err
		}

		// Tx with size exceeding block limit.
		key := utils.OrPanic1(crypto.ToECDSA(utils.GenBytes(rng, 32)))
		signed := utils.OrPanic1(ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{
			Data: utils.GenBytes(rng, int(types.MaxTxsBytesPerLaneBlock+1)),
		}), ethtypes.LatestSignerForChainID(big.NewInt(1)), key))
		tx := utils.OrPanic1(signed.MarshalBinary())
		// Should be rejected by mempool.
		err := env.state.InsertTx(ctx, env.parseTx(tx))
		if !errors.Is(err, errTooLarge) {
			return fmt.Errorf("unexpected error: got %v, want %v", err, errTooLarge)
		}
		if pendingTxCount(env.state) != 0 {
			return fmt.Errorf("expected no unconfirmed transactions")
		}
		return nil
	}))
}

func TestInsertTx_GasWantedExceeded(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	app := newTestApp()
	cfg := app.Cfg()
	env := newTestEnv(t, rng, cfg, app)
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return env.Run(ctx) })
		if _, err := env.waitForMempool(ctx); err != nil {
			return err
		}

		// Tx with gas wanted exceeding block limit
		addr, nonce := app.NewAccount(rng)
		tx := env.genTxSpec(rng, addr, nonce)
		tx.GasWanted = cfg.MaxGasWantedPerBlock + 1
		// Should be rejected by mempool.
		err := env.state.InsertTx(ctx, env.parseTx(tx.encode()))
		if !errors.Is(err, errTooLarge) {
			return fmt.Errorf("unexpected error: got %v, want %v", err, errTooLarge)
		}
		if pendingTxCount(env.state) != 0 {
			return fmt.Errorf("expected no unconfirmed transactions")
		}
		return nil
	}))
}

func TestInsertTx_GasEstimatedExceeded(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	app := newTestApp()
	cfg := app.Cfg()
	cfg.MaxGasEstimatedPerBlock = 10000
	cfg.MaxGasWantedPerBlock = cfg.MaxGasEstimatedPerBlock * 2
	env := newTestEnv(t, rng, cfg, app)
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return env.Run(ctx) })
		if _, err := env.waitForMempool(ctx); err != nil {
			return err
		}

		// Tx with gas wanted exceeding block limit
		addr, nonce := app.NewAccount(rng)
		tx := env.genTxSpec(rng, addr, nonce)
		tx.GasEstimated = cfg.MaxGasEstimatedPerBlock + 1
		tx.GasWanted = tx.GasEstimated
		// Should be rejected by mempool.
		err := env.state.InsertTx(ctx, env.parseTx(tx.encode()))
		if !errors.Is(err, errTooLarge) {
			return fmt.Errorf("unexpected error: got %v, want %v", err, errTooLarge)
		}
		if pendingTxCount(env.state) != 0 {
			return fmt.Errorf("expected no unconfirmed transactions")
		}
		return nil
	}))
}

func TestInsertTx_AppRejectsTx(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	app := newTestApp()
	env := newTestEnv(t, rng, app.Cfg(), app)
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return env.Run(ctx) })
		if _, err := env.waitForMempool(ctx); err != nil {
			return err
		}

		// The Ethereum envelope is valid, but the test app rejects its payload.
		key := utils.OrPanic1(crypto.ToECDSA(utils.GenBytes(rng, 32)))
		signed := utils.OrPanic1(ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{Data: utils.GenBytes(rng, 1)}), ethtypes.LatestSignerForChainID(big.NewInt(1)), key))
		tx := utils.OrPanic1(signed.MarshalBinary())
		_, err := decodeTxSpec(tx)
		if err == nil {
			return fmt.Errorf("expected an error")
		}
		// Should be rejected by app.
		err = env.state.InsertTx(ctx, env.parseTx(tx))
		if !errors.Is(err, abevm.ErrTxRejected) {
			return fmt.Errorf("unexpected successful CheckTx response")
		}
		if app.checked.Load() != 1 {
			return fmt.Errorf("expected application rejection")
		}
		if pendingTxCount(env.state) != 0 {
			return fmt.Errorf("expected no unconfirmed transactions")
		}
		return nil
	}))
}

func TestInsertTx_ConcurrentSequentialNonces(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	app := newTestApp()
	env := newTestEnv(t, rng, app.Cfg(), app)
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return env.Run(ctx) })
		if _, err := env.waitForMempool(ctx); err != nil {
			return err
		}

		const (
			accountCount = 5
			txCount      = 20
		)
		type account struct {
			addr  common.Address
			start uint64
			rng   utils.Rng
		}
		accounts := make([]account, accountCount)
		for i := range accounts {
			accounts[i] = account{rng: rng.Split()}
			accounts[i].addr, accounts[i].start = app.NewAccount(rng)
		}

		if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			for _, account := range accounts {
				s.Spawn(func() error {
					for nonce := account.start; nonce < account.start+txCount; nonce++ {
						if err := env.state.InsertTx(ctx, env.genTx(account.rng, account.addr, nonce)); err != nil {
							return fmt.Errorf("InsertTx(): %w", err)
						}
					}
					return nil
				})
			}
			return nil
		}); err != nil {
			return err
		}

		for _, account := range accounts {
			if err := utils.TestDiff(account.start+txCount, env.state.EvmNextPendingNonce(account.addr)); err != nil {
				return err
			}
		}
		return nil
	}))
}

func TestInsertTx_BadNonceRejected(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	app := newTestApp()
	env := newTestEnv(t, rng, app.Cfg(), app)
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return env.Run(ctx) })
		if _, err := env.waitForMempool(ctx); err != nil {
			return err
		}

		addr, nonce := app.NewAccount(rng)

		for _, txNonce := range []uint64{nonce - 1, nonce + 1} {
			err := env.state.InsertTx(ctx, env.genTx(rng, addr, txNonce))
			if !errors.Is(err, errBadNonce) {
				return fmt.Errorf("unexpected error: got %v, want %v", err, errBadNonce)
			}
		}
		err := env.state.InsertTx(ctx, env.genTx(rng, addr, nonce))
		if err != nil {
			return err
		}

		for _, txNonce := range []uint64{nonce, nonce + 2} {
			err := env.state.InsertTx(ctx, env.genTx(rng, addr, txNonce))
			if !errors.Is(err, errBadNonce) {
				return fmt.Errorf("unexpected error: got %v, want %v", err, errBadNonce)
			}
		}
		return nil
	}))
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
		s.sizeBytes <= types.MaxTxsBytesPerLaneBlock &&
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
	env := newTestEnv(t, rng, cfg, app)
	want := utils.NewMutex(map[common.Address][]*txSpec{})
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("env", func() error { return env.Run(ctx) })
		if _, err := env.waitForMempool(ctx); err != nil {
			return err
		}
		for range 10 {
			// Independent tasks submitting txs for some account.
			s.Spawn(func() error {
				// Initialize nonce for random address.
				addr, nonce := app.NewAccount(rng)
				for range 5 {
					// Submit a sequence of txs with 1 which will fail to increment nonce.
					failAt := nonce + uint64(rng.Intn(20))
					for ; ; nonce += 1 {
						// Generate tx.
						tx := env.genTxSpec(rng, addr, nonce)
						tx.ShouldFail = nonce == failAt

						if nonce <= failAt {
							// Check that nonce is as expected.
							if got, want := env.state.EvmNextPendingNonce(tx.Address), nonce; got != want {
								return fmt.Errorf("EvmNextPendingNonce() = %v, want %v", got, want)
							}
							// Insert tx.
							err := env.state.InsertTx(ctx, env.parseTx(tx.encode()))
							if err != nil {
								return fmt.Errorf("env.state.InsertTx(): %w", err)
							}
						} else {
							// As soon as execution reaches failAt tx (which may be multiple blocks after sequencing it)
							// the mempool will reset the expected nonce back to the current app state (i.e. to failAt).
							err := env.state.InsertTx(ctx, env.parseTx(tx.encode()))
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

func TestProducer_LeaveCancelsAndRejoinStartsNewLane(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	app := newTestApp()
	cfg := app.Cfg()
	cfg.AllowEmptyBlocks = true
	cfg.BlockInterval = 10 * time.Millisecond
	env, registry, keys := newTestEnvN(t, rng, 2, cfg, app)
	a, b := keys[0], keys[1]
	availState := env.consensus.Avail()

	lane0 := types.LaneID{Validator: a.Public(), Joined: 0}
	require.Equal(t, lane0, availState.LocalLane().OrPanic("genesis"))

	if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("avail", func() error {
			return utils.IgnoreCancel(availState.Run(ctx))
		})
		s.SpawnBgNamed("producer", func() error {
			return utils.IgnoreCancel(env.state.Run(ctx))
		})

		if _, err := availState.Block(ctx, lane0, 0); err != nil {
			return err
		}

		addr, _ := app.NewAccount(rng)
		stuck := env.genTx(rng, addr, app.EvmNonce(addr))
		if err := env.state.InsertTx(ctx, stuck); err != nil {
			return err
		}

		if err := registry.TestPushEpoch(0, map[types.PublicKey]uint64{b.Public(): 1}); err != nil {
			return err
		}
		epLeave := registry.MustEpoch(2)
		if err := avail.TestAdvanceToEpoch(ctx, availState, keys, registry, epLeave.EpochIndex()); err != nil {
			return err
		}
		if err := availState.WaitUntilClosed(ctx, lane0); err != nil {
			return err
		}
		for {
			if !env.state.mempool.Load().IsPresent() {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Millisecond):
			}
		}

		if err := env.state.InsertTx(ctx, env.genTx(rng, addr, app.EvmNonce(addr))); !errors.Is(err, ErrNotProducing) {
			return fmt.Errorf("InsertTx after leave: got %v, want ErrNotProducing", err)
		}

		if err := registry.TestPushEpoch(1, map[types.PublicKey]uint64{a.Public(): 1, b.Public(): 1}); err != nil {
			return err
		}
		epJoin := registry.MustEpoch(3)
		if err := avail.TestAdvanceToEpoch(ctx, availState, keys, registry, epJoin.EpochIndex()); err != nil {
			return err
		}
		got, err := availState.WaitForNextLane(ctx, a.Public(), utils.Some(lane0))
		if err != nil {
			return err
		}
		if _, err = availState.Block(ctx, got, 0); err != nil {
			return err
		}
		err = env.state.InsertTx(ctx, env.genTx(rng, addr, app.EvmNonce(addr)))
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// Inserts without an active produce session return ErrNotProducing.
func TestInsertTx_WithoutSession(t *testing.T) {
	rng := utils.TestRng()
	app := newTestApp()
	env := newTestEnv(t, rng, app.Cfg(), app)
	tx := env.fullTx(rng, app).encode()
	err := env.state.InsertTx(t.Context(), env.parseTx(tx))
	require.ErrorIs(t, err, ErrNotProducing)
}

// fullTx returns a tx that alone fills a block, so every admitted one seals the previous block.
func (env *testEnv) fullTx(rng utils.Rng, app *testApp) *txSpec {
	addr, nonce := app.NewAccount(rng)
	tx := env.genTxSpec(rng, addr, nonce)
	tx.GasWanted = env.state.cfg.MaxGasWantedPerBlock
	tx.GasEstimated = tx.GasWanted
	return tx
}

// fillMempool pauses execution and fills the active session mempool.
func (env *testEnv) fillMempool(ctx context.Context, rng utils.Rng, app *testApp) error {
	env.pauseExecution()
	for range avail.BlocksPerLane + 1 {
		if err := env.state.InsertTx(ctx, env.parseTx(env.fullTx(rng, app).encode())); err != nil {
			return err
		}
	}

	return nil
}

// pauseExecution holds finalized blocks until execution is resumed.
func (env *testEnv) pauseExecution() {
	for paused, ctrl := range env.executionPaused.Lock() {
		*paused = true
		ctrl.Updated()
	}
}

// resumeExecution allows finalized blocks to execute.
func (env *testEnv) resumeExecution() {
	for paused, ctrl := range env.executionPaused.Lock() {
		*paused = false
		ctrl.Updated()
	}
}

// enqueueInserters starts calls and waits until each has completed CheckTx.
func (env *testEnv) enqueueInserters(ctx context.Context, s scope.Scope, rng utils.Rng, app *testApp, n int, want error) error {
	checked := app.checked.Load()
	for range n {
		tx := env.fullTx(rng, app)
		s.Spawn(func() error {
			err := env.state.InsertTx(ctx, env.parseTx(tx.encode()))
			if !errors.Is(err, want) {
				return fmt.Errorf("InsertTx(): got %v, want %v", err, want)
			}
			return nil
		})
	}
	_, err := app.checked.Wait(ctx, func(got uint64) bool { return got >= checked+uint64(n) })
	return err
}

// Resuming execution frees capacity for waiting transactions.
func TestInsertTx_AdmissionAfterExecution(t *testing.T) {
	rng := utils.TestRng()
	app := newTestApp()
	env := newTestEnv(t, rng, app.Cfg(), app)
	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return env.Run(ctx) })
		if _, err := env.waitForMempool(ctx); err != nil {
			return err
		}

		err := env.fillMempool(ctx, rng, app)
		if err != nil {
			return err
		}
		const n = 5
		if err := scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			admitted := make(chan *txSpec, n)
			checked := app.checked.Load()
			for range n {
				tx := env.fullTx(rng, app)
				s.Spawn(func() error {
					if err := env.state.InsertTx(ctx, env.parseTx(tx.encode())); err != nil {
						return err
					}
					return utils.Send(ctx, admitted, tx)
				})
			}
			if _, err := app.checked.Wait(ctx, func(got uint64) bool { return got >= checked+n }); err != nil {
				return err
			}
			env.resumeExecution()
			for range n {
				tx, err := utils.Recv(ctx, admitted)
				if err != nil {
					return err
				}
				if got, want := env.state.EvmNextPendingNonce(tx.Address), tx.Nonce+1; got != want {
					return fmt.Errorf("admitted transaction nonce: got %v, want %v", got, want)
				}
			}
			return nil
		}); err != nil {
			return err
		}
		return nil
	}))
}

// InsertTx queues callers beyond the EVM RPC's default concurrency limit.
func TestInsertTx_QueuesWithoutRPCAdmission(t *testing.T) {
	rng := utils.TestRng()
	app := newTestApp()
	env := newTestEnv(t, rng, app.Cfg(), app)
	require.NoError(t, scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return env.Run(ctx) })
		if _, err := env.waitForMempool(ctx); err != nil {
			return err
		}

		err := env.fillMempool(ctx, rng, app)
		if err != nil {
			return err
		}
		if err := utils.IgnoreCancel(scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
			if err := env.enqueueInserters(ctx, s, rng, app, 4097, context.Canceled); err != nil {
				return err
			}
			s.Cancel(context.Canceled)
			return nil
		})); err != nil {
			return err
		}
		return nil
	}))
}

func (a *testApp) Info() *abevm.ResponseInfo {
	for inner := range a.inner.Lock() {
		return &abevm.ResponseInfo{LastBlockAppHash: slices.Clone(inner.appHash)}
	}
	panic("unreachable")
}

func newTestSigner(chainID uint64, workers int) *abevm.Signer {
	return abevm.NewSigner(ethtypes.LatestSignerForChainID(new(big.Int).SetUint64(chainID)), 1024, workers)
}

func pendingTxCount(s *State) int {
	mp, ok := s.mempool.Load().Get()
	if !ok {
		return 0
	}
	for m := range mp.inner.Lock() {
		if m.closed {
			return 0
		}
		return len(m.nextBlock.rawTxs)
	}
	panic("unreachable")
}

func (env *testEnv) parseTx(raw []byte) abevm.EvmTxRaw {
	return utils.OrPanic1(env.signer.ParseTx(raw))
}
