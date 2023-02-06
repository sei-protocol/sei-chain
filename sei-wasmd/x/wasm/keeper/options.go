package keeper

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/CosmWasm/wasmd/x/wasm/types"
)

type optsFn func(*Keeper)

func (f optsFn) apply(keeper *Keeper) {
	f(keeper)
}

// WithWasmEngine is an optional constructor parameter to replace the default wasmVM engine with the
// given one.
func WithWasmEngine(x types.WasmerEngine) Option {
	return optsFn(func(k *Keeper) {
		k.wasmVM = x
	})
}

// WithMessageHandler is an optional constructor parameter to set a custom handler for wasmVM messages.
// This option should not be combined with Option `WithMessageEncoders` or `WithMessageHandlerDecorator`
func WithMessageHandler(x Messenger) Option {
	return optsFn(func(k *Keeper) {
		k.messenger = x
	})
}

// WithMessageHandlerDecorator is an optional constructor parameter to decorate the wasm handler for wasmVM messages.
// This option should not be combined with Option `WithMessageEncoders` or `WithMessageHandler`
func WithMessageHandlerDecorator(d func(old Messenger) Messenger) Option {
	return optsFn(func(k *Keeper) {
		k.messenger = d(k.messenger)
	})
}

// WithQueryHandler is an optional constructor parameter to set custom query handler for wasmVM requests.
// This option should not be combined with Option `WithQueryPlugins` or `WithQueryHandlerDecorator`
func WithQueryHandler(x WasmVMQueryHandler) Option {
	return optsFn(func(k *Keeper) {
		k.wasmVMQueryHandler = x
	})
}

// WithQueryHandlerDecorator is an optional constructor parameter to decorate the default wasm query handler for wasmVM requests.
// This option should not be combined with Option `WithQueryPlugins` or `WithQueryHandler`
func WithQueryHandlerDecorator(d func(old WasmVMQueryHandler) WasmVMQueryHandler) Option {
	return optsFn(func(k *Keeper) {
		k.wasmVMQueryHandler = d(k.wasmVMQueryHandler)
	})
}

// WithQueryPlugins is an optional constructor parameter to pass custom query plugins for wasmVM requests.
// This option expects the default `QueryHandler` set an should not be combined with Option `WithQueryHandler` or `WithQueryHandlerDecorator`.
func WithQueryPlugins(x *QueryPlugins) Option {
	return optsFn(func(k *Keeper) {
		q, ok := k.wasmVMQueryHandler.(QueryPlugins)
		if !ok {
			panic(fmt.Sprintf("Unsupported query handler type: %T", k.wasmVMQueryHandler))
		}
		k.wasmVMQueryHandler = q.Merge(x)
	})
}

// WithMessageEncoders is an optional constructor parameter to pass custom message encoder to the default wasm message handler.
// This option expects the `DefaultMessageHandler` set and should not be combined with Option `WithMessageHandler` or `WithMessageHandlerDecorator`.
func WithMessageEncoders(x *MessageEncoders) Option {
	return optsFn(func(k *Keeper) {
		q, ok := k.messenger.(*MessageHandlerChain)
		if !ok {
			panic(fmt.Sprintf("Unsupported message handler type: %T", k.messenger))
		}
		s, ok := q.handlers[0].(SDKMessageHandler)
		if !ok {
			panic(fmt.Sprintf("Unexpected message handler type: %T", q.handlers[0]))
		}
		e, ok := s.encoders.(MessageEncoders)
		if !ok {
			panic(fmt.Sprintf("Unsupported encoder type: %T", s.encoders))
		}
		s.encoders = e.Merge(x)
		q.handlers[0] = s
	})
}

// WithCoinTransferrer is an optional constructor parameter to set a custom coin transferrer
func WithCoinTransferrer(x CoinTransferrer) Option {
	return optsFn(func(k *Keeper) {
		k.bank = x
	})
}

func WithVMCacheMetrics(r prometheus.Registerer) Option {
	return optsFn(func(k *Keeper) {
		NewWasmVMMetricsCollector(k.wasmVM).Register(r)
	})
}

// WithGasRegister set a new gas register to implement custom gas costs.
// When the "gas multiplier" for wasmvm gas conversion is modified inside the new register,
// make sure to also use `WithApiCosts` option for non default values
func WithGasRegister(x GasRegister) Option {
	return optsFn(func(k *Keeper) {
		k.gasRegister = x
	})
}

// WithAPICosts sets custom api costs. Amounts are in cosmwasm gas Not SDK gas.
func WithAPICosts(human, canonical uint64) Option {
	return optsFn(func(_ *Keeper) {
		costHumanize = human
		costCanonical = canonical
	})
}

// WithMaxQueryStackSize overwrites the default limit for maximum query stacks
func WithMaxQueryStackSize(m uint32) Option {
	return optsFn(func(k *Keeper) {
		k.maxQueryStackSize = m
	})
}
