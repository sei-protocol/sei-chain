package evmrpc

// bench.go — debug_benchProgram: a measurement-only endpoint that times two
// matched EVM programs INSIDE the node process, with the node's own heap,
// allocator, garbage collector and scheduler around them.
//
// This exists for one study (gas-repricing, cross-cutting/memops-in-production)
// and is never meant for a release line. It is disabled unless
// `evm.enable_bench_api = true` is set in app.toml, and it refuses when it
// is not registered. It executes nothing against chain state: every call
// builds its EVM on a cache context of the latest committed state, installs
// the request's contracts there, and discards the context afterwards.
//
// Method shape (one call = one cell):
//
//	debug_benchProgram({name, a:{code,calldata,extra,gas}, b:{...}|null,
//	                    precompile:{addr,input,wantOut,wantGas}|null,
//	                    n, opGas, gate:"diff"|"equal"|"abs",
//	                    rounds, batchNs, pinCpu})
//	-> {gateOk, gateNote, gasA, gasB, itersA, itersB,
//	    rounds:[{round, order, nsA, nsB}], runtime:{before, after},
//	    busyPin, busySib, busyAll, height, pinned}
//
// Both arms run in every round, in alternating order; the client differences
// them within the round. Batches are sized by a pilot to ~batchNs. The gas
// gate is the same one the out-of-process harness applies: EVM-reported gas
// per arm must equal the analytic figure the client computed, and the arms
// must differ by exactly n×opGas (or be equal, or — for a precompile — the
// output must equal an independently computed expectation and RequiredGas
// must match).
//
// Programs run through evm.Call on the contract address the code was
// installed at, which is exactly how the parametric harness ran them
// (runtime.Call → evm.Call), so figures are comparable. The precompile
// control calls pc.Run directly — no CALL frame, no result cache.
//
// Pinning: pinCpu >= 0 locks the handling goroutine to its OS thread and
// sets that thread's affinity to the one vCPU for the duration of the call,
// restoring the previous mask afterwards. It restricts the measuring
// thread; it reserves nothing. GOGC and GOMAXPROCS are never touched.

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"runtime"
	rtmetrics "runtime/metrics"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/holiman/uint256"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/state"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	benchGasLimit     = 500_000_000
	benchMaxCode      = 64 << 10
	benchMaxRounds    = 2000
	benchMaxBatchNs   = 20e6
	benchMaxIters     = 5_000_000
	benchMaxDuration  = 120 * time.Second
	benchDefaultSlots = 8
)

var benchCaller = common.HexToAddress("0x00000000000000000000000000000000c0de00ca")
var benchContract = common.HexToAddress("0x00000000000000000000000000000000c0de0001")

type BenchAPI struct {
	keeper      *keeper.Keeper
	ctxProvider func(int64) sdk.Context
	sem         chan struct{}
}

func NewBenchAPI(k *keeper.Keeper, ctxProvider func(int64) sdk.Context, maxConcurrent uint64) *BenchAPI {
	if maxConcurrent == 0 {
		maxConcurrent = benchDefaultSlots
	}
	return &BenchAPI{keeper: k, ctxProvider: ctxProvider, sem: make(chan struct{}, maxConcurrent)}
}

type BenchArm struct {
	Code     hexutil.Bytes                    `json:"code"`
	Calldata hexutil.Bytes                    `json:"calldata"`
	Extra    map[common.Address]hexutil.Bytes `json:"extra"`
	Gas      uint64                           `json:"gas"` // analytic expectation; 0 = not checked
}

type BenchPrecompile struct {
	Addr    common.Address `json:"addr"`
	Input   hexutil.Bytes  `json:"input"`
	WantOut hexutil.Bytes  `json:"wantOut"`
	WantGas uint64         `json:"wantGas"`
}

type BenchRequest struct {
	Name       string           `json:"name"`
	A          *BenchArm        `json:"a"`
	B          *BenchArm        `json:"b"`
	Precompile *BenchPrecompile `json:"precompile"`
	N          int              `json:"n"`
	OpGas      uint64           `json:"opGas"`
	Gate       string           `json:"gate"` // diff | equal | abs
	Rounds     int              `json:"rounds"`
	BatchNs    float64          `json:"batchNs"`
	PinCPU     int              `json:"pinCpu"`
	ItersA     int              `json:"itersA"` // 0 = size by pilot
	ItersB     int              `json:"itersB"`
}

