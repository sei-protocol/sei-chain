package producer

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/big"
	"maps"
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
	address      common.Address
	nonce        uint64
	gasWanted    uint64
	gasEstimated uint64
	requiredBalance uint64
	payload      []byte
}

func (t *txSpec) encode() []byte {
	e := binary.BigEndian
	data := slices.Clone(t.address[:])
	data = e.AppendUint64(data, t.nonce)
	data = e.AppendUint64(data, t.gasWanted)
	data = e.AppendUint64(data, t.gasEstimated)
	data = e.AppendUint64(data, t.requiredBalance)
	data = append(data,t.payload...)
	return data
}

func (t *txSpec) asResponse() *abci.ResponseCheckTxV2 {
	return &abci.ResponseCheckTxV2{
		ResponseCheckTx: &abci.ResponseCheckTx{
			Code:         abci.CodeTypeOK,
			GasWanted:    int64(t.gasWanted),
			GasEstimated: int64(t.gasEstimated),
		},
		IsEVM:              true,
		EVMNonce:           t.nonce,
		EVMSenderAddress:   t.address,
		SeiSenderAddress:   t.address[:],
		EVMRequiredBalance: big.NewInt(int64(t.requiredBalance)),
	}
}

func decodeTxSpec(data []byte) (*txSpec,error) {
	panic("TODO")
}

func genTx(rng utils.Rng, cfg *Config) *txSpec {
	return &txSpec{
		address: common.Address(utils.GenBytes(rng,len(common.Address{}))),
		nonce: uint64(rng.Intn(1000)),
		gasWanted: uint64(rng.Int63n(int64(cfg.MaxGasPerBlock))),
		gasEstimated: uint64(rng.Int63n(int64(cfg.MaxGasPerBlock))),
		payload: utils.GenBytes(rng, 32),
	}
}

type testApp struct {
	abci.BaseApplication
	nonces map[common.Address]uint64
}

func newTestApp() *testApp {
	return &testApp{nonces: map[common.Address]uint64{}}
}

func (a testApp) EvmNonce(addr common.Address) uint64 { return a.nonces[addr] }

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

func testCfg() *Config {
	app := newTestApp()
	return &Config{
		MaxGasPerBlock:  types.MaxTxsBytesPerBlock,
		MaxTxsPerBlock:  types.MaxTxsPerBlock,
		BlockInterval:   time.Hour,
		MaxTxsPerSecond: utils.None[uint64](),
		App:             proxy.New(app, proxy.NopMetrics()),
	}
}

func newTestState(rng utils.Rng, cfg *Config) *State {
	committee, keys := types.GenCommittee(rng, 3)
	dataState := utils.OrPanic1(data.NewState(
		&data.Config{Committee: committee},
		utils.OrPanic1(data.NewDataWAL(utils.None[string](), committee)),
	))
	consensusState := utils.OrPanic1(consensus.NewState(&consensus.Config{
		Key:                keys[0],
		ViewTimeout:        func(types.View) time.Duration { return time.Hour },
		PersistentStateDir: utils.None[string](),
	}, dataState))
	return NewState(cfg, consensusState)
}

func TestInsertTx_TooLargeTx(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	state := newTestState(rng, testCfg())
	// Tx with size exceeding block limit.
	tx := utils.GenBytes(rng, int(types.MaxTxsBytesPerBlock+1))
	// Should be rejected by mempool.
	_, err := state.InsertTx(ctx, tx)
	require.ErrorIs(t, err, errTooLarge)
	require.Empty(t, state.UnconfirmedTxs())
}

func TestInsertTx_GasWantedExceeded(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	cfg := testCfg()
	state := newTestState(rng,cfg)
	// Tx with gas wanted exceeding block limit
	tx := genTx(rng,cfg)
	tx.gasWanted = cfg.MaxGasPerBlock + 1
	// Should be rejected by mempool.
	_, err := state.InsertTx(ctx, tx.encode())
	require.ErrorIs(t, err, errTooLarge)
	require.Empty(t, state.UnconfirmedTxs())
}

func TestInsertTx_AppRejectsTx(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	state := newTestState(rng, testCfg())
	// Construct tx with invalid encoding.
	tx := utils.GenBytes(rng, 1)
	_,err := decodeTxSpec(tx)
	require.Error(t, err)
	// Should be rejected by app.
	resp, err := state.InsertTx(ctx, tx)
	require.NoError(t, err)
	require.NotEqual(t, resp.Code, abci.CodeTypeOK)
	require.Empty(t, state.UnconfirmedTxs())
}

func TestInsertTx_Ok(t *testing.T) {
	ctx := t.Context()
	rng := utils.TestRng()
	cfg := testCfg()
	state := newTestState(rng, cfg)
	tx := genTx(rng, cfg)
	resp, err := state.InsertTx(ctx, tx.encode())
	require.NoError(t, err)
	require.Equal(t, tx.asResponse().ResponseCheckTx, resp)
	require.Equal(t, [][]byte{tx.encode()}, state.UnconfirmedTxs())
}

