package evmonlyapp

import (
	"encoding/binary"
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// The cursor is stored as a named changeset outside keys.EVMStoreKey, so it
// shares the block's storage version without entering the EVM state hash.
const (
	evmOnlyCursorModule = "evmonly"
	evmOnlyCursorKey    = "cursor"
	evmOnlyCursorSize   = 8 + common.HashLength + common.HashLength + common.HashLength + 8
	// evmOnlyCursorLegacySize is the width before the cursor carried prevRandao.
	// TEMPORARY: remove once no running chain still holds a cursor this wide.
	evmOnlyCursorLegacySize = evmOnlyCursorSize - common.HashLength
)

// evmOnlyCursor identifies a block whose state is committed to storage and
// what the next block executes against.
type evmOnlyCursor struct {
	height    int64
	appHash   common.Hash
	blockHash common.Hash
	// prevRandao is the PREVRANDAO this block executed with; EvmCall replays it.
	prevRandao common.Hash
	gasLimit   uint64
}

func (c evmOnlyCursor) changeSet() *proto.NamedChangeSet {
	return &proto.NamedChangeSet{
		Name: evmOnlyCursorModule,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{
			Key:   []byte(evmOnlyCursorKey),
			Value: c.encode(),
		}}},
	}
}

func (c evmOnlyCursor) encode() []byte {
	buf := make([]byte, 0, evmOnlyCursorSize)
	buf = binary.BigEndian.AppendUint64(buf, uint64(c.height)) //nolint:gosec // G115: height is non-negative.
	buf = append(buf, c.appHash[:]...)
	buf = append(buf, c.blockHash[:]...)
	buf = append(buf, c.prevRandao[:]...)
	return binary.BigEndian.AppendUint64(buf, c.gasLimit)
}

func decodeEVMOnlyCursor(raw []byte) (evmOnlyCursor, error) {
	raw = migrateLegacyEVMOnlyCursor(raw)
	if len(raw) != evmOnlyCursorSize {
		return evmOnlyCursor{}, fmt.Errorf("EVM-only cursor is %d bytes, want %d", len(raw), evmOnlyCursorSize)
	}
	height, ok := utils.SafeCast[int64](binary.BigEndian.Uint64(raw))
	if !ok {
		return evmOnlyCursor{}, fmt.Errorf("EVM-only cursor height exceeds int64")
	}
	raw = raw[8:]
	return evmOnlyCursor{
		height:     height,
		appHash:    common.BytesToHash(raw[:common.HashLength]),
		blockHash:  common.BytesToHash(raw[common.HashLength : 2*common.HashLength]),
		prevRandao: common.BytesToHash(raw[2*common.HashLength : 3*common.HashLength]),
		gasLimit:   binary.BigEndian.Uint64(raw[3*common.HashLength:]),
	}, nil
}

// migrateLegacyEVMOnlyCursor widens a cursor written before prevRandao existed;
// anything else is returned untouched. Without it a node that committed a block
// under the older width refuses to start.
//
// prevRandao sits ahead of gasLimit, so it is spliced in, not appended: appending
// would read the gas limit out of the zeroed bytes. Every node fills the same zero
// for the block it resumes, and the next commit persists the full width.
//
// TEMPORARY, with evmOnlyCursorLegacySize.
func migrateLegacyEVMOnlyCursor(raw []byte) []byte {
	if len(raw) != evmOnlyCursorLegacySize {
		return raw
	}
	const head = 8 + 2*common.HashLength
	widened := make([]byte, 0, evmOnlyCursorSize)
	widened = append(widened, raw[:head]...)
	widened = append(widened, make([]byte, common.HashLength)...)
	return append(widened, raw[head:]...)
}

// loadEVMOnlyCursor reads the cursor of the store's latest version. It is None
// for an empty store and for one seeded by InitChain with no block committed
// yet; both are resumed through InitChain.
func loadEVMOnlyCursor(store *flatkv.CommitStore) (utils.Option[evmOnlyCursor], error) {
	latest, err := store.GetLatestVersion()
	if err != nil {
		return utils.None[evmOnlyCursor](), fmt.Errorf("read EVM-only state version: %w", err)
	}
	if latest == 0 {
		return utils.None[evmOnlyCursor](), nil
	}
	raw, found := store.Get(evmOnlyCursorModule, []byte(evmOnlyCursorKey))
	if !found {
		return utils.None[evmOnlyCursor](), nil
	}
	cursor, err := decodeEVMOnlyCursor(raw)
	if err != nil {
		return utils.None[evmOnlyCursor](), err
	}
	if cursor.height != latest {
		return utils.None[evmOnlyCursor](), fmt.Errorf("EVM-only cursor is at height %d but state is at %d", cursor.height, latest)
	}
	return utils.Some(cursor), nil
}
