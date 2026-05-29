package producer

import (
	"context"
	"encoding/binary"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/consensus"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/scope"
	"github.com/stretchr/testify/require"
)

type sealTrigger uint8

const (
	sealByCount sealTrigger = iota
	sealByBytes
	sealByGas
)

type txSpec struct {
	tx           []byte
	gasWanted    int64
	gasEstimated int64
}

type evmTxSpec struct {
	tx     []byte
	sender common.Address
	nonce  uint64
}

type overflowSpec struct {
	count bool
	bytes bool
	gas   bool
}

type sealScenario struct {
	countLimit uint64
	gasLimit   uint64
	sealed     [][]txSpec
	overflow   []overflowSpec
	open       []txSpec
	allTxs     [][]byte
	specsByTx  map[string]txSpec
}

type gasWantedApp struct {
	abci.BaseApplication
	gasWanted int64
}

func (a gasWantedApp) CheckTx(_ context.Context, _ *abci.RequestCheckTxV2) *abci.ResponseCheckTxV2 {
	return &abci.ResponseCheckTxV2{
		ResponseCheckTx: &abci.ResponseCheckTx{
			Code:      abci.CodeTypeOK,
			GasWanted: a.gasWanted,
		},
	}
}

type rejectingApp struct {
	abci.BaseApplication
	resp *abci.ResponseCheckTx
}

func (a rejectingApp) CheckTx(_ context.Context, _ *abci.RequestCheckTxV2) *abci.ResponseCheckTxV2 {
	return &abci.ResponseCheckTxV2{ResponseCheckTx: a.resp}
}

type acceptingApp struct {
	abci.BaseApplication
	resp *abci.ResponseCheckTx
}

func (a acceptingApp) CheckTx(_ context.Context, _ *abci.RequestCheckTxV2) *abci.ResponseCheckTxV2 {
	return &abci.ResponseCheckTxV2{ResponseCheckTx: a.resp}
}

type txSpecApp struct {
	abci.BaseApplication
	specs map[string]txSpec
}

func (a txSpecApp) CheckTx(_ context.Context, req *abci.RequestCheckTxV2) *abci.ResponseCheckTxV2 {
	spec := a.specs[string(req.Tx)]
	return &abci.ResponseCheckTxV2{
		ResponseCheckTx: &abci.ResponseCheckTx{
			Code:         abci.CodeTypeOK,
			GasWanted:    spec.gasWanted,
			GasEstimated: spec.gasEstimated,
		},
	}
}

type evmTxSpecApp struct {
	abci.BaseApplication
	baseNonces map[common.Address]uint64
}

func (a evmTxSpecApp) CheckTx(_ context.Context, req *abci.RequestCheckTxV2) *abci.ResponseCheckTxV2 {
	spec := decodeEvmTxSpec(req.Tx)
	return &abci.ResponseCheckTxV2{
		ResponseCheckTx: &abci.ResponseCheckTx{
			Code:         abci.CodeTypeOK,
			GasWanted:    50,
			GasEstimated: 40,
		},
		IsEVM:              true,
		EVMNonce:           spec.nonce,
		EVMSenderAddress:   spec.sender,
		SeiSenderAddress:   []byte("sender"),
		EVMRequiredBalance: big.NewInt(1),
	}
}

func (a evmTxSpecApp) EvmNonce(addr common.Address) uint64 {
	return a.baseNonces[addr]
}

func encodeEvmTx(sender common.Address, nonce uint64) []byte {
	tx := make([]byte, common.AddressLength+8)
	copy(tx, sender.Bytes())
	binary.BigEndian.PutUint64(tx[common.AddressLength:], nonce)
	return tx
}

func decodeEvmTxSpec(tx []byte) evmTxSpec {
	return evmTxSpec{
		tx:     tx,
		sender: common.BytesToAddress(tx[:common.AddressLength]),
		nonce:  binary.BigEndian.Uint64(tx[common.AddressLength:]),
	}
}

func newSealScenario(t *testing.T, rng utils.Rng) sealScenario {
	const (
		wantSealedBlocks   = 24
		minTriggerCoverage = 3
		maxAttempts        = 64
		gasScale           = 5_000
		unitMax            = 16
	)
	for range maxAttempts {
		countLimit := uint64(8 + rng.Intn(5))
		avgUnitsPerTx := (unitMax + 1) / 2
		byteBudgetUnits := max(unitMax+1, int(countLimit)*avgUnitsPerTx+rng.Intn(2*int(countLimit)+1)-int(countLimit))
		gasBudgetUnits := max(unitMax+1, int(countLimit)*avgUnitsPerTx+rng.Intn(2*int(countLimit)+1)-int(countLimit))
		byteScale := max(1, int(types.MaxTxsBytesPerBlock)/byteBudgetUnits)
		gasLimit := uint64(gasScale * gasBudgetUnits)
		var seq uint64
		specs := make([]txSpec, 0, wantSealedBlocks*int(countLimit))
		for len(specs) < wantSealedBlocks*int(countLimit)*4 {
			specs = append(specs, randomTxSpec(rng, &seq, byteScale, gasScale, unitMax))
		}
		scenario := simulateSealScenario(countLimit, gasLimit, specs)
		if len(scenario.sealed) < wantSealedBlocks {
			continue
		}
		scenario = truncateSealScenario(scenario, wantSealedBlocks)
		countHits, byteHits, gasHits := 0, 0, 0
		for _, overflow := range scenario.overflow {
			if overflow.count {
				countHits += 1
			}
			if overflow.bytes {
				byteHits += 1
			}
			if overflow.gas {
				gasHits += 1
			}
		}
		if countHits >= minTriggerCoverage && byteHits >= minTriggerCoverage && gasHits >= minTriggerCoverage {
			return scenario
		}
	}
	t.Fatal("failed to generate seal scenario with sufficient trigger coverage")
	return sealScenario{}
}

