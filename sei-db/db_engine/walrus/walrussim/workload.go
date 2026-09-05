package walrussim

import (
	"fmt"

	crand "github.com/sei-protocol/sei-chain/sei-db/common/rand"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/walrus"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// The address type byte every generated key carries, which keeps these keys distinct from any other
// generator's for the same id.
const walrusAddressType = uint8('w')

// workload turns a block number into the changes that block makes, and separately answers what any key held
// at any block. Neither direction stores anything.
//
// A key with id k belongs to a class with period p and is written at exactly the blocks where
// k is congruent to the block number modulo p. That one rule is what makes the workload both enumerable per
// block, so a block can be produced without scanning every key, and invertible per key, so the expected
// answer to any query is a handful of integer operations rather than a search.
type workload struct {
	classes    []resolvedClass
	random     *crand.CannedRandom
	storeName  string
	keySize    int
	valueSize  int
	deleteRate uint64
	firstBlock uint64

	// Ids below this are written by some class; ids from here up to neverWrittenLimit are written by none.
	liveIDLimit uint64

	// One past the highest id the workload will ever generate a key for.
	neverWrittenLimit uint64

	// How many key changes a block carries, which sizes the slice a block is built into.
	writesPerBlock int
}

// resolvedClass is one key class with its id range resolved.
type resolvedClass struct {
	firstID uint64
	endID   uint64
	period  uint64
}

// newWorkload resolves the configured key classes into contiguous id ranges.
func newWorkload(config *Config) *workload {
	created := &workload{
		random:     crand.NewCannedRandom(config.CannedRandomSize, config.Seed),
		storeName:  config.StoreName,
		keySize:    config.KeySize,
		valueSize:  config.ValueSize,
		deleteRate: config.DeleteRate,
		firstBlock: config.FirstBlock,
	}

	next := uint64(0)
	for _, class := range config.KeyClasses {
		created.classes = append(created.classes, resolvedClass{
			firstID: next,
			endID:   next + class.KeyCount,
			period:  class.Period,
		})
		next += class.KeyCount
		perBlock := int((class.KeyCount + class.Period - 1) / class.Period) //nolint:gosec // G115 - small
		created.writesPerBlock += perBlock
	}
	created.liveIDLimit = next
	created.neverWrittenLimit = next + config.NeverWrittenKeyCount
	return created
}

// block returns the changes block number makes.
func (w *workload) block(number uint64) walrus.Block {
	pairs := make([]*proto.KVPair, 0, w.writesPerBlock)
	for _, class := range w.classes {
		for id := class.firstWriterAt(number); id < class.endID; id += class.period {
			pairs = append(pairs, w.pair(id, number))
		}
	}
	return walrus.Block{
		Number:     number,
		ChangeSets: []*proto.NamedChangeSet{{Name: w.storeName, Changeset: proto.ChangeSet{Pairs: pairs}}},
	}
}

// firstWriterAt returns the lowest id in the class written at the given block.
//
// The class's keys written at a block form an arithmetic sequence stepping by the period, so a block is
// produced by walking that sequence rather than by testing every key.
func (c resolvedClass) firstWriterAt(blockNumber uint64) uint64 {
	period := int64(c.period) //nolint:gosec // G115 - periods stay far below the int64 ceiling
	//nolint:gosec // G115 - block numbers and ids in a benchmark stay far below the int64 ceiling
	remainder := ((int64(blockNumber)-int64(c.firstID))%period + period) % period
	return c.firstID + uint64(remainder) //nolint:gosec // G115 - the remainder is a modulus of a positive period
}

// pair returns the key change id makes at the given block.
func (w *workload) pair(id uint64, blockNumber uint64) *proto.KVPair {
	if w.isDelete(id, blockNumber) {
		return &proto.KVPair{Key: w.key(id), Delete: true}
	}
	return &proto.KVPair{Key: w.key(id), Value: w.value(id, blockNumber)}
}

// expected returns what key id held at the end of a block, without consulting anything that was stored.
func (w *workload) expected(id uint64, blockNumber uint64) (value []byte, found bool) {
	period, live := w.periodOf(id)
	if !live {
		return nil, false
	}

	//nolint:gosec // G115 - block numbers and ids in a benchmark stay far below the int64 ceiling
	signedPeriod := int64(period)
	//nolint:gosec // G115 - as above
	remainder := ((int64(blockNumber)-int64(id))%signedPeriod + signedPeriod) % signedPeriod
	//nolint:gosec // G115 - as above
	lastWrite := int64(blockNumber) - remainder

	//nolint:gosec // G115 - as above
	if lastWrite < int64(w.firstBlock) {
		// The key's first write is still ahead of this block, so nothing has ever written it here.
		return nil, false
	}
	written := uint64(lastWrite) //nolint:gosec // G115 - lastWrite is at or above the first block
	if w.isDelete(id, written) {
		return nil, false
	}
	return w.value(id, written), true
}

// periodOf returns the write period of a key, and whether any class writes it at all.
func (w *workload) periodOf(id uint64) (period uint64, live bool) {
	for _, class := range w.classes {
		if id >= class.firstID && id < class.endID {
			return class.period, true
		}
	}
	return 0, false
}

// key returns the bytes of a key.
//
// CannedRandom.Address reads only its pre-generated buffer, so this is safe to call from every reader
// goroutine at once without cloning the generator.
func (w *workload) key(id uint64) []byte {
	//nolint:gosec // G115 - ids in a benchmark stay far below the int64 ceiling
	return w.random.Address(walrusAddressType, int64(id), w.keySize)
}

// value returns the bytes a key holds after being written at the given block.
//
// The result aliases the generator's buffer rather than copying it. Nothing in the write path or the
// comparison path modifies a value, so sharing it costs nothing.
func (w *workload) value(id uint64, blockNumber uint64) []byte {
	return w.random.SeededBytes(w.valueSize, writeSeed(id, blockNumber))
}

// isDelete reports whether the write a key makes at a block is a deletion.
func (w *workload) isDelete(id uint64, blockNumber uint64) bool {
	if w.deleteRate == 0 {
		return false
	}
	seed := uint64(writeSeed(id, blockNumber)) //nolint:gosec // G115 - a hash, whose sign carries no meaning
	return mix64(seed)%w.deleteRate == 0
}

// writeSeed derives the seed a key's value at a block is generated from.
func writeSeed(id uint64, blockNumber uint64) int64 {
	//nolint:gosec // G115 - the seed is a hash, and its sign carries no meaning
	return int64(mix64(id*0x9E3779B97F4A7C15 ^ blockNumber))
}

// mix64 is the SplitMix64 finalizer, used to spread an id and block into a well distributed seed.
func mix64(value uint64) uint64 {
	value ^= value >> 30
	value *= 0xbf58476d1ce4e5b9
	value ^= value >> 27
	value *= 0x94d049bb133111eb
	value ^= value >> 31
	return value
}

// describe returns a human readable summary of the workload's shape.
func (w *workload) describe() string {
	return fmt.Sprintf("%d live keys across %d classes, %d never-written keys, %d writes per block",
		w.liveIDLimit, len(w.classes), w.neverWrittenLimit-w.liveIDLimit, w.writesPerBlock)
}