type BenchRound struct {
	Round int     `json:"round"`
	Order string  `json:"order"`
	NsA   float64 `json:"nsA"`
	NsB   float64 `json:"nsB"`
}

type BenchRuntime struct {
	GCCycles   uint64  `json:"gcCycles"`
	GCCPUSec   float64 `json:"gcCpuSec"`
	HeapLive   uint64  `json:"heapLive"`
	Goroutines uint64  `json:"goroutines"`
	GOGC       uint64  `json:"gogc"`
	MemLimit   uint64  `json:"memLimit"`
	TotalAlloc uint64  `json:"totalAlloc"`
	NumCPU     int     `json:"numCpu"`
	GOMAXPROCS int     `json:"gomaxprocs"`
}

type BenchResponse struct {
	Name     string       `json:"name"`
	GateOK   bool         `json:"gateOk"`
	GateNote string       `json:"gateNote"`
	GasA     uint64       `json:"gasA"`
	GasB     uint64       `json:"gasB"`
	ItersA   int          `json:"itersA"`
	ItersB   int          `json:"itersB"`
	Rounds   []BenchRound `json:"rounds"`
	Before   BenchRuntime `json:"before"`
	After    BenchRuntime `json:"after"`
	BusyPin  float64      `json:"busyPin"`
	BusySib  float64      `json:"busySib"`
	BusyAll  float64      `json:"busyAll"`
	Height   int64        `json:"height"`
	Pinned   bool         `json:"pinned"`
	PinCPU   int          `json:"pinCpu"`
	Sibling  int          `json:"sibling"`
	WallMs   float64      `json:"wallMs"`
	GoVer    string       `json:"goVersion"`
}

func benchRuntime() BenchRuntime {
	s := []rtmetrics.Sample{
		{Name: "/gc/cycles/total:gc-cycles"},
		{Name: "/cpu/classes/gc/total:cpu-seconds"},
		{Name: "/gc/heap/live:bytes"},
		{Name: "/sched/goroutines:goroutines"},
		{Name: "/gc/gogc:percent"},
		{Name: "/gc/gomemlimit:bytes"},
		{Name: "/gc/heap/allocs:bytes"},
	}
	rtmetrics.Read(s)
	u := func(i int) uint64 {
		if s[i].Value.Kind() == rtmetrics.KindUint64 {
			return s[i].Value.Uint64()
		}
		return 0
	}
	f := func(i int) float64 {
		if s[i].Value.Kind() == rtmetrics.KindFloat64 {
			return s[i].Value.Float64()
		}
		return 0
	}
	return BenchRuntime{GCCycles: u(0), GCCPUSec: f(1), HeapLive: u(2), Goroutines: u(3), GOGC: u(4),
		MemLimit: u(5), TotalAlloc: u(6), NumCPU: runtime.NumCPU(), GOMAXPROCS: runtime.GOMAXPROCS(0)}
}

// newBenchEVM builds an EVM on a cache context of the latest committed state,
// the way the keeper builds a read-only EVM for static calls, and installs
// the arm's contracts on its StateDB.
func (api *BenchAPI) newBenchEVM(arm *BenchArm) (*vm.EVM, int64, error) {
	sdkCtx := api.ctxProvider(LatestCtxHeight)
	cacheCtx, _ := sdkCtx.CacheContext()
	executionCtx := cacheCtx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(cacheCtx))
	stateDB := state.NewDBImpl(executionCtx, api.keeper, true)
	blockCtx, err := api.keeper.GetVMBlockContext(executionCtx, api.keeper.GetGasPool())
	if err != nil {
		return nil, 0, err
	}
	sstore := api.keeper.GetSstoreSetGasEIP2200(executionCtx)
	cfg := types.DefaultChainConfig().EthereumConfigWithSstore(api.keeper.ChainID(executionCtx), &sstore)
	evm := vm.NewEVM(*blockCtx, stateDB, cfg, vm.Config{}, api.keeper.CustomPrecompiles(executionCtx))
	evm.SetTxContext(vm.TxContext{Origin: benchCaller})
	if arm != nil {
		for addr, code := range arm.Extra {
			stateDB.SetCode(addr, code)
		}
		stateDB.SetCode(benchContract, arm.Code)
	}
	return evm, sdkCtx.BlockHeight(), nil
}

