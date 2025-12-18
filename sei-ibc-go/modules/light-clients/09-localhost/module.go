package localhost

import (
	"github.com/sei-protocol/sei-chain/sei-ibc-go/modules/light-clients/09-localhost/types"
)

// Name returns the IBC client name
func Name() string {
	return types.SubModuleName
}
