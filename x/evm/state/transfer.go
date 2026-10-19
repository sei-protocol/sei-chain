package state

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// TransferWithoutEvents performs core.Transfer without emitting the bank module's Cosmos events.
func TransferWithoutEvents(db vm.StateDB, sender, recipient common.Address, amount *uint256.Int, rules *params.Rules) {
	sdb := GetDBImpl(db)
	if sdb == nil {
		panic("EventlessTransfer only works with DBImpl")
	}
	sdb.DisableEvents()
	defer sdb.EnableEvents()

	core.Transfer(db, sender, recipient, amount, rules)
}
