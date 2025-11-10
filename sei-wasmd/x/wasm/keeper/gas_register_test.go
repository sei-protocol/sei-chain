package keeper

import (
	"math"
	"strings"
	"testing"

	wasmvmtypes "github.com/CosmWasm/wasmvm/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
)

func TestCompileCosts(t *testing.T) {
	specs := map[string]struct {
		srcLen    int
		srcConfig WasmGasRegisterConfig
		exp       sdk.Gas
		expPanic  bool
	}{
		"one byte": {
			srcLen:    1,
			srcConfig: DefaultGasRegisterConfig(),
			exp:       sdk.Gas(3), // DefaultCompileCost
		},
		"zero byte": {
			srcLen:    0,
			srcConfig: DefaultGasRegisterConfig(),
			exp:       sdk.Gas(0),
		},
		"negative len": {
			srcLen:    -1,
			srcConfig: DefaultGasRegisterConfig(),
			expPanic:  true,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			if spec.expPanic {
				assert.Panics(t, func() {
					NewWasmGasRegister(spec.srcConfig).CompileCosts(spec.srcLen)
				})
				return
			}
			gotGas := NewWasmGasRegister(spec.srcConfig).CompileCosts(spec.srcLen)
			assert.Equal(t, spec.exp, gotGas)
		})
	}
}

func TestNewContractInstanceCosts(t *testing.T) {
	specs := map[string]struct {
		srcLen    int
		srcConfig WasmGasRegisterConfig
		pinned    bool
		exp       sdk.Gas
		expPanic  bool
	}{
		"small msg - pinned": {
			srcLen:    1,
			srcConfig: DefaultGasRegisterConfig(),
			pinned:    true,
			exp:       DefaultContractMessageDataCost,
		},
		"big msg - pinned": {
			srcLen:    math.MaxUint32,
			srcConfig: DefaultGasRegisterConfig(),
			pinned:    true,
			exp:       DefaultContractMessageDataCost * sdk.Gas(math.MaxUint32),
		},
		"empty msg - pinned": {
			srcLen:    0,
			pinned:    true,
			srcConfig: DefaultGasRegisterConfig(),
			exp:       sdk.Gas(0),
		},
		"small msg - unpinned": {
			srcLen:    1,
			srcConfig: DefaultGasRegisterConfig(),
			exp:       DefaultContractMessageDataCost + DefaultInstanceCost,
		},
		"big msg - unpinned": {
			srcLen:    math.MaxUint32,
			srcConfig: DefaultGasRegisterConfig(),
			exp:       sdk.Gas(DefaultContractMessageDataCost*math.MaxUint32 + DefaultInstanceCost),
		},
		"empty msg - unpinned": {
			srcLen:    0,
			srcConfig: DefaultGasRegisterConfig(),
			exp:       sdk.Gas(DefaultInstanceCost),
		},

		"negative len": {
			srcLen:    -1,
			srcConfig: DefaultGasRegisterConfig(),
			expPanic:  true,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			if spec.expPanic {
				assert.Panics(t, func() {
					NewWasmGasRegister(spec.srcConfig).NewContractInstanceCosts(spec.pinned, spec.srcLen)
				})
				return
			}
			gotGas := NewWasmGasRegister(spec.srcConfig).NewContractInstanceCosts(spec.pinned, spec.srcLen)
			assert.Equal(t, spec.exp, gotGas)
		})
	}
}

func TestContractInstanceCosts(t *testing.T) {
	// same as TestNewContractInstanceCosts currently
	specs := map[string]struct {
		srcLen    int
		srcConfig WasmGasRegisterConfig
		pinned    bool
		exp       sdk.Gas
		expPanic  bool
	}{
		"small msg - pinned": {
			srcLen:    1,
			srcConfig: DefaultGasRegisterConfig(),
			pinned:    true,
			exp:       DefaultContractMessageDataCost,
		},
		"big msg - pinned": {
			srcLen:    math.MaxUint32,
			srcConfig: DefaultGasRegisterConfig(),
			pinned:    true,
			exp:       sdk.Gas(DefaultContractMessageDataCost * math.MaxUint32),
		},
		"empty msg - pinned": {
			srcLen:    0,
			pinned:    true,
			srcConfig: DefaultGasRegisterConfig(),
			exp:       sdk.Gas(0),
		},
		"small msg - unpinned": {
			srcLen:    1,
			srcConfig: DefaultGasRegisterConfig(),
			exp:       DefaultContractMessageDataCost + DefaultInstanceCost,
		},
		"big msg - unpinned": {
			srcLen:    math.MaxUint32,
			srcConfig: DefaultGasRegisterConfig(),
			exp:       sdk.Gas(DefaultContractMessageDataCost*math.MaxUint32 + DefaultInstanceCost),
		},
		"empty msg - unpinned": {
			srcLen:    0,
			srcConfig: DefaultGasRegisterConfig(),
			exp:       sdk.Gas(DefaultInstanceCost),
		},

		"negative len": {
			srcLen:    -1,
			srcConfig: DefaultGasRegisterConfig(),
			expPanic:  true,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			if spec.expPanic {
				assert.Panics(t, func() {
					NewWasmGasRegister(spec.srcConfig).InstantiateContractCosts(spec.pinned, spec.srcLen)
				})
				return
			}
			gotGas := NewWasmGasRegister(spec.srcConfig).InstantiateContractCosts(spec.pinned, spec.srcLen)
			assert.Equal(t, spec.exp, gotGas)
		})
	}
}