func truncateSealScenario(scenario sealScenario, wantSealedBlocks int) sealScenario {
	sealed := append([][]txSpec(nil), scenario.sealed[:wantSealedBlocks]...)
	overflow := append([]overflowSpec(nil), scenario.overflow[:wantSealedBlocks]...)
	open := scenario.open
	if wantSealedBlocks < len(scenario.sealed) {
		open = scenario.sealed[wantSealedBlocks]
	}
	allTxs := make([][]byte, 0)
	specsByTx := map[string]txSpec{}
	for _, block := range sealed {
		for _, spec := range block {
			allTxs = append(allTxs, spec.tx)
			specsByTx[string(spec.tx)] = spec
		}
	}
	for _, spec := range open {
		allTxs = append(allTxs, spec.tx)
		specsByTx[string(spec.tx)] = spec
	}
	return sealScenario{
		countLimit: scenario.countLimit,
		gasLimit:   scenario.gasLimit,
		sealed:     sealed,
		overflow:   overflow,
		open:       append([]txSpec(nil), open...),
		allTxs:     allTxs,
		specsByTx:  specsByTx,
	}
}

func randomTxSpec(rng utils.Rng, seq *uint64, byteScale, gasScale, unitMax int) txSpec {
	sizeUnits := 1 + rng.Intn(unitMax)
	gasUnits := 1 + rng.Intn(unitMax)
	size := sizeUnits * byteScale
	gasWanted := int64(gasUnits * gasScale)
	tx := make([]byte, size)
	binary.BigEndian.PutUint64(tx[:8], *seq)
	copy(tx[8:], utils.GenBytes(rng, size-8))
	*seq += 1
	gasEstimated := gasWanted
	if gasWanted > 1 {
		gasEstimated = gasWanted - int64(rng.Intn(int(gasWanted-1)))
	}
	return txSpec{tx: tx, gasWanted: gasWanted, gasEstimated: gasEstimated}
}

func simulateSealScenario(countLimit, gasLimit uint64, specs []txSpec) sealScenario {
	current := make([]txSpec, 0, countLimit)
	sealed := make([][]txSpec, 0)
	overflow := make([]overflowSpec, 0)
	allTxs := make([][]byte, 0, len(specs))
	specsByTx := make(map[string]txSpec, len(specs))

	for _, spec := range specs {
		allTxs = append(allTxs, spec.tx)
		specsByTx[string(spec.tx)] = spec
		if len(current) > 0 {
			o := blockOverflow(current, spec, countLimit, gasLimit)
			if o.count || o.bytes || o.gas {
				sealed = append(sealed, append([]txSpec(nil), current...))
				overflow = append(overflow, o)
				current = current[:0]
			}
		}
		current = append(current, spec)
	}
	return sealScenario{
		countLimit: countLimit,
		gasLimit:   gasLimit,
		sealed:     sealed,
		overflow:   overflow,
		open:       append([]txSpec(nil), current...),
		allTxs:     allTxs,
		specsByTx:  specsByTx,
	}
}

func blockOverflow(block []txSpec, next txSpec, countLimit, gasLimit uint64) overflowSpec {
	size, gas := blockTotals(block)
	return overflowSpec{
		count: uint64(len(block))+1 > countLimit,
		bytes: size+uint64(len(next.tx)) > uint64(types.MaxTxsBytesPerBlock),
		gas:   gas+uint64(next.gasWanted) > gasLimit,
	}
}

func blockTotals(block []txSpec) (size uint64, gas uint64) {
	for _, spec := range block {
		size += uint64(len(spec.tx))
		gas += uint64(spec.gasWanted)
	}
	return size, gas
}

func txsOf(block []txSpec) [][]byte {
	txs := make([][]byte, len(block))
	for i, spec := range block {
		txs[i] = spec.tx
	}
	return txs
}

