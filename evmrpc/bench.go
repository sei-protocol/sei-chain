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
//	debug_benchProgram({name, a:{code,calldata,extra,gas,wantOut,wantTimedGas}, b:{...}|null,
//	                    precompile:{addr,input,wantOut,wantGas}|null,
//	                    n, opGas, gate:"diff"|"equal"|"abs",
//	                    rounds, batchNs, pinCpu, timed, roundGapNs})
//	-> {gateOk, gateNote, gasA, gasB, itersA, itersB, timedGasA, timedGasB,
//	    rounds:[{round, order, nsA, nsB}], runtime:{before, after},
//	    busyPin, busySib, busyAll, height, pinned}
//
// Timed mode (timed: true; the large-working-set cells of PLAN §3): each arm
// carries its own timing boundary — an experiment-only precompile at
// benchTimerAddr that records time.Now() and the GAS value the program
// hands it, twice per call. One call is one batch (itersA = itersB = 1) and
// the round's nsA/nsB are the stamped intervals, so preparation (memory
// expansion, page touch, warm-up sweeps) and verification lie outside the
// clock while the interpreter and opcodes are the release ones. The gate
// checks the return data against wantOut and gas1 − gas2 against
// wantTimedGas; the two arms must use equal total gas. roundGapNs sleeps
// between rounds so the probe does not keep the node under sustained load.
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
	"maps"
	"math/big"
	"runtime"
	rtmetrics "runtime/metrics"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/tracing"
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
	benchMaxCalldata  = 64 << 20 // timed mode ships an address table (32 B per cache line)
	benchMaxRounds    = 2000
	benchMaxBatchNs   = 20e6
	benchMaxIters     = 5_000_000
	benchMaxDuration  = 120 * time.Second
	benchDefaultSlots = 8
)

var benchCaller = common.HexToAddress("0x00000000000000000000000000000000c0de00ca")
var benchContract = common.HexToAddress("0x00000000000000000000000000000000c0de0001")

// benchTimerAddr is the timing boundary of timed mode; the same address the
// out-of-process harness installs (memops-prod pkg/ws.go timerAddr).
var benchTimerAddr = common.HexToAddress("0x000000000000000000000000000000000000c10c")

// benchTimer records the first two stamps of one call.
type benchTimer struct {
	t [2]time.Time
	g [2]uint64
	n int
}

func (t *benchTimer) RequiredGas([]byte) uint64 { return 0 }
func (t *benchTimer) Run(_ *vm.EVM, _, _ common.Address, input []byte, _ *big.Int, _, _ bool, _ *tracing.Hooks) ([]byte, error) {
	now := time.Now()
	if t.n < 2 {
		t.t[t.n] = now
		t.g[t.n] = new(big.Int).SetBytes(input).Uint64()
	}
	t.n++
	return nil, nil
}

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
	// timed mode only
	WantOut      hexutil.Bytes `json:"wantOut"`      // expected return data
	WantTimedGas uint64        `json:"wantTimedGas"` // expected gas1 - gas2 across the two stamps (cold timer address)
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
	Timed      bool             `json:"timed"`      // arms carry their own timing boundary (see header)
	RoundGapNs float64          `json:"roundGapNs"` // sleep between rounds; 0 = none
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
	Name      string       `json:"name"`
	GateOK    bool         `json:"gateOk"`
	GateNote  string       `json:"gateNote"`
	GasA      uint64       `json:"gasA"`
	GasB      uint64       `json:"gasB"`
	ItersA    int          `json:"itersA"`
	ItersB    int          `json:"itersB"`
	TimedGasA uint64       `json:"timedGasA"` // timed mode: gas1 - gas2 observed at the gate
	TimedGasB uint64       `json:"timedGasB"`
	Rounds    []BenchRound `json:"rounds"`
	Before    BenchRuntime `json:"before"`
	After     BenchRuntime `json:"after"`
	BusyPin   float64      `json:"busyPin"`
	BusySib   float64      `json:"busySib"`
	BusyAll   float64      `json:"busyAll"`
	Height    int64        `json:"height"`
	Pinned    bool         `json:"pinned"`
	PinCPU    int          `json:"pinCpu"`
	Sibling   int          `json:"sibling"`
	WallMs    float64      `json:"wallMs"`
	GoVer     string       `json:"goVersion"`
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
	return api.newBenchEVMTimer(arm, nil)
}