func TestReplyCost(t *testing.T) {
	specs := map[string]struct {
		src       wasmvmtypes.Reply
		srcConfig WasmGasRegisterConfig
		pinned    bool
		exp       sdk.Gas
		expPanic  bool
	}{
		"subcall response with events and data - pinned": {
			src: wasmvmtypes.Reply{
				Result: wasmvmtypes.SubMsgResult{
					Ok: &wasmvmtypes.SubMsgResponse{
						Events: []wasmvmtypes.Event{
							{Type: "foo", Attributes: []wasmvmtypes.EventAttribute{{Key: "myKey", Value: "myData"}}},
						},
						Data: []byte{0x1},
					},
				},
			},
			srcConfig: DefaultGasRegisterConfig(),
			pinned:    true,
			exp:       sdk.Gas(3*DefaultEventAttributeDataCost + DefaultPerAttributeCost + DefaultContractMessageDataCost), // 3 == len("foo")
		},
		"subcall response with events - pinned": {
			src: wasmvmtypes.Reply{
				Result: wasmvmtypes.SubMsgResult{
					Ok: &wasmvmtypes.SubMsgResponse{
						Events: []wasmvmtypes.Event{
							{Type: "foo", Attributes: []wasmvmtypes.EventAttribute{{Key: "myKey", Value: "myData"}}},
						},
					},
				},
			},
			srcConfig: DefaultGasRegisterConfig(),
			pinned:    true,
			exp:       sdk.Gas(3*DefaultEventAttributeDataCost + DefaultPerAttributeCost), // 3 == len("foo")
		},
		"subcall response with events exceeds free tier- pinned": {
			src: wasmvmtypes.Reply{
				Result: wasmvmtypes.SubMsgResult{
					Ok: &wasmvmtypes.SubMsgResponse{
						Events: []wasmvmtypes.Event{
							{Type: "foo", Attributes: []wasmvmtypes.EventAttribute{{Key: strings.Repeat("x", DefaultEventAttributeDataFreeTier), Value: "myData"}}},
						},
					},
				},
			},
			srcConfig: DefaultGasRegisterConfig(),
			pinned:    true,
			exp:       sdk.Gas((3+6)*DefaultEventAttributeDataCost + DefaultPerAttributeCost), // 3 == len("foo"), 6 == len("myData")
		},
		"subcall response error - pinned": {
			src: wasmvmtypes.Reply{
				Result: wasmvmtypes.SubMsgResult{
					Err: "foo",
				},
			},
			srcConfig: DefaultGasRegisterConfig(),
			pinned:    true,
			exp:       3 * DefaultContractMessageDataCost,
		},
		"subcall response with events and data - unpinned": {
			src: wasmvmtypes.Reply{
				Result: wasmvmtypes.SubMsgResult{
					Ok: &wasmvmtypes.SubMsgResponse{
						Events: []wasmvmtypes.Event{
							{Type: "foo", Attributes: []wasmvmtypes.EventAttribute{{Key: "myKey", Value: "myData"}}},
						},
						Data: []byte{0x1},
					},
				},
			},
			srcConfig: DefaultGasRegisterConfig(),
			exp:       sdk.Gas(DefaultInstanceCost + 3*DefaultEventAttributeDataCost + DefaultPerAttributeCost + DefaultContractMessageDataCost),
		},
		"subcall response with events - unpinned": {
			src: wasmvmtypes.Reply{
				Result: wasmvmtypes.SubMsgResult{
					Ok: &wasmvmtypes.SubMsgResponse{
						Events: []wasmvmtypes.Event{
							{Type: "foo", Attributes: []wasmvmtypes.EventAttribute{{Key: "myKey", Value: "myData"}}},
						},
					},
				},
			},
			srcConfig: DefaultGasRegisterConfig(),
			exp:       sdk.Gas(DefaultInstanceCost + 3*DefaultEventAttributeDataCost + DefaultPerAttributeCost),
		},
		"subcall response with events exceeds free tier- unpinned": {
			src: wasmvmtypes.Reply{
				Result: wasmvmtypes.SubMsgResult{
					Ok: &wasmvmtypes.SubMsgResponse{
						Events: []wasmvmtypes.Event{
							{Type: "foo", Attributes: []wasmvmtypes.EventAttribute{{Key: strings.Repeat("x", DefaultEventAttributeDataFreeTier), Value: "myData"}}},
						},
					},
				},
			},
			srcConfig: DefaultGasRegisterConfig(),
			exp:       sdk.Gas(DefaultInstanceCost + (3+6)*DefaultEventAttributeDataCost + DefaultPerAttributeCost), // 3 == len("foo"), 6 == len("myData")
		},
		"subcall response error - unpinned": {
			src: wasmvmtypes.Reply{
				Result: wasmvmtypes.SubMsgResult{
					Err: "foo",
				},
			},
			srcConfig: DefaultGasRegisterConfig(),
			exp:       sdk.Gas(DefaultInstanceCost + 3*DefaultContractMessageDataCost),
		},
		"subcall response with empty events": {
			src: wasmvmtypes.Reply{
				Result: wasmvmtypes.SubMsgResult{
					Ok: &wasmvmtypes.SubMsgResponse{
						Events: make([]wasmvmtypes.Event, 10),
					},
				},
			},
			srcConfig: DefaultGasRegisterConfig(),
			exp:       DefaultInstanceCost,
		},
		"subcall response with events unset": {
			src: wasmvmtypes.Reply{
				Result: wasmvmtypes.SubMsgResult{
					Ok: &wasmvmtypes.SubMsgResponse{},
				},
			},
			srcConfig: DefaultGasRegisterConfig(),
			exp:       DefaultInstanceCost,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			if spec.expPanic {
				assert.Panics(t, func() {
					NewWasmGasRegister(spec.srcConfig).ReplyCosts(spec.pinned, spec.src)
				})
				return
			}
			gotGas := NewWasmGasRegister(spec.srcConfig).ReplyCosts(spec.pinned, spec.src)
			assert.Equal(t, spec.exp, gotGas)
		})
	}
}

