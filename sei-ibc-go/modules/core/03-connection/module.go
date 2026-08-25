package connection

import (
	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/03-connection/types"
)

// Name returns the IBC connection ICS name.
func Name() string {
	return types.SubModuleName
}