// newBenchEVMTimer is newBenchEVM with the timing precompile installed at
// benchTimerAddr (timed mode). The node's custom precompiles are cloned, not
// modified.
func (api *BenchAPI) newBenchEVMTimer(arm *BenchArm, tp *benchTimer) (*vm.EVM, int64, error) {
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
	custom := api.keeper.CustomPrecompiles(executionCtx)
	if tp != nil {
		custom = maps.Clone(custom)
		if custom == nil {
			custom = map[common.Address]vm.PrecompiledContract{}
		}
		custom[benchTimerAddr] = tp
	}
	evm := vm.NewEVM(*blockCtx, stateDB, cfg, vm.Config{}, custom)
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
		if len(a.Code) > benchMaxCode || len(a.Calldata) > benchMaxCalldata {
			return nil, fmt.Errorf("code above %d or calldata above %d bytes", benchMaxCode, benchMaxCalldata)
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
	// A runner builds a FRESH EVM (fresh CacheContext + StateDB) and returns
	// the function one timed batch calls. The node's DBImpl accumulates
	// journal/cache/event state across calls, so reusing one StateDB for
	// thousands of calls makes every call slower than the last (measured:
	// 15 µs → 90 µs over 60 rounds). A batch of a few dozen calls on a
	// fresh StateDB is what a transaction sees; the build is not timed.
	var runA, runB func() (func(), error)
	// timed mode: one call = one stamped interval (see the header).
	var timedA, timedB func() (float64, error)
	if req.Timed {
		if req.Precompile != nil || req.A == nil || req.B == nil {
			return nil, errors.New("timed mode needs arms a and b and no precompile")
		}
		exec := func(a *BenchArm) (ns float64, used, g1, g2 uint64, out []byte, err error) {
			tp := &benchTimer{}
			evm, _, err := api.newBenchEVMTimer(a, tp)
			if err != nil {
				return 0, 0, 0, 0, nil, err
			}
			out, left, err := evm.Call(benchCaller, benchContract, a.Calldata, benchGasLimit, uint256.NewInt(0))
			if err != nil {
				return 0, benchGasLimit - left, 0, 0, out, err
			}
			if tp.n != 2 {
				return 0, benchGasLimit - left, 0, 0, out, fmt.Errorf("timer stamped %d times, want 2", tp.n)
			}
			return float64(tp.t[1].Sub(tp.t[0]).Nanoseconds()), benchGasLimit - left, tp.g[0], tp.g[1], out, nil
		}
		check := func(label string, a *BenchArm) (used, timed uint64, note string, ok bool) {
			_, used, g1, g2, out, err := exec(a)
			if err != nil {
				return used, 0, fmt.Sprintf("FAIL arm %s: %v", label, err), false
			}
			if string(out) != string(a.WantOut) {
				return used, g1 - g2, fmt.Sprintf("FAIL arm %s: return data differs from the client's expectation", label), false
			}
			switch g1 - g2 {
			case a.WantTimedGas:
			case a.WantTimedGas - 2500:
				note = "timer address warm in the node (timed-section gas 2500 below the cold expectation)"
			default:
				return used, g1 - g2, fmt.Sprintf("FAIL arm %s: timed-section gas %d != analytic %d", label, g1-g2, a.WantTimedGas), false
			}
			return used, g1 - g2, note, true
		}
		_, h, err := api.newBenchEVM(nil)
		if err != nil {
			return nil, err
		}
		resp.Height = h
		var ok bool
		var noteA, noteB string
		resp.GasA, resp.TimedGasA, noteA, ok = check("A", req.A)
		if !ok {
			resp.GateNote = noteA
			return resp, nil
		}
		resp.GasB, resp.TimedGasB, noteB, ok = check("B", req.B)
		if !ok {
			resp.GateNote = noteB
			return resp, nil
		}
		if req.Gate == "equal" && resp.GasA != resp.GasB {
			resp.GateNote = fmt.Sprintf("FAIL gas differs by %d (must be equal)", int64(resp.GasA)-int64(resp.GasB))
			return resp, nil
		}
		if noteA != "" {
			resp.GateNote = noteA
		}
		armA, armB := req.A, req.B
		timedA = func() (float64, error) { ns, _, _, _, _, err := exec(armA); return ns, err }
		timedB = func() (float64, error) { ns, _, _, _, _, err := exec(armB); return ns, err }
		runA = func() (func(), error) { return func() {}, nil }
		runB = runA
	} else if req.Precompile != nil {
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
		runA = func() (func(), error) {
			evm, _, err := api.newBenchEVM(nil)
			if err != nil {
				return nil, err
			}
			return func() { pc.Run(evm, benchCaller, addr, in, new(big.Int), false, false, nil) }, nil
		}
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
		armA := req.A
		runA = func() (func(), error) {
			evm, _, err := api.newBenchEVM(armA)
			if err != nil {
				return nil, err
			}
			cd := armA.Calldata
			return func() { evm.Call(benchCaller, benchContract, cd, benchGasLimit, uint256.NewInt(0)) }, nil
		}
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
			armB := req.B
			runB = func() (func(), error) {
				evm, _, err := api.newBenchEVM(armB)
				if err != nil {
					return nil, err
				}
				cd := armB.Calldata
				return func() { evm.Call(benchCaller, benchContract, cd, benchGasLimit, uint256.NewInt(0)) }, nil
			}
		}
	}
	resp.GateOK = true

	// ---------- sizing ----------
	resp.ItersA = req.ItersA
	if req.Timed {
		resp.ItersA, resp.ItersB = 1, 1
		for i := 0; i < 3; i++ { // warm calls, as the out-of-process harness does
			if _, err := timedA(); err != nil {
				return nil, err
			}
			if _, err := timedB(); err != nil {
				return nil, err
			}
		}
	} else if resp.ItersA <= 0 {
		fn, err := runA()
		if err != nil {
			return nil, err
		}
		resp.ItersA = benchPilot(fn, req.BatchNs)
	}
	if runB != nil && !req.Timed {
		resp.ItersB = req.ItersB
		if resp.ItersB <= 0 {
			fn, err := runB()
			if err != nil {
				return nil, err
			}
			resp.ItersB = benchPilot(fn, req.BatchNs)
		}
	}
	// one timed batch on a fresh EVM; the build is outside the clock
	batch := func(mk func() (func(), error), iters int) (float64, error) {
		fn, err := mk()
		if err != nil {
			return 0, err
		}
		return benchBatch(fn, iters), nil
	}
	batchA := func(iters int) (float64, error) { return batch(runA, iters) }
	batchB := func(iters int) (float64, error) { return batch(runB, iters) }
	if req.Timed {
		batchA = func(int) (float64, error) { return timedA() }
		batchB = func(int) (float64, error) { return timedB() }
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
		var errA, errB error
		order := "AB"
		if i%2 == 0 {
			a, errA = batchA(resp.ItersA)
			if runB != nil {
				b, errB = batchB(resp.ItersB)
			}
		} else {
			order = "BA"
			if runB != nil {
				b, errB = batchB(resp.ItersB)
			}
			a, errA = batchA(resp.ItersA)
		}
		if errA != nil || errB != nil {
			resp.GateNote = fmt.Sprintf("stopped at round %d: %v %v", i, errA, errB)
			break
		}
		resp.Rounds = append(resp.Rounds, BenchRound{Round: i, Order: order, NsA: a, NsB: b})
		if req.RoundGapNs > 0 && i+1 < req.Rounds {
			time.Sleep(time.Duration(req.RoundGapNs))
		}
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
