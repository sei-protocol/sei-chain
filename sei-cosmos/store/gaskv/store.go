package gaskv

import (
	"io"
	"time"

	"github.com/cosmos/cosmos-sdk/store/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
)

type IStoreTracer interface {
	Get([]byte, []byte, string)
	Has([]byte, string)
	Set([]byte, []byte, string)
	Delete([]byte, string)
	DerivePrestateToJson() []byte
	Clear()
}

var _ types.KVStore = &Store{}

// Store applies gas tracking to an underlying KVStore. It implements the
// KVStore interface.
type Store struct {
	gasMeter   types.GasMeter
	gasConfig  types.GasConfig
	parent     types.KVStore
	moduleName string
	tracer     IStoreTracer
}

// NewStore returns a reference to a new GasKVStore.
func NewStore(parent types.KVStore, gasMeter types.GasMeter, gasConfig types.GasConfig, moduleName string, tracer IStoreTracer) *Store {
	kvs := &Store{
		gasMeter:   gasMeter,
		gasConfig:  gasConfig,
		parent:     parent,
		moduleName: moduleName,
		tracer:     tracer,
	}
	return kvs
}

// Implements Store.
func (gs *Store) GetStoreType() types.StoreType {
	return gs.parent.GetStoreType()
}

func (gs *Store) GetWorkingHash() ([]byte, error) {
	return gs.parent.GetWorkingHash()
}

// Implements KVStore.
func (gs *Store) Get(key []byte) (value []byte) {
	gs.gasMeter.ConsumeGas(gs.gasConfig.ReadCostFlat, types.GasReadCostFlatDesc)
	value = gs.parent.Get(key)

	// TODO overflow-safe math?
	gs.gasMeter.ConsumeGas(gs.gasConfig.ReadCostPerByte*types.Gas(len(key)), types.GasReadPerByteDesc)
	gs.gasMeter.ConsumeGas(gs.gasConfig.ReadCostPerByte*types.Gas(len(value)), types.GasReadPerByteDesc)
	if gs.tracer != nil {
		gs.tracer.Get(key, value, gs.moduleName)
	}

	return value
}

// Implements KVStore.
func (gs *Store) Set(key []byte, value []byte) {
	types.AssertValidKey(key)
	types.AssertValidValue(value)
	gs.gasMeter.ConsumeGas(gs.gasConfig.WriteCostFlat, types.GasWriteCostFlatDesc)
	// TODO overflow-safe math?
	gs.gasMeter.ConsumeGas(gs.gasConfig.WriteCostPerByte*types.Gas(len(key)), types.GasWritePerByteDesc)
	gs.gasMeter.ConsumeGas(gs.gasConfig.WriteCostPerByte*types.Gas(len(value)), types.GasWritePerByteDesc)
	gs.parent.Set(key, value)
	if gs.tracer != nil {
		gs.tracer.Set(key, value, gs.moduleName)
	}
}

// Implements KVStore.
func (gs *Store) Has(key []byte) bool {
	defer telemetry.MeasureSince(time.Now(), "store", "gaskv", "has")
	gs.gasMeter.ConsumeGas(gs.gasConfig.HasCost, types.GasHasDesc)
	res := gs.parent.Has(key)
	if gs.tracer != nil && res {
		gs.tracer.Has(key, gs.moduleName)
	}
	return res
}

// Implements KVStore.
func (gs *Store) Delete(key []byte) {
	defer telemetry.MeasureSince(time.Now(), "store", "gaskv", "delete")
	// charge gas to prevent certain attack vectors even though space is being freed
	gs.gasMeter.ConsumeGas(gs.gasConfig.DeleteCost, types.GasDeleteDesc)
	gs.parent.Delete(key)
	if gs.tracer != nil {
		gs.tracer.Delete(key, gs.moduleName)
	}
}

// Iterator implements the KVStore interface. It returns an iterator which
// incurs a flat gas cost for seeking to the first key/value pair and a variable
// gas cost based on the current value's length if the iterator is valid.
func (gs *Store) Iterator(start, end []byte) types.Iterator {
	return gs.iterator(start, end, true)
}

// ReverseIterator implements the KVStore interface. It returns a reverse
// iterator which incurs a flat gas cost for seeking to the first key/value pair
// and a variable gas cost based on the current value's length if the iterator
// is valid.
func (gs *Store) ReverseIterator(start, end []byte) types.Iterator {
	return gs.iterator(start, end, false)
}

