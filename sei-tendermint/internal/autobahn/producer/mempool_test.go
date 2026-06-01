package producer

import (
	"context"
	"crypto/sha256"
	"fmt"
	"encoding/binary"
	"math/big"
	"slices"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
)

type txSpec struct {
	Address      common.Address
	Nonce        uint64
	GasWanted    uint64
	GasEstimated uint64
	RequiredBalance uint64
	Payload      []byte
}

func (t *txSpec) encode() []byte {
	e := binary.BigEndian
	data := slices.Clone(t.Address[:])
	data = e.AppendUint64(data, t.Nonce)
	data = e.AppendUint64(data, t.GasWanted)
	data = e.AppendUint64(data, t.GasEstimated)
	data = e.AppendUint64(data, t.RequiredBalance)
	data = append(data,t.Payload...)
	return data
}

func decodeTxSpec(data []byte) (*txSpec,error) {
	const headerSize = len(common.Address{}) + 4*8
	if len(data) < headerSize {
		return nil, fmt.Errorf("tx too short: got %d bytes, need at least %d", len(data), headerSize)
	}
	e := binary.BigEndian
	spec := &txSpec{}
	copy(spec.Address[:], data[:len(common.Address{})])
	offset := len(common.Address{})
	spec.Nonce = e.Uint64(data[offset:])
	offset += 8
	spec.GasWanted = e.Uint64(data[offset:])
	offset += 8
	spec.GasEstimated = e.Uint64(data[offset:])
	offset += 8
	spec.RequiredBalance = e.Uint64(data[offset:])
	offset += 8
	spec.Payload = slices.Clone(data[offset:])
	return spec,nil
}

func (t *txSpec) asResponse() *abci.ResponseCheckTxV2 {
	return &abci.ResponseCheckTxV2{
		ResponseCheckTx: &abci.ResponseCheckTx{
			Code:         abci.CodeTypeOK,
			GasWanted:    int64(t.GasWanted),
			GasEstimated: int64(t.GasEstimated),
		},
		IsEVM:              true,
		EVMNonce:           t.Nonce,
		EVMSenderAddress:   t.Address,
		SeiSenderAddress:   t.Address[:],
		EVMRequiredBalance: big.NewInt(int64(t.RequiredBalance)),
	}
}

func (env *testEnv) genTx(rng utils.Rng) *txSpec {
	for inner := range env.inner.Lock() {
		addr := inner.addrs[rng.Intn(len(inner.addrs))]
		nonce := inner.nonces[addr]
		inner.nonces[addr] += 1
		gasBase := int(env.state.cfg.MaxGasPerBlock/env.state.cfg.maxTxsPerBlock())
		gasJitter := min(gasBase,10)
		return &txSpec{
			Address: addr,
			Nonce: nonce,
			// We randomize the gas in a way that both gas and tx count limit have a chance of being exercised.
			GasWanted: min(env.state.cfg.MaxGasPerBlock,uint64(gasBase - gasJitter + rng.Intn(2*gasJitter))),
			GasEstimated: uint64(rng.Int63n(int64(env.state.cfg.MaxGasPerBlock))),
			Payload: utils.GenBytes(rng, 32),
		}
	}
	panic("unreachable")
}

type testAppInner struct {
	nonces map[common.Address]uint64
	appHash types.AppHash
}

// Application tracking evm nonces.
type testApp struct {
	abci.BaseApplication
	inner utils.Mutex[*testAppInner]
}

func newTestApp() *testApp {
	return &testApp{
		inner: utils.NewMutex(&testAppInner {
			nonces: map[common.Address]uint64{},
		}),
	}
}

func (a *testApp) EvmNonce(addr common.Address) uint64 {
	for inner := range a.inner.Lock() {
		return inner.nonces[addr]
	}
	panic("unreachable")
}

