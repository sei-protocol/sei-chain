package evmonly

import (
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// CheckStatelessRules refuses p when the checks execution runs before charging gas would reject it in
// a block at blockCtx, reading the sender's account from view. It does not check the nonce.
func CheckStatelessRules(view gigatypes.EVMStateView, chainConfig *params.ChainConfig, blockCtx BlockContext, p PreparedTx) error {
	if err := validateSupportedTx(p.Tx); err != nil {
		return err
	}
	msg, err := transactionToPreparedMessage(p, blockCtx.BaseFee)
	if err != nil {
		return err
	}
	// Admission takes transactions ahead of the sender's committed nonce.
	msg.SkipNonceCheck()
	stateDB := newNativeStateDB(gigaSnapshotStateReader{snapshot: view})
	evm := vm.NewEVM(EVMBlockContext(blockCtx), stateDB, chainConfig, vm.Config{})
	stateDB.SetEVM(evm)
	evm.SetTxContext(core.NewEVMTxContext(msg))
	return core.NewStateTransition(evm, msg, core.NewGasPool(blockCtx.GasLimit), false, false).StatelessChecks()
}
