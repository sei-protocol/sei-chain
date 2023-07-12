package upgrades

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/goutils"
)

// Chain-ID constants for use in hard fork handlers.
const (
	ChainIDSeiHardForkTest = "sei-hard-fork-test"
)

type HardForkHandler interface {
	// a unique identifying name to ensure no duplicate handlers are registered
	GetName() string
	// The target chain ID for which to run this handler (which chain to run on)
	GetTargetChainID() string
	// The target height at which the handler should be executed
	GetTargetHeight() int64
	// An execution function used to process a hard fork handler
	ExecuteHandler(ctx sdk.Context) error
}

type HardForkManager struct {
	chainID       string
	handlerMap    map[int64][]HardForkHandler
	uniqueNameMap map[string]struct{}
}

// Create a new hard fork manager for a given chain ID.
// This will filter out any handlers for other chain IDs,
// and create a map that maps between target height and handlers to run for the given heights.
func NewHardForkManager(chainID string) *HardForkManager {
	return &HardForkManager{
		chainID:       chainID,
		handlerMap:    make(map[int64][]HardForkHandler),
		uniqueNameMap: make(map[string]struct{}),
	}
}

// This tries to register a handler with the hard fork manager.
// If the handler's target chain ID doesn't match the chain ID of the manager,
// this is a no-op and the handler is ignored.
func (hfm *HardForkManager) RegisterHandler(handler HardForkHandler) {
	if handler.GetTargetChainID() != hfm.chainID {
		return
	}
	handlerName := handler.GetName()
	if _, ok := hfm.uniqueNameMap[handlerName]; ok {
		// we already have a migration with this name - panic
		panic(fmt.Errorf("hard fork handler with name %s already registered", handlerName))
	}
	// register name for uniqueness assertion
	hfm.uniqueNameMap[handlerName] = struct{}{}
	targetHeight := handler.GetTargetHeight()
	if handlers, ok := hfm.handlerMap[targetHeight]; ok {
		goutils.InPlaceAppend[[]HardForkHandler](&handlers, handler)
		hfm.handlerMap[targetHeight] = handlers
	} else {
		hfm.handlerMap[targetHeight] = []HardForkHandler{handler}
	}
}

// This returns a boolean indicating whether or not the current height is a
// target height for which there are hard fork handlers to run.
func (hfm *HardForkManager) TargetHeightReached(ctx sdk.Context) bool {
	_, ok := hfm.handlerMap[ctx.BlockHeight()]
	return ok
}

// This executes the hard fork handlers for the current height. This will function will panic upon receiving an error during hard fork handler execution
func (hfm *HardForkManager) ExecuteForTargetHeight(ctx sdk.Context) {
	handlers, ok := hfm.handlerMap[ctx.BlockHeight()]
	if !ok {
		return
	}
	for _, handler := range handlers {
		handlerName := handler.GetName()
		ctx.Logger().Info(fmt.Sprintf(
			"Executing hard fork handler %s for chain ID %s at height %d",
			handlerName,
			handler.GetTargetChainID(),
			handler.GetTargetHeight(),
		))
		err := handler.ExecuteHandler(ctx)
		if err != nil {
			panic(err)
		}
		ctx.Logger().Info(fmt.Sprintf(
			"Completed execution for hard fork handler %s",
			handlerName,
		))
	}
	// TODO: do we want to emit any events to the context event manager (for use in beginBlockResponse)
}