func (a *testApp) CheckTx(_ context.Context, req *abci.RequestCheckTxV2) *abci.ResponseCheckTxV2 {
	tx,err := decodeTxSpec(req.Tx)
	if err!=nil {
		return &abci.ResponseCheckTxV2 {
			ResponseCheckTx: &abci.ResponseCheckTx{
				Code: 1,
				Codespace: "some codespace",
				Log: err.Error(),
			},
		}
	}
	return tx.asResponse() 
}

func (a *testApp) FinalizeBlock(_ context.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
	for inner := range a.inner.Lock() {
		for _, txRaw := range req.Txs {
			tx,err := decodeTxSpec(txRaw)
			if err!=nil { return nil, fmt.Errorf("decodeTxSpec(): %w",err) }
			if inner.nonces[tx.Address] == tx.Nonce {
				inner.nonces[tx.Address] += 1
			}
		}
		h := sha256.Sum256(slices.Concat(req.Hash, inner.appHash[:]))
		inner.appHash = h[:] 
		return &abci.ResponseFinalizeBlock{AppHash: inner.appHash},nil
	}
	panic("unreachable")
}

func testCfg() *Config {
	return &Config{
		MaxGasPerBlock:  types.MaxTxsBytesPerBlock,
		MaxTxsPerBlock:  types.MaxTxsPerBlock,
		BlockInterval:   time.Hour,
		App:             proxy.New(newTestApp(), proxy.NopMetrics()),
	}
}

type testEnvInner struct {
	addrs []common.Address
	nonces map[common.Address]uint64
}

// Single node consensus network for testing mempool behavior.
type testEnv struct {
	state *State
	consensus *consensus.State
	data *data.State
	
	inner utils.Mutex[*testEnvInner]
}

func (env *testEnv) Run(ctx context.Context) error {
	return utils.IgnoreCancel(scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.Spawn(func() error { return env.data.Run(ctx) })
		s.Spawn(func() error { return env.consensus.Run(ctx) })
		s.Spawn(func() error { return consensus.RunTestNetwork(ctx, utils.Slice(env.consensus)) })
		s.Spawn(func() error { return env.state.Run(ctx) })
		return nil	
	}))
}

func newTestEnv(rng utils.Rng, cfg *Config, numAddrs int) *testEnv {
	addrs := utils.GenSliceN(rng, numAddrs, func(rng utils.Rng) common.Address {
		return common.Address(utils.GenBytes(rng,len(common.Address{})))
	})

	committee, keys := types.GenCommittee(rng, 1)
	dataState := utils.OrPanic1(data.NewState(
		&data.Config{Committee: committee},
		utils.OrPanic1(data.NewDataWAL(utils.None[string](), committee)),
	))
	consensusState := utils.OrPanic1(consensus.NewState(&consensus.Config{
		Key:                keys[0],
		ViewTimeout:        func(types.View) time.Duration { return time.Hour },
		PersistentStateDir: utils.None[string](),
	}, dataState))
	return &testEnv {
		data: dataState,
		consensus: consensusState,
		state: NewState(cfg, consensusState),
		inner: utils.NewMutex(&testEnvInner{
			addrs:addrs,
			nonces:map[common.Address]uint64{},
		}),
	}
}

func TestInsertTx_TooLargeTx(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	env := newTestEnv(rng, testCfg(), 1)
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
	cfg := testCfg()
	env := newTestEnv(rng,cfg, 1)
	// Tx with gas wanted exceeding block limit
	tx := env.genTx(rng)
	tx.GasWanted = cfg.MaxGasPerBlock + 1
	// Should be rejected by mempool.
	_, err := env.state.InsertTx(ctx, tx.encode())
	require.ErrorIs(t, err, errTooLarge)
	require.Empty(t, env.state.UnconfirmedTxs())
}

func TestInsertTx_AppRejectsTx(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	env := newTestEnv(rng, testCfg(), 1)
	// Construct tx with invalid encoding.
	tx := utils.GenBytes(rng, 1)
	_,err := decodeTxSpec(tx)
	require.Error(t, err)
	// Should be rejected by app.
	resp, err := env.state.InsertTx(ctx, tx)
	require.NoError(t, err)
	require.NotEqual(t, resp.Code, abci.CodeTypeOK)
	require.Empty(t, env.state.UnconfirmedTxs())
}