func TestEventCosts(t *testing.T) {
	// most cases are covered in TestReplyCost already. This ensures some edge cases
	specs := map[string]struct {
		srcAttrs  []wasmvmtypes.EventAttribute
		srcEvents wasmvmtypes.Events
		expGas    sdk.Gas
	}{
		"empty events": {
			srcEvents: make([]wasmvmtypes.Event, 1),
			expGas:    DefaultPerCustomEventCost,
		},
		"empty attributes": {
			srcAttrs: make([]wasmvmtypes.EventAttribute, 1),
			expGas:   DefaultPerAttributeCost,
		},
		"both nil": {
			expGas: 0,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			gotGas := NewDefaultWasmGasRegister().EventCosts(spec.srcAttrs, spec.srcEvents)
			assert.Equal(t, spec.expGas, gotGas)
		})
	}
}

func TestToWasmVMGasConversion(t *testing.T) {
	specs := map[string]struct {
		src       storetypes.Gas
		srcConfig WasmGasRegisterConfig
		exp       uint64
		expPanic  bool
	}{
		"0": {
			src:       0,
			exp:       0,
			srcConfig: DefaultGasRegisterConfig(),
		},
		"max": {
			srcConfig: WasmGasRegisterConfig{
				GasMultiplier: 1,
			},
			src: math.MaxUint64,
			exp: math.MaxUint64,
		},
		"overflow": {
			srcConfig: WasmGasRegisterConfig{
				GasMultiplier: 2,
			},
			src:      math.MaxUint64,
			expPanic: true,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			if spec.expPanic {
				assert.Panics(t, func() {
					r := NewWasmGasRegister(spec.srcConfig)
					_ = r.ToWasmVMGas(spec.src)
				})
				return
			}
			r := NewWasmGasRegister(spec.srcConfig)
			got := r.ToWasmVMGas(spec.src)
			assert.Equal(t, spec.exp, got)
		})
	}
}

func TestFromWasmVMGasConversion(t *testing.T) {
	specs := map[string]struct {
		src       uint64
		exp       storetypes.Gas
		srcConfig WasmGasRegisterConfig
		expPanic  bool
	}{
		"0": {
			src:       0,
			exp:       0,
			srcConfig: DefaultGasRegisterConfig(),
		},
		"max": {
			srcConfig: WasmGasRegisterConfig{
				GasMultiplier: 1,
			},
			src: math.MaxUint64,
			exp: math.MaxUint64,
		},
		"missconfigured": {
			srcConfig: WasmGasRegisterConfig{
				GasMultiplier: 0,
			},
			src:      1,
			expPanic: true,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			if spec.expPanic {
				assert.Panics(t, func() {
					r := NewWasmGasRegister(spec.srcConfig)
					_ = r.FromWasmVMGas(spec.src)
				})
				return
			}
			r := NewWasmGasRegister(spec.srcConfig)
			got := r.FromWasmVMGas(spec.src)
			assert.Equal(t, spec.exp, got)
		})
	}
}
