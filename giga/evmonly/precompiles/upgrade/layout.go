package upgrade

import (
	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/solstorage"
)

// Address is the upgrade precompile's address.
var Address = common.HexToAddress("0x0000000000000000000000000000000000001015")

// Top-level slots of docs/giga-storage/UpgradeStorage.sol.
const (
	slotPlan          = 0
	slotAppliedHeight = 7
	slotLastApplied   = 8
)

// UpgradeStorage.Plan members, relative to slotPlan.
const (
	planName   = 0
	planHeight = 1
	planInfo   = 2
	planGap    = 3
)

func newStore(state precompiles.State) solstorage.Store {
	return solstorage.Store{State: state, Address: Address}
}

func planSlot(member uint64) common.Hash {
	return solstorage.Offset(solstorage.Slot(slotPlan), member)
}

func appliedHeightSlot(name string) common.Hash {
	return solstorage.MapString(solstorage.Slot(slotAppliedHeight), name)
}
