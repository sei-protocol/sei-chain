package state_test

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/sei-protocol/sei-chain/x/evm/state"
)

// selfDestructToSelf mirrors the SELFDESTRUCT opcode with the account as its own
// beneficiary: it burns the balance, then marks the account self-destructed.
func selfDestructToSelf(statedb *state.DBImpl, addr common.Address) {
	statedb.SubBalance(addr, statedb.GetBalance(addr), tracing.BalanceDecreaseSelfdestruct)
	statedb.SelfDestruct(addr)
}

// selfDestruct6780 mirrors the EIP-6780 SELFDESTRUCT opcode with the account as
// its own beneficiary and reports whether the account was destructed.
func selfDestruct6780(statedb *state.DBImpl, addr common.Address) bool {
	if !statedb.IsNewContract(addr) {
		return false
	}
	selfDestructToSelf(statedb, addr)
	return true
}
