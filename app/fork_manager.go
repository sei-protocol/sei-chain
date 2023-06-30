package app

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

type HardForkHandler interface {
	GetTargetChainID() string
	GetTargetHeight() int64
	ExecuteHandler(ctx sdk.Context) error
}

type HardForkManager struct {
	chainID    string
	handlerMap map[int64][]HardForkHandler
}

// Create a new hard fork manager for a given chain ID.
// This will filter out any handlers for other chain IDs,
// and create a map that maps between target height and handlers to run for the given heights.
func NewHardForkManager(chainID string) *HardForkManager {
	return &HardForkManager{
		chainID:    chainID,
		handlerMap: make(map[int64][]HardForkHandler),
	}
}

// This tries to register a handler with the hard fork manager.
// If the handler's target chain ID doesn't match the chain ID of the manager,
// this is a no-op and the handler is ignored.
func (hfm *HardForkManager) RegisterHandler(handler HardForkHandler) {
	if handler.GetTargetChainID() != hfm.chainID {
		return
	}
	targetHeight := handler.GetTargetHeight()
	if handlers, ok := hfm.handlerMap[targetHeight]; ok {
		// TODO: refactor to use goutils InPlaceAppend once it's merged
		handlers = append(handlers, handler)
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
		err := handler.ExecuteHandler(ctx)
		if err != nil {
			panic(err)
		}
	}
	// TODO: do we want to emit any events to the context event manager (for use in beginBlockResponse)
}
