package evmonlyapp

import (
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// The cursor is stored as a non-EVM write outside keys.EVMStoreKey, so it
// shares the block's storage version without entering the EVM state hash.
const (
	evmOnlyCursorModule = "evmonly"
	evmOnlyCursorKey    = "cursor"
	evmOnlyCursorSize   = 8 + common.HashLength + common.HashLength + 8 + (evmOnlyBaseFeeLag+1)*32
)

// evmOnlyBaseFees are the base fee a block ran at, then those set for the
// evmOnlyBaseFeeLag blocks after it, in height order.
type evmOnlyBaseFees [evmOnlyBaseFeeLag + 1]uint256.Int

// genesisBaseFees returns the base fees of the cursor InitChain writes:
// evmOnlyBlockMinGasPrice, at every height.
func genesisBaseFees() evmOnlyBaseFees {
	var fees evmOnlyBaseFees
	for i := range fees {
		fees[i].SetUint64(evmOnlyBlockMinGasPrice)
	}
	return fees
}

// evmOnlyCursor identifies a block whose state is committed to storage and
// what the next block executes against.
type evmOnlyCursor struct {
	height    int64
	appHash   common.Hash
	blockHash common.Hash
	gasLimit  uint64
	baseFees  evmOnlyBaseFees
}

// baseFee returns the base fee this block ran at.
func (c evmOnlyCursor) baseFee() *big.Int {
	return c.baseFees[0].ToBig()
}

// nextBaseFee returns the base fee the block after this one runs at.
func (c evmOnlyCursor) nextBaseFee() *big.Int {
	return c.baseFees[1].ToBig()
}

// highestScheduledBaseFee returns the highest base fee set for the blocks
// after this one.
func (c evmOnlyCursor) highestScheduledBaseFee() *big.Int {
	highest := c.baseFees[1]
	for _, fee := range c.baseFees[2:] {
		if fee.Gt(&highest) {
			highest = fee
		}
	}
	return highest.ToBig()
}

// nextBaseFees returns the base fees of the next block's cursor when the next
// block used gasUsed of its gasLimit: the ones this cursor set, then the one
// that use sets under params, never below evmOnlyBlockMinGasPrice.
func (c evmOnlyCursor) nextBaseFees(params evmonly.BaseFeeParams, gasUsed, gasLimit uint64) evmOnlyBaseFees {
	var fees evmOnlyBaseFees
	copy(fees[:], c.baseFees[1:])
	next := params.NextBaseFee(c.baseFees[len(c.baseFees)-1], gasUsed, gasLimit)
	if minGasPrice := uint256.NewInt(evmOnlyBlockMinGasPrice); next.Lt(minGasPrice) {
		next = *minGasPrice
	}
	fees[len(fees)-1] = next
	return fees
}

func (c evmOnlyCursor) miscChange() gigatypes.MiscChange {
	return gigatypes.MiscChange{
		Module: evmOnlyCursorModule,
		Key:    []byte(evmOnlyCursorKey),
		Value:  c.encode(),
	}
}

func (c evmOnlyCursor) encode() []byte {
	buf := make([]byte, 0, evmOnlyCursorSize)
	buf = binary.BigEndian.AppendUint64(buf, uint64(c.height)) //nolint:gosec // G115: height is non-negative.
	buf = append(buf, c.appHash[:]...)
	buf = append(buf, c.blockHash[:]...)
	buf = binary.BigEndian.AppendUint64(buf, c.gasLimit)
	for _, fee := range c.baseFees {
		raw := fee.Bytes32()
		buf = append(buf, raw[:]...)
	}
	return buf
}

func decodeEVMOnlyCursor(raw []byte) (evmOnlyCursor, error) {
	if len(raw) != evmOnlyCursorSize {
		return evmOnlyCursor{}, fmt.Errorf("EVM-only cursor is %d bytes, want %d", len(raw), evmOnlyCursorSize)
	}
	height, ok := utils.SafeCast[int64](binary.BigEndian.Uint64(raw))
	if !ok {
		return evmOnlyCursor{}, fmt.Errorf("EVM-only cursor height exceeds int64")
	}
	raw = raw[8:]
	cursor := evmOnlyCursor{
		height:    height,
		appHash:   common.BytesToHash(raw[:common.HashLength]),
		blockHash: common.BytesToHash(raw[common.HashLength : 2*common.HashLength]),
		gasLimit:  binary.BigEndian.Uint64(raw[2*common.HashLength:]),
	}
	fees := raw[2*common.HashLength+8:]
	for i := range cursor.baseFees {
		cursor.baseFees[i].SetBytes32(fees[i*32:])
	}
	return cursor, nil
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
