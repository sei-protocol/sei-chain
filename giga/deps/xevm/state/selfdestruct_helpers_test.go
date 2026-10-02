package state_test

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/sei-protocol/sei-chain/giga/deps/xevm/state"
)

// selfDestructToSelf mirrors the SELFDESTRUCT opcode with the account as its own
// beneficiary: the opcode burns the balance before marking the account
// self-destructed (StateDB.SelfDestruct no longer debits the balance).
func selfDestructToSelf(statedb *state.DBImpl, addr common.Address) {
	statedb.SubBalance(addr, statedb.GetBalance(addr), tracing.BalanceDecreaseSelfdestruct)
	statedb.SelfDestruct(addr)
}

// selfDestruct6780 mirrors the EIP-6780 SELFDESTRUCT opcode with the account as
// its own beneficiary (the former StateDB.SelfDestruct6780).
func selfDestruct6780(statedb *state.DBImpl, addr common.Address) bool {
	if !statedb.IsNewContract(addr) {
		return false
	}
	selfDestructToSelf(statedb, addr)
	return true
}