type blockStats struct {
	count uint64
	sizeBytes uint64
	gasWanted uint64
}

// Push increments the block stats.
// Returns true iff the block stats are within block limits.
func (s *blockStats) Push(tx *txSpec, cfg *Config) bool {
	s.count += 1
	s.sizeBytes += uint64(len(tx.encode()))
	s.gasWanted += tx.GasWanted
	return s.count <= cfg.MaxTxsPerBlock &&
		s.sizeBytes <= types.MaxTxsBytesPerBlock &&
		s.gasWanted <= cfg.MaxGasPerBlock
}

func TestMempool_HappyPath(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	cfg := testCfg()
	cfg.MaxTxsPerBlock = 20
	cfg.MaxGasPerBlock = 100
	env := newTestEnv(rng, cfg, 10)
	numTxs := 1000
	require.NoError(t, scope.Run(ctx, func(ctx context.Context, s scope.Scope) error {
		s.SpawnBgNamed("env", func() error { return env.Run(ctx) })
		want := utils.NewMutex(&[]*txSpec{})
		s.SpawnBgNamed("genTx", func() error {
			// Generate transactions.
			for i:=0;;i += 1 {
				t.Logf("tx[%v]",i)
				tx := env.genTx(rng)
				resp, err := env.state.InsertTx(ctx, tx.encode())
				if err!=nil { return utils.IgnoreCancel(fmt.Errorf("env.state.InsertTx(): %w",err)) }
				if err := utils.TestDiff(tx.asResponse().ResponseCheckTx, resp); err!=nil {
					return err
				}
				for want := range want.Lock() {
					*want = append(*want,tx)
				}
			}
		})
		
		var got []*txSpec
		stats := blockStats{}
		for i := env.data.Committee().FirstBlock();; i += 1 {
			t.Logf("block[%v]",i)
			if len(got) >= numTxs {
				break
			}

			// Wait for the next block to be finalized.
			b,err := env.data.GlobalBlock(ctx,i)
			if err!=nil { return fmt.Errorf("env.data.GlobalBlock(): %w",err) }

			// Check that adding first transaction to the previous block would exceed the limit.
			if i>env.data.Committee().FirstBlock() {
				tx,err := decodeTxSpec(b.Payload.Txs()[0])
				if err!=nil {
					return fmt.Errorf("decodeTxSpec(): %w",err)
				}
				if stats.Push(tx,cfg) {
					return fmt.Errorf("block sealed too early")
				}
			}

			// Check that block does not exceed limits.
			stats = blockStats{}
			for _,txRaw := range b.Payload.Txs() {
				tx,err := decodeTxSpec(txRaw)
				if err!=nil {
					return fmt.Errorf("decodeTxSpec(): %w",err)
				}
				got = append(got,tx)
				if !stats.Push(tx,cfg) {
					return fmt.Errorf("block sealed too late")
				}	
			}

			// Mark block as executed.
			h := b.Header.Hash()
			resp,err:=cfg.App.FinalizeBlock(ctx,&abci.RequestFinalizeBlock{Txs:b.Payload.Txs(),Hash:h[:]})
			if err!=nil {
				return fmt.Errorf("app.FinalizeBlock(): %w",err)
			}
			if err:=env.data.PushAppHash(ctx,i,resp.AppHash); err!=nil {
				return err
			}
		}
		// Check that the transactions are finalized in the same order that they were sequenced.
		for want := range want.Lock() {
			return utils.TestDiff((*want)[:len(got)],got)
		}
		panic("unreachable")
	}))
}

func TestMempool_EvmNextPendingNonce(t *testing.T) {
	// TODO
	// tracking includes pending txs
	// fallback to app.EvmNonce
}

func TestMempool_FailedNonce(t *testing.T) {
	// TODO
	// account is reverted to the last successful executed
	// other accounts are untouched
}

func TestInsertTx_NonceOutOfOrder(t *testing.T) {
	// TODO
}