// Implements KVStore.
func (gs *Store) CacheWrap(_ types.StoreKey) types.CacheWrap {
	panic("cannot CacheWrap a GasKVStore")
}

// CacheWrapWithTrace implements the KVStore interface.
func (gs *Store) CacheWrapWithTrace(_ types.StoreKey, _ io.Writer, _ types.TraceContext) types.CacheWrap {
	panic("cannot CacheWrapWithTrace a GasKVStore")
}

// CacheWrapWithListeners implements the CacheWrapper interface.
func (gs *Store) CacheWrapWithListeners(_ types.StoreKey, _ []types.WriteListener) types.CacheWrap {
	panic("cannot CacheWrapWithListeners a GasKVStore")
}

func (gs *Store) iterator(start, end []byte, ascending bool) types.Iterator {
	var parent types.Iterator
	if ascending {
		parent = gs.parent.Iterator(start, end)
	} else {
		parent = gs.parent.ReverseIterator(start, end)
	}

	gi := newGasIterator(gs.gasMeter, gs.gasConfig, parent, gs.moduleName, gs.tracer)
	defer func() {
		if err := recover(); err != nil {
			// if there is a panic, we close the iterator then reraise
			gi.Close()
			panic(err)
		}
	}()
	gi.(*gasIterator).consumeSeekGas()

	return gi
}

func (gs *Store) VersionExists(version int64) bool {
	return gs.parent.VersionExists(version)
}

func (gs *Store) DeleteAll(start, end []byte) error {
	return gs.parent.DeleteAll(start, end)
}

func (gs *Store) GetAllKeyStrsInRange(start, end []byte) (res []string) {
	return gs.parent.GetAllKeyStrsInRange(start, end)
}

type gasIterator struct {
	gasMeter   types.GasMeter
	gasConfig  types.GasConfig
	parent     types.Iterator
	moduleName string
	tracer     IStoreTracer
}

func newGasIterator(gasMeter types.GasMeter, gasConfig types.GasConfig, parent types.Iterator, moduleName string, tracer IStoreTracer) types.Iterator {
	return &gasIterator{
		gasMeter:   gasMeter,
		gasConfig:  gasConfig,
		parent:     parent,
		moduleName: moduleName,
		tracer:     tracer,
	}
}

// Implements Iterator.
func (gi *gasIterator) Domain() (start []byte, end []byte) {
	return gi.parent.Domain()
}

// Implements Iterator.
func (gi *gasIterator) Valid() bool {
	return gi.parent.Valid()
}

// Next implements the Iterator interface. It seeks to the next key/value pair
// in the iterator. It incurs a flat gas cost for seeking and a variable gas
// cost based on the current value's length if the iterator is valid.
func (gi *gasIterator) Next() {
	gi.consumeSeekGas()
	gi.parent.Next()
}

// Key implements the Iterator interface. It returns the current key and it does
// not incur any gas cost.
func (gi *gasIterator) Key() (key []byte) {
	key = gi.parent.Key()
	if gi.tracer != nil {
		gi.tracer.Has(key, gi.moduleName)
	}
	return key
}

// Value implements the Iterator interface. It returns the current value and it
// does not incur any gas cost.
func (gi *gasIterator) Value() (value []byte) {
	value = gi.parent.Value()
	if gi.tracer != nil {
		gi.tracer.Get(gi.Key(), value, gi.moduleName)
	}
	return value
}

// Implements Iterator.
func (gi *gasIterator) Close() error {
	return gi.parent.Close()
}

// Error delegates the Error call to the parent iterator.
func (gi *gasIterator) Error() error {
	return gi.parent.Error()
}

// consumeSeekGas consumes on each iteration step a flat gas cost and a variable gas cost
// based on the current value's length.
func (gi *gasIterator) consumeSeekGas() {
	if gi.Valid() {
		key := gi.Key()
		value := gi.Value()

		gi.gasMeter.ConsumeGas(gi.gasConfig.ReadCostPerByte*types.Gas(len(key)), types.GasValuePerByteDesc)
		gi.gasMeter.ConsumeGas(gi.gasConfig.ReadCostPerByte*types.Gas(len(value)), types.GasValuePerByteDesc)
	}

	gi.gasMeter.ConsumeGas(gi.gasConfig.IterNextCostFlat, types.GasIterNextCostFlatDesc)
}