func benchBatch(fn func(), iters int) float64 {
	t0 := time.Now()
	for i := 0; i < iters; i++ {
		fn()
	}
	return float64(time.Since(t0).Nanoseconds()) / float64(iters)
}

func benchPilot(fn func(), batchNs float64) int {
	for i := 0; i < 3; i++ {
		fn()
	}
	clamp := func(k int) int {
		if k < 1 {
			return 1
		}
		if k > benchMaxIters {
			return benchMaxIters
		}
		return k
	}
	one := benchBatch(fn, 1)
	if one <= 0 {
		return 1
	}
	k := clamp(int(batchNs / one))
	two := benchBatch(fn, k)
	if two > 0 {
		k = clamp(int(batchNs / two))
	}
	return k
}

// BenchProgram is the RPC method.
func (api *BenchAPI) BenchProgram(ctx context.Context, req BenchRequest) (*BenchResponse, error) {
	if api == nil {
		return nil, errors.New("bench api not enabled")
	}
	select {
	case api.sem <- struct{}{}:
		defer func() { <-api.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if req.Rounds <= 0 || req.Rounds > benchMaxRounds {
		return nil, fmt.Errorf("rounds must be in 1..%d", benchMaxRounds)
	}
	if req.BatchNs <= 0 || req.BatchNs > benchMaxBatchNs {
		return nil, fmt.Errorf("batchNs must be in (0, %.0f]", benchMaxBatchNs)
	}
	for _, a := range []*BenchArm{req.A, req.B} {
		if a == nil {
			continue
		}
		if len(a.Code) > benchMaxCode || len(a.Calldata) > benchMaxCode {
			return nil, fmt.Errorf("code/calldata above %d bytes", benchMaxCode)
		}
		for _, c := range a.Extra {
			if len(c) > benchMaxCode {
				return nil, fmt.Errorf("extra code above %d bytes", benchMaxCode)
			}
		}
	}
	if req.Precompile == nil && req.A == nil {
		return nil, errors.New("need an arm or a precompile")
	}
	deadline := time.Now().Add(benchMaxDuration)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}

	resp := &BenchResponse{Name: req.Name, PinCPU: req.PinCPU, Sibling: -1, GoVer: runtime.Version()}

	// ---------- pin ----------
	if req.PinCPU >= 0 {
		runtime.LockOSThread()
		restore, err := benchPin(req.PinCPU)
		if err != nil {
			runtime.UnlockOSThread()
			return nil, fmt.Errorf("pin: %w", err)
		}
		defer func() {
			restore()
			runtime.UnlockOSThread()
		}()
		resp.Pinned = true
		resp.Sibling = benchSiblingOf(req.PinCPU)
	}

	// ---------- gate + runners ----------
	var runA, runB func()
	if req.Precompile != nil {
		evm, h, err := api.newBenchEVM(nil)
		if err != nil {
			return nil, err
		}
		resp.Height = h
		rules := evm.ChainConfig().Rules(evm.Context.BlockNumber, evm.Context.Random != nil, evm.Context.Time)
		pc, ok := vm.ActivePrecompiledContracts(rules, api.keeper.CustomPrecompiles(api.ctxProvider(LatestCtxHeight)))[req.Precompile.Addr]
		if !ok {
			resp.GateNote = fmt.Sprintf("FAIL precompile %s not active", req.Precompile.Addr.Hex())
			return resp, nil
		}
		gas := pc.RequiredGas(req.Precompile.Input)
		resp.GasA = gas
		if gas != req.Precompile.WantGas {
			resp.GateNote = fmt.Sprintf("FAIL RequiredGas %d != %d", gas, req.Precompile.WantGas)
			return resp, nil
		}
		out, err := pc.Run(evm, benchCaller, req.Precompile.Addr, req.Precompile.Input, new(big.Int), false, false, nil)
		if err != nil {
			resp.GateNote = "FAIL Run: " + err.Error()
			return resp, nil
		}
		if string(out) != string(req.Precompile.WantOut) {
			resp.GateNote = "FAIL output differs from the client's independent computation"
			return resp, nil
		}
		in, addr := req.Precompile.Input, req.Precompile.Addr
		runA = func() { pc.Run(evm, benchCaller, addr, in, new(big.Int), false, false, nil) }
	} else {
		evmA, h, err := api.newBenchEVM(req.A)
		if err != nil {
			return nil, err
		}
		resp.Height = h
		once := func(evm *vm.EVM, a *BenchArm) (uint64, error) {
			_, left, err := evm.Call(benchCaller, benchContract, a.Calldata, benchGasLimit, uint256.NewInt(0))
			return benchGasLimit - left, err
		}
		gA, errA := once(evmA, req.A)
		if errA != nil {
			resp.GateNote = "FAIL arm A errored: " + errA.Error()
			return resp, nil
		}
		resp.GasA = gA
		if req.A.Gas != 0 && gA != req.A.Gas {
			resp.GateNote = fmt.Sprintf("FAIL arm A gas %d != analytic %d", gA, req.A.Gas)
			return resp, nil
		}
		cdA := req.A.Calldata
		runA = func() { evmA.Call(benchCaller, benchContract, cdA, benchGasLimit, uint256.NewInt(0)) }
		if req.B != nil {
			evmB, _, err := api.newBenchEVM(req.B)
			if err != nil {
				return nil, err
			}
			gB, errB := once(evmB, req.B)
			if errB != nil {
				resp.GateNote = "FAIL arm B errored: " + errB.Error()
				return resp, nil
			}
			resp.GasB = gB
			if req.B.Gas != 0 && gB != req.B.Gas {
				resp.GateNote = fmt.Sprintf("FAIL arm B gas %d != analytic %d", gB, req.B.Gas)
				return resp, nil
			}
			switch req.Gate {
			case "diff":
				if want := uint64(req.N) * req.OpGas; gA < gB || gA-gB != want {
					resp.GateNote = fmt.Sprintf("FAIL gas differential %d != %d x %d", int64(gA)-int64(gB), req.N, req.OpGas)
					return resp, nil
				}
			case "equal":
				if gA != gB {
					resp.GateNote = fmt.Sprintf("FAIL gas differs by %d (must be equal)", int64(gA)-int64(gB))
					return resp, nil
				}
			}
			cdB := req.B.Calldata
			runB = func() { evmB.Call(benchCaller, benchContract, cdB, benchGasLimit, uint256.NewInt(0)) }
		}
	}
	resp.GateOK = true

	// ---------- sizing ----------
	resp.ItersA = req.ItersA
	if resp.ItersA <= 0 {
		resp.ItersA = benchPilot(runA, req.BatchNs)
	}
	if runB != nil {
		resp.ItersB = req.ItersB
		if resp.ItersB <= 0 {
			resp.ItersB = benchPilot(runB, req.BatchNs)
		}
	}

	// ---------- rounds ----------
	cpu0 := benchCPUTimes()
	resp.Before = benchRuntime()
	t0 := time.Now()
	for i := 0; i < req.Rounds; i++ {
		if time.Now().After(deadline) {
			resp.GateNote = fmt.Sprintf("stopped at round %d: duration bound", i)
			break
		}
		if err := ctx.Err(); err != nil {
			resp.GateNote = fmt.Sprintf("stopped at round %d: %v", i, err)
			break
		}
		var a, b float64
		order := "AB"
		if i%2 == 0 {
			a = benchBatch(runA, resp.ItersA)
			if runB != nil {
				b = benchBatch(runB, resp.ItersB)
			}
		} else {
			order = "BA"
			if runB != nil {
				b = benchBatch(runB, resp.ItersB)
			}
			a = benchBatch(runA, resp.ItersA)
		}
		resp.Rounds = append(resp.Rounds, BenchRound{Round: i, Order: order, NsA: a, NsB: b})
	}
	resp.WallMs = float64(time.Since(t0).Microseconds()) / 1e3
	resp.After = benchRuntime()
	cpu1 := benchCPUTimes()
	// -1 = unknown (no /proc, or not pinned). JSON cannot carry NaN.
	resp.BusyPin, resp.BusySib, resp.BusyAll = -1, -1, -1
	if busy := benchBusyBetween(cpu0, cpu1); len(busy) > 0 {
		sum := 0.0
		for _, v := range busy {
			sum += v
		}
		resp.BusyAll = sum / float64(len(busy))
		if req.PinCPU >= 0 && req.PinCPU < len(busy) {
			resp.BusyPin = busy[req.PinCPU]
			if resp.Sibling >= 0 && resp.Sibling < len(busy) {
				resp.BusySib = busy[resp.Sibling]
			}
		}
	}
	return resp, nil
}
