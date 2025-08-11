package solomachine

import (
	"github.com/sei-protocol/sei-chain/ibc-go/v3/modules/light-clients/06-solomachine/types"
)

// Name returns the solo machine client name.
func Name() string {
	return types.SubModuleName
}
