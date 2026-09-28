package rpc

import (
	"context"
	"fmt"
)

type netAPI struct {
	backend Backend
}

func (api *netAPI) Version(_ context.Context) string {
	return fmt.Sprintf("%d", api.backend.EvmChainID())
}