func assertSealScenario(
	t *testing.T,
	state *State,
	firstBlock types.BlockNumber,
	scenario sealScenario,
) {
	t.Helper()

	lane := state.consensus.Avail().PublicKey()
	for i := range scenario.sealed {
		block, err := state.consensus.Avail().Block(t.Context(), lane, firstBlock+types.BlockNumber(i))
		require.NoError(t, err)
		require.Equal(t, txsOf(scenario.sealed[i]), block.Msg().Block().Payload().Txs())
	}
	require.Equal(t, txsOf(scenario.open), state.UnconfirmedTxs())
	require.Len(t, scenario.overflow, len(scenario.sealed))
	for i, sealed := range scenario.sealed {
		size, gas := blockTotals(sealed)
		require.LessOrEqual(t, uint64(len(sealed)), scenario.countLimit)
		require.LessOrEqual(t, size, uint64(types.MaxTxsBytesPerBlock))
		require.LessOrEqual(t, gas, scenario.gasLimit)
		var nextFirst txSpec
		if i+1 < len(scenario.sealed) {
			nextFirst = scenario.sealed[i+1][0]
		} else {
			nextFirst = scenario.open[0]
		}
		require.Equal(t, blockOverflow(sealed, nextFirst, scenario.countLimit, scenario.gasLimit), scenario.overflow[i])
		require.True(t, scenario.overflow[i].count || scenario.overflow[i].bytes || scenario.overflow[i].gas)
	}

	for m := range state.mempool.Lock() {
		require.Equal(t, firstBlock, m.first)
		require.Equal(t, firstBlock+types.BlockNumber(len(scenario.sealed)), m.next)
		require.Len(t, m.blocks, len(scenario.sealed))
		for i := range scenario.sealed {
			require.Equal(t, txsOf(scenario.sealed[i]), m.blocks[firstBlock+types.BlockNumber(i)].txs)
		}
		require.Equal(t, txsOf(scenario.open), m.nextBlock.txs)
		return
	}
	t.Fatal("unreachable")
}

func newTestState(t *testing.T, app abci.Application) *State {
	t.Helper()

	rng := utils.TestRng()
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

	return NewState(&Config{
		MaxGasPerBlock:  types.MaxTxsBytesPerBlock,
		MaxTxsPerBlock:  types.MaxTxsPerBlock,
		BlockInterval:   time.Hour,
		MaxTxsPerSecond: utils.None[uint64](),
		App:             proxy.New(app, proxy.NopMetrics()),
	}, consensusState)
}

func TestInsertTxRejectsTooLargeTransaction(t *testing.T) {
	state := newTestState(t, abci.BaseApplication{})
	tx := make([]byte, types.MaxTxsBytesPerBlock+1)

	resp, err := state.InsertTx(t.Context(), tx)

	require.Nil(t, resp)
	require.ErrorIs(t, err, errTooLarge)
	require.True(t, errors.Is(err, errTooLarge))
}

func TestInsertTxRejectsGasWantedAboveBlockLimit(t *testing.T) {
	state := newTestState(t, gasWantedApp{gasWanted: 101})
	state.cfg.MaxGasPerBlock = 100

	resp, err := state.InsertTx(t.Context(), []byte("tx"))

	require.Nil(t, resp)
	require.ErrorIs(t, err, errTooLarge)
}

func TestInsertTxReturnsRejectedCheckTxWithoutEnqueueing(t *testing.T) {
	wantResp := &abci.ResponseCheckTx{
		Code: 1,
		Log:  "rejected",
	}
	state := newTestState(t, rejectingApp{resp: wantResp})

	gotResp, err := state.InsertTx(t.Context(), []byte("tx"))

	require.NoError(t, err)
	require.Same(t, wantResp, gotResp)
	require.Empty(t, state.UnconfirmedTxs())
}

func TestInsertTxAppendsAcceptedTransactionToOpenBlock(t *testing.T) {
	wantResp := &abci.ResponseCheckTx{
		Code:         abci.CodeTypeOK,
		GasWanted:    50,
		GasEstimated: 40,
	}
	state := newTestState(t, acceptingApp{resp: wantResp})
	tx := []byte("tx1")

	gotResp, err := state.InsertTx(t.Context(), tx)

	require.NoError(t, err)
	require.Same(t, wantResp, gotResp)
	require.Equal(t, [][]byte{tx}, state.UnconfirmedTxs())
}

func TestInsertTxSealsCurrentBlockWhenTxCountWouldOverflow(t *testing.T) {
	rng := utils.TestRng()
	scenario := newSealScenario(t, rng)
	state := newTestState(t, txSpecApp{specs: scenario.specsByTx})
	state.cfg.MaxTxsPerBlock = scenario.countLimit
	state.cfg.MaxGasPerBlock = scenario.gasLimit
	lane := state.consensus.Avail().PublicKey()
	firstBlock := state.consensus.Avail().NextBlock(lane)
	err := scope.Run(t.Context(), func(ctx context.Context, s scope.Scope) error {
		s.SpawnBg(func() error { return utils.IgnoreCancel(state.Run(ctx)) })

		for _, tx := range scenario.allTxs {
			resp, err := state.InsertTx(ctx, tx)
			require.NoError(t, err)
			require.Equal(t, scenario.specsByTx[string(tx)].gasWanted, resp.GasWanted)
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

	state := newTestState(t, evmTxSpecApp{baseNonces: baseNonces})
	state.cfg.MaxTxsPerBlock = uint64(blockSize)

	currentExpected := make(map[common.Address]uint64, len(baseNonces))
	for addr, nonce := range baseNonces {
		currentExpected[addr] = nonce
	}
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
