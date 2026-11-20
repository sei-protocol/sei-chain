package evmonlyapp

import (
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"

	chainparams "github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/params"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// The cursor is stored as a non-EVM write outside keys.EVMStoreKey, so it
// shares the block's storage version without entering the EVM state hash.
const (
	evmOnlyCursorModule = "evmonly"
	evmOnlyCursorKey    = "cursor"
	// evmOnlyCursorHeaderSize is the size of the cursor's fields before its base fees.
	evmOnlyCursorHeaderSize = 8 + common.HashLength + common.HashLength + 8
)

// evmOnlyBaseFees are the base fee a block ran at, then those set for the blocks after it, in
// height order. At least one and at most chainparams.MaxBaseFeeLag follow the first.
type evmOnlyBaseFees []uint256.Int

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

// nextBaseFees returns the base fees of the next block's cursor when the next block used gasUsed
// of its gasLimit. They are the ones this cursor set and, when those set fewer than rules.lag
// blocks after the next one, the last of them held for each block no gas use sets, then the one
// that use sets. A base fee once set never changes, so after the lag falls the cursor sets none
// until the ones already set cover fewer blocks than the lag.
func (c evmOnlyCursor) nextBaseFees(rules baseFeeRules, gasUsed, gasLimit uint64) evmOnlyBaseFees {
	fees := append(evmOnlyBaseFees(nil), c.baseFees[1:]...)
	if len(fees)-1 >= rules.lag {
		return fees
	}
	for len(fees) < rules.lag {
		fees = append(fees, rules.step.Hold(fees[len(fees)-1]))
	}
	return append(fees, rules.step.NextBaseFee(fees[len(fees)-1], gasUsed, gasLimit))
}

func (c evmOnlyCursor) miscChange() gigatypes.MiscChange {
	return gigatypes.MiscChange{
		Module: evmOnlyCursorModule,
		Key:    []byte(evmOnlyCursorKey),
		Value:  c.encode(),
	}
}

func (c evmOnlyCursor) encode() []byte {
	buf := make([]byte, 0, evmOnlyCursorHeaderSize+len(c.baseFees)*32)
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
	feeBytes := len(raw) - evmOnlyCursorHeaderSize
	fees := feeBytes / 32
	if feeBytes < 0 || feeBytes%32 != 0 || fees < 2 || fees > int(chainparams.MaxBaseFeeLag)+1 {
		return evmOnlyCursor{}, fmt.Errorf("EVM-only cursor is %d bytes, want %d plus 2 to %d base fees of 32", len(raw), evmOnlyCursorHeaderSize, chainparams.MaxBaseFeeLag+1)
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
		baseFees:  make(evmOnlyBaseFees, fees),
	}
	raw = raw[2*common.HashLength+8:]
	for i := range cursor.baseFees {
		cursor.baseFees[i].SetBytes32(raw[i*32:])
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
