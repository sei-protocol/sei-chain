package port

import "github.com/sei-protocol/sei-chain/sei-ibc-go/modules/core/05-port/types"

// Name returns the IBC port ICS name.
func Name() string {
	return types.SubModuleName
}
