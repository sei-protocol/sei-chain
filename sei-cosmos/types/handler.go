package types

import (
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
)

// Handler defines the core of the state transition function of an application.
type Handler func(ctx Context, msg Msg) (*Result, error)

// AnteHandler authenticates transactions, before their internal messages are handled.
// If newCtx.IsZero(), ctx is used instead.
type AnteHandler func(ctx Context, tx Tx, simulate bool) (newCtx Context, err error)
type AnteDepGenerator func(txDeps []sdkacltypes.AccessOperation, tx Tx, txIndex int) (newTxDeps []sdkacltypes.AccessOperation, err error)

// AnteDecorator wraps the next AnteHandler to perform custom pre- and post-processing.
type AnteDecorator interface {
	AnteHandle(ctx Context, tx Tx, simulate bool, next AnteHandler) (newCtx Context, err error)
}

type AnteDepDecorator interface {
	AnteDeps(txDeps []sdkacltypes.AccessOperation, tx Tx, txIndex int, next AnteDepGenerator) (newTxDeps []sdkacltypes.AccessOperation, err error)
}

type DefaultDepDecorator struct{}

// Defeault AnteDeps returned
func (d DefaultDepDecorator) AnteDeps(txDeps []sdkacltypes.AccessOperation, tx Tx, txIndex int, next AnteDepGenerator) (newTxDeps []sdkacltypes.AccessOperation, err error) {
	defaultDeps := []sdkacltypes.AccessOperation{
		{
			ResourceType:       sdkacltypes.ResourceType_ANY,
			AccessType:         sdkacltypes.AccessType_UNKNOWN,
			IdentifierTemplate: "*",
		},
	}
	return next(append(txDeps, defaultDeps...), tx, txIndex)
}

type NoDepDecorator struct{}

func (d NoDepDecorator) AnteDeps(txDeps []sdkacltypes.AccessOperation, tx Tx, txIndex int, next AnteDepGenerator) (newTxDeps []sdkacltypes.AccessOperation, err error) {
	return next(txDeps, tx, txIndex)
}

type AnteFullDecorator interface {
	AnteDecorator
	AnteDepDecorator
}

func ChainAnteDecorators(chain ...AnteFullDecorator) (AnteHandler, AnteDepGenerator) {
	anteHandlerChainFunc := chainAnteDecoratorHandlers(chain...)
	anteHandlerDepGenFunc := chainAnteDecoratorDepGenerators(chain...)
	return anteHandlerChainFunc, anteHandlerDepGenFunc

}

// ChainDecorator chains AnteDecorators together with each AnteDecorator
// wrapping over the decorators further along chain and returns a single AnteHandler.
//
// NOTE: The first element is outermost decorator, while the last element is innermost
// decorator. Decorator ordering is critical since some decorators will expect
// certain checks and updates to be performed (e.g. the Context) before the decorator
// is run. These expectations should be documented clearly in a CONTRACT docline
// in the decorator's godoc.
//
// NOTE: Any application that uses GasMeter to limit transaction processing cost
// MUST set GasMeter with the FIRST AnteDecorator. Failing to do so will cause
// transactions to be processed with an infinite gasmeter and open a DOS attack vector.
// Use `ante.SetUpContextDecorator` or a custom Decorator with similar functionality.
// Returns nil when no AnteDecorator are supplied.
func chainAnteDecoratorHandlers(chain ...AnteFullDecorator) AnteHandler {
	if len(chain) == 0 {
		return nil
	}

	// handle non-terminated decorators chain
	if (chain[len(chain)-1] != Terminator{}) {
		chain = append(chain, Terminator{})
	}

	return func(ctx Context, tx Tx, simulate bool) (Context, error) {
		return chain[0].AnteHandle(ctx, tx, simulate, chainAnteDecoratorHandlers(chain[1:]...))
	}
}

func chainAnteDecoratorDepGenerators(chain ...AnteFullDecorator) AnteDepGenerator {
	if len(chain) == 0 {
		return nil
	}

	// handle non-terminated decorators chain
	if (chain[len(chain)-1] != Terminator{}) {
		chain = append(chain, Terminator{})
	}

	return func(txDeps []sdkacltypes.AccessOperation, tx Tx, txIndex int) ([]sdkacltypes.AccessOperation, error) {
		return chain[0].AnteDeps(txDeps, tx, txIndex, chainAnteDecoratorDepGenerators(chain[1:]...))
	}
}

type WrappedAnteDecorator struct {
	Decorator    AnteDecorator
	DepDecorator AnteDepDecorator
}

func CustomDepWrappedAnteDecorator(decorator AnteDecorator, depDecorator AnteDepDecorator) WrappedAnteDecorator {
	return WrappedAnteDecorator{
		Decorator:    decorator,
		DepDecorator: depDecorator,
	}
}

func DefaultWrappedAnteDecorator(decorator AnteDecorator) WrappedAnteDecorator {
	return WrappedAnteDecorator{
		Decorator: decorator,
		// TODO:: Use DefaultDepDecorator when each decorator defines their own
		//		  See NewConsumeGasForTxSizeDecorator for an example of how to implement a decorator
		DepDecorator: NoDepDecorator{},
	}
}

func (wad WrappedAnteDecorator) AnteHandle(ctx Context, tx Tx, simulate bool, next AnteHandler) (newCtx Context, err error) {
	return wad.Decorator.AnteHandle(ctx, tx, simulate, next)
}

func (wad WrappedAnteDecorator) AnteDeps(txDeps []sdkacltypes.AccessOperation, tx Tx, txIndex int, next AnteDepGenerator) (newTxDeps []sdkacltypes.AccessOperation, err error) {
	return wad.DepDecorator.AnteDeps(txDeps, tx, txIndex, next)
}

// Terminator AnteDecorator will get added to the chain to simplify decorator code
// Don't need to check if next == nil further up the chain
//
//	                      ______
//	                   <((((((\\\
//	                   /      . }\
//	                   ;--..--._|}
//	(\                 '--/\--'  )
//	 \\                | '-'  :'|
//	  \\               . -==- .-|
//	   \\               \.__.'   \--._
//	   [\\          __.--|       //  _/'--.
//	   \ \\       .'-._ ('-----'/ __/      \
//	    \ \\     /   __>|      | '--.       |
//	     \ \\   |   \   |     /    /       /
//	      \ '\ /     \  |     |  _/       /
//	       \  \       \ |     | /        /
//	 snd    \  \      \        /
type Terminator struct{}

// Simply return provided Context and nil error
func (t Terminator) AnteHandle(ctx Context, _ Tx, _ bool, _ AnteHandler) (Context, error) {
	return ctx, nil
}

// Simply return provided txDeps and nil error
func (t Terminator) AnteDeps(txDeps []sdkacltypes.AccessOperation, _ Tx, _ int, _ AnteDepGenerator) ([]sdkacltypes.AccessOperation, error) {
	return txDeps, nil
}
