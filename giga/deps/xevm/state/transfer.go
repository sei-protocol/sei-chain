package state

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/holiman/uint256"
)

func TransferWithoutEvents(db vm.StateDB, sender, recipient common.Address, amount *uint256.Int) {
	sdb := GetDBImpl(db)
	if sdb == nil {
		panic("EventlessTransfer only works with DBImpl")
	}
	sdb.DisableEvents()
	defer sdb.EnableEvents()

	sdb.SubBalance(sender, amount, tracing.BalanceChangeTransfer)
	sdb.AddBalance(recipient, amount, tracing.BalanceChangeTransfer)
}