func TestInsertTx_SealFullBlocks(t *testing.T) {
	rng := utils.TestRng()
	scenario := newSealScenario(t, rng)
	state := newTestState(rng, txSpecApp{specs: scenario.specsByTx})
	state.cfg.MaxTxsPerBlock = scenario.countLimit
	state.cfg.MaxGasPerBlock = scenario.gasLimit
	lane := state.consensus.Avail().PublicKey()
	firstBlock := state.consensus.Avail().NextBlock(lane)
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return utils.IgnoreCancel(state.Run(ctx)) })
		for _, tx := range scenario.allTxs {
			if _, err := state.InsertTx(ctx, tx); err!=nil {
				return fmt.Errorf("state.InsertTx(): %w",err)
			}
		}
		assertSealScenario(t, state, firstBlock, scenario)
		return nil
	})
	require.NoError(t, err)
}

func TestInsertTxRequiresEVMNonceOrderAcrossAccountsAndBlocks(t *testing.T) {
	rng := utils.TestRng()
	accountCount := 3 + rng.Intn(2)
	blockSize := 2 + rng.Intn(2)
	goodCount := 2*blockSize + 1 + rng.Intn(blockSize+1)

	accounts := make([]common.Address, accountCount)
	baseNonces := make(map[common.Address]uint64, accountCount)
	expectedNonces := make(map[common.Address]uint64, accountCount)
	perAccountAccepted := make(map[common.Address]int, accountCount)
	for i := range accountCount {
		accounts[i] = common.BytesToAddress(utils.GenBytes(rng, common.AddressLength))
		baseNonces[accounts[i]] = uint64(rng.Intn(20))
		expectedNonces[accounts[i]] = baseNonces[accounts[i]]
	}

	type attempt struct {
		spec  evmTxSpec
		isBad bool
	}
	attempts := make([]attempt, 0, 2*goodCount)
	good := make([]evmTxSpec, 0, goodCount)

	newTx := func(sender common.Address, nonce uint64, label byte) evmTxSpec {
		_ = label
		return evmTxSpec{
			tx:     encodeEvmTx(sender, nonce),
			sender: sender,
			nonce:  nonce,
		}
	}
	badNonce := func(sender common.Address, want uint64) uint64 {
		switch rng.Intn(3) {
		case 0:
			return want + 1 + uint64(rng.Intn(3))
		case 1:
			if want == 0 {
				return 1 + uint64(rng.Intn(3))
			}
			return want - 1
		default:
			if want > baseNonces[sender] {
				return baseNonces[sender] + uint64(rng.Intn(int(want-baseNonces[sender])))
			}
			return want + 2 + uint64(rng.Intn(2))
		}
	}

	for i := range goodCount {
		sender := accounts[rng.Intn(len(accounts))]
		want := expectedNonces[sender]
		if i > 0 || want > 0 {
			bad := newTx(sender, badNonce(sender, want), 'b')
			attempts = append(attempts, attempt{spec: bad, isBad: true})
		}
		ok := newTx(sender, want, 'g')
		attempts = append(attempts, attempt{spec: ok})
		good = append(good, ok)
		expectedNonces[sender] = want + 1
		perAccountAccepted[sender] += 1
	}

	state := newTestState(rng, evmTxSpecApp{baseNonces: baseNonces})
	state.cfg.MaxTxsPerBlock = uint64(blockSize)

	currentExpected := maps.Clone(baseNonces)
	assertPendingNonces := func() {
		t.Helper()
		for addr, nonce := range currentExpected {
			require.Equal(t, nonce, state.EvmNextPendingNonce(addr))
		}
	}
	for _, x := range attempts {
		resp, err := state.InsertTx(t.Context(), x.spec.tx)
		if x.isBad {
			require.Nil(t, resp)
			require.ErrorIs(t, err, errBadNonce)
			assertPendingNonces()
			continue
		}
		require.NoError(t, err)
		require.EqualValues(t, 50, resp.GasWanted)
		currentExpected[x.spec.sender] += 1
		assertPendingNonces()
	}

	assertPendingNonces()

	sealedBlocks := (len(good) - 1) / blockSize
	openStart := sealedBlocks * blockSize
	require.Equal(t, txsOfEVM(good[openStart:]), state.UnconfirmedTxs())

	for m := range state.mempool.Lock() {
		require.Equal(t, sealedBlocks, len(m.blocks))
		for i := range sealedBlocks {
			from := i * blockSize
			to := from + blockSize
			require.Equal(t, txsOfEVM(good[from:to]), m.blocks[m.first+types.BlockNumber(i)].txs)
		}
		require.Equal(t, txsOfEVM(good[openStart:]), m.nextBlock.txs)
		return
	}
	t.Fatal("unreachable")
}

func txsOfEVM(specs []evmTxSpec) [][]byte {
	txs := make([][]byte, len(specs))
	for i, spec := range specs {
		txs[i] = spec.tx
	}
	return txs
}
