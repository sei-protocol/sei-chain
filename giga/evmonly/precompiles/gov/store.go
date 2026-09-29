package gov

import (
	"encoding/binary"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Proposal kinds.
const (
	kindSoftwareUpgrade       uint64 = 1
	kindCancelSoftwareUpgrade uint64 = 2
)

// Proposal field slots, relative to a proposal.
const (
	fieldKind byte = iota + 1
	fieldStatus
	fieldProposer
	fieldSubmitTime
	fieldVotingEndTime
	fieldPlanHeight
	fieldTallyYes
	fieldTallyAbstain
	fieldTallyNo
	fieldTallyNoWithVeto
	fieldTitle
	fieldDescription
	fieldPlanName
	fieldPlanInfo
)

// Upgrade plan field slots.
const (
	planFieldHeight byte = iota + 1
	planFieldName
	planFieldInfo
	planFieldProposal
)

var (
	slotProposalCount = storageKey("proposal-count")
	slotQueueHead     = storageKey("queue-head")
	slotQueueTail     = storageKey("queue-tail")
)

// storageKey derives a slot of the precompile's storage. Every slot is a
// keccak256 image under a distinct label, so no two layouts overlap.
func storageKey(label string, parts ...[]byte) common.Hash {
	data := make([][]byte, 0, len(parts)+1)
	data = append(data, []byte("sei.giga.gov/"+label))
	data = append(data, parts...)
	return crypto.Keccak256Hash(data...)
}

func u64Bytes(v uint64) []byte { return binary.BigEndian.AppendUint64(nil, v) }

func proposalSlot(id uint64, field byte) common.Hash {
	return storageKey("proposal", u64Bytes(id), []byte{field})
}

func voteSlot(id uint64, voter common.Address) common.Hash {
	return storageKey("vote", u64Bytes(id), voter[:])
}

func queueSlot(index uint64) common.Hash {
	return storageKey("queue", u64Bytes(index))
}

func planSlot(field byte) common.Hash {
	return storageKey("plan", []byte{field})
}

// doneSlot holds the height at which the upgrade named name was completed.
func doneSlot(name string) common.Hash {
	return storageKey("done", []byte(name))
}

// Reader reads the precompile's storage.
type Reader interface {
	GetState(common.Address, common.Hash) common.Hash
}

// Writer reads and writes the precompile's storage.
type Writer interface {
	Reader
	SetState(common.Address, common.Hash, common.Hash)
}

// store is the precompile's storage at one address.
type store struct {
	addr common.Address
	db   Reader
}

func (s store) u64(slot common.Hash) uint64 {
	value := s.db.GetState(s.addr, slot)
	return binary.BigEndian.Uint64(value[common.HashLength-8:])
}

func (s store) address(slot common.Hash) common.Address {
	return common.BytesToAddress(s.db.GetState(s.addr, slot).Bytes())
}

// str reads a string written by setStr: its length at slot, then 32-byte chunks.
func (s store) str(slot common.Hash) string {
	n := s.u64(slot)
	out := make([]byte, 0, n)
	for i := uint64(0); uint64(len(out)) < n; i++ {
		chunk := s.db.GetState(s.addr, stringChunkSlot(slot, i))
		out = append(out, chunk[:min(uint64(common.HashLength), n-uint64(len(out)))]...)
	}
	return string(out)
}

func stringChunkSlot(slot common.Hash, index uint64) common.Hash {
	return storageKey("string-chunk", slot[:], u64Bytes(index))
}

// stringSlots is the number of slots setStr uses for a string of n bytes.
func stringSlots(n int) uint64 {
	return 1 + uint64((n+common.HashLength-1)/common.HashLength) //nolint:gosec // n is a non-negative length.
}

// writer is a store that can also write.
type writer struct {
	store
	db Writer
}

func newWriter(addr common.Address, db Writer) writer {
	return writer{store: store{addr: addr, db: db}, db: db}
}

func (w writer) setU64(slot common.Hash, v uint64) {
	var value common.Hash
	binary.BigEndian.PutUint64(value[common.HashLength-8:], v)
	w.db.SetState(w.addr, slot, value)
}

func (w writer) setAddress(slot common.Hash, addr common.Address) {
	w.db.SetState(w.addr, slot, common.BytesToHash(addr[:]))
}

// setStr writes v at slot, first clearing any longer string stored there.
func (w writer) setStr(slot common.Hash, v string) {
	old := w.u64(slot)
	chunks := uint64((len(v) + common.HashLength - 1) / common.HashLength)
	oldChunks := (old + common.HashLength - 1) / common.HashLength
	for i := chunks; i < oldChunks; i++ {
		w.db.SetState(w.addr, stringChunkSlot(slot, i), common.Hash{})
	}
	for i := range chunks {
		var chunk common.Hash
		copy(chunk[:], v[i*common.HashLength:])
		w.db.SetState(w.addr, stringChunkSlot(slot, i), chunk)
	}
	w.setU64(slot, uint64(len(v)))
}
