// Package kvrepairtest rehearses the A8 recovery action: repairing a known,
// enumerated set of EVM storage slots at a coordinated future height.
//
// It registers two handlers against the same chain. The first writes a sentinel
// value over a set of slots, standing in for the state damage an incident would
// leave behind. The second writes the original values back, which is the action
// A8 performs for real. Running both on one node turns the rehearsal into a
// closed loop: the logical digest agrees with the reserve, diverges after the
// damage height, and agrees again after the repair height.
//
// This package is a test fixture. It must only ever be compiled into a build
// destined for a disposable node. HardForkManager filters handlers by chain ID,
// so a handler whose target chain does not match is dropped at registration,
// but that filter is a backstop and not a licence to ship this in a release.
package kvrepairtest

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/app/upgrades"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	evmkeeper "github.com/sei-protocol/sei-chain/x/evm/keeper"
)

// Slot pairs one storage key with the value the reserve holds for it. Original
// is the pre-damage reading, so the repair handler restores exactly what was
// there.
type Slot struct {
	Key      common.Hash
	Original common.Hash
}

// Target chain and heights for this rehearsal. The chain ID is deliberately a
// live chain: HardForkManager drops handlers whose chain does not match, so a
// placeholder would make the fixture silently inert. Deployment discipline, not
// this constant, is what keeps the build off production nodes.
const (
	TargetChainID = "arctic-1"
	DamageHeight  = 186914000
	RepairHeight  = 186918000
)

// Contract owns the storage slots this rehearsal touches.
var Contract = common.HexToAddress("0x0000000000000068f116a894984e2db1123eb395")

// Slots are six storage entries of Contract, each verified unchanged between
// heights 186830000 and 186900000 and read back identically through
// eth_getStorageAt. Cold entries keep the damage from spreading: no transaction
// reads them, so nothing downstream derives a second wrong value from the first.
var Slots = []Slot{
	{Key: common.HexToHash("0x02595f0765d65bb37816a64423e645d24e0ac6dbf5358069a39e6bd6511b2a40"), Original: common.HexToHash("0x0000000000000000000000000000020000000000000000000000000000020001")},
	{Key: common.HexToHash("0x08a4039dd9d5164dd4a5c8542669b7b2e3dad99d11e1538e1c57ec9a19e5aa61"), Original: common.HexToHash("0x0000000000000000000000000000010000000000000000000000000000010001")},
	{Key: common.HexToHash("0x0f13fca7683d83aa1095b451dd34106864a274c9f6ecbeea3868d3f48d83a162"), Original: common.HexToHash("0x0000000000000000000000000000010000000000000000000000000000010001")},
	{Key: common.HexToHash("0x1ba66ef248dae5468aa17aaaccf1d5ed9533ccd73da4960be5cdf004f69e2143"), Original: common.HexToHash("0x0000000000000000000000000000010000000000000000000000000000010001")},
	{Key: common.HexToHash("0x22cc78e6e2d912e3d749bbe5ffd7c689ff67967cef32a32cfcbb6fc57b452802"), Original: common.HexToHash("0x0000000000000000000000000000010000000000000000000000000000010001")},
	{Key: common.HexToHash("0x2c326e5a371a85a802e8e773a7d149091110dd3b17cdd4659a60f24b732f02e7"), Original: common.HexToHash("0x0000000000000000000000000000010000000000000000000000000000010001")},
}

// sentinel is written over each slot at the damage height. It is a recognisable
// constant rather than a plausible value, so a reader who finds it in a dump
// knows immediately that it came from this fixture.
//
// It must be non-zero: Keeper.SetState deletes the entry when handed the zero
// hash, which would remove the key rather than corrupt it. A removal is a
// different kind of damage and would not exercise the repair path.
var sentinel = common.HexToHash("0xDEAD00000000000000000000000000000000000000000000000000000000DEAD")

// Mode selects which of the two operations a handler performs.
type Mode int

const (
	// ModeDamage overwrites every slot with the sentinel.
	ModeDamage Mode = iota
	// ModeRepair writes each slot's original value back.
	ModeRepair
)

func (m Mode) String() string {
	if m == ModeDamage {
		return "damage"
	}
	return "repair"
}

// Handler implements upgrades.HardForkHandler for one operation at one height.
type Handler struct {
	mode          Mode
	targetHeight  int64
	targetChainID string
	contract      common.Address
	slots         []Slot
	evmKeeper     *evmkeeper.Keeper
}

// NewHandler builds one half of the rehearsal. Pair two of them, a damage
// handler at the earlier height and a repair handler at the later one.
func NewHandler(
	mode Mode,
	targetHeight int64,
	targetChainID string,
	contract common.Address,
	slots []Slot,
	k *evmkeeper.Keeper,
) upgrades.HardForkHandler {
	return Handler{
		mode:          mode,
		targetHeight:  targetHeight,
		targetChainID: targetChainID,
		contract:      contract,
		slots:         slots,
		evmKeeper:     k,
	}
}

// Register wires both halves of the rehearsal onto the manager.
func Register(m *upgrades.HardForkManager, k *evmkeeper.Keeper) {
	m.RegisterHandler(NewHandler(ModeDamage, DamageHeight, TargetChainID, Contract, Slots, k))
	m.RegisterHandler(NewHandler(ModeRepair, RepairHeight, TargetChainID, Contract, Slots, k))
}

// GetName distinguishes the two handlers, which HardForkManager requires: it
// panics on a duplicate name.
func (h Handler) GetName() string {
	return fmt.Sprintf("kv-repair-test-%s-%d", h.mode, h.targetHeight)
}

func (h Handler) GetTargetChainID() string { return h.targetChainID }

func (h Handler) GetTargetHeight() int64 { return h.targetHeight }

// ExecuteHandler writes every slot and verifies each write by reading it back.
// A returned error reaches HardForkManager, which panics, so a partial write
// stops the node instead of leaving it running on half-applied state.
func (h Handler) ExecuteHandler(ctx sdk.Context) error {
	for i, s := range h.slots {
		want := s.Original
		if h.mode == ModeDamage {
			want = sentinel
		}

		h.evmKeeper.SetState(ctx, h.contract, s.Key, want)

		if got := h.evmKeeper.GetState(ctx, h.contract, s.Key); got != want {
			return fmt.Errorf(
				"kvrepairtest %s: slot %d (%s): wrote %s, read back %s",
				h.mode, i, s.Key.Hex(), want.Hex(), got.Hex(),
			)
		}
	}
	return nil
}
