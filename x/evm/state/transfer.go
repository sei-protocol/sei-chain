package state

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"math/big"
)

func TransferWithoutEvents(db vm.StateDB, sender, recipient common.Address, amount *big.Int) {
	sdb, ok := db.(*DBImpl)
	if !ok {
		panic("EventlessTransfer only works with DBImpl")
	}
	sdb.DisableEvents()
	defer sdb.EnableEvents()

	sdb.SubBalance(sender, amount, tracing.BalanceChangeTransfer)
	sdb.AddBalance(recipient, amount, tracing.BalanceChangeTransfer)
}
