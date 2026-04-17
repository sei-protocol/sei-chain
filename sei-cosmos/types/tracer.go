package types

import (
	"encoding/hex"
	"encoding/json"
	"slices"
	"sync"
	"time"

	seidbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// Per-tx tracer caps. Bound memory and wire size so a pathological tx (huge
// iterator scans, many opened iterators) can't make the profile response
// unbounded. Values picked to comfortably cover normal EVM txs while
// capping worst-case overhead at a few MB per module.
const (
	maxStoreTraceIterators    = 16
	maxStoreTraceIteratorKeys = 64
	maxLowLevelReadSamples    = 128
)

// StoreTracer collects every KVStore access (Get/Has/Set/Delete/iterator)
// performed under a debug_traceTransactionProfile call, grouped by module.
// One tracer is attached to the sdk.Context for the duration of the traced
// tx; callers must treat it as single-tx-scoped. All methods are safe for
// concurrent use.
type StoreTracer struct {
	Modules        map[string]*ModuleTrace
	nextIteratorID int
	mu             *sync.Mutex
}

// ModuleTrace holds every access event for a single module within a trace,
// plus a per-iterator roll-up and a bounded sample of low-level read events
// emitted by the underlying state store.
type ModuleTrace struct {
	Accesses        []Access
	Iterators       []*IteratorTrace
	LowLevelReads   []seidbtypes.ReadTraceEvent
	iteratorIndexBy map[int]int
}

// IteratorTrace aggregates one opened iterator: its bounds, direction, the
// keys it surfaced to the tx (capped at maxStoreTraceIteratorKeys; Truncated
// flags overflow), and cumulative Next() count + time.
type IteratorTrace struct {
	Start         []byte
	End           []byte
	Ascending     bool
	Keys          [][]byte
	NextCount     int
	DurationNanos int64
	Truncated     bool
}

// Access is a single access event in a module's chronological log. Value is
// only populated for Get/Set/IteratorValue; other ops leave it nil.
type Access struct {
	Op            OpType
	Key           []byte
	Value         []byte
	DurationNanos int64
}

// OpType tags an Access with the operation the tx performed.
type OpType int

const (
	Get OpType = iota
	Has
	Set
	Delete
	IteratorOpen
	IteratorNext
	IteratorValue
)

// String returns the JSON/report-friendly name for the op.
func (o OpType) String() string {
	switch o {
	case Get:
		return "get"
	case Has:
		return "has"
	case Set:
		return "set"
	case Delete:
		return "delete"
	case IteratorOpen:
		return "iterator"
	case IteratorNext:
		return "iteratorNext"
	case IteratorValue:
		return "iteratorValue"
	default:
		return "unknown"
	}
}

// NewStoreTracer returns an empty StoreTracer ready to record per-module
// access events for a single debug_traceTransactionProfile call.
func NewStoreTracer() *StoreTracer {
	return &StoreTracer{
		Modules: map[string]*ModuleTrace{},
		mu:      &sync.Mutex{},
	}
}

// Get records a KVStore.Get access against module.
func (st *StoreTracer) Get(key []byte, value []byte, module string, duration time.Duration) {
	st.recordAccess(module, Access{
		Op:            Get,
		Key:           slices.Clone(key),
		Value:         slices.Clone(value),
		DurationNanos: duration.Nanoseconds(),
	})
}

// Set records a KVStore.Set access against module.
func (st *StoreTracer) Set(key []byte, value []byte, module string, duration time.Duration) {
	st.recordAccess(module, Access{
		Op:            Set,
		Key:           slices.Clone(key),
		Value:         slices.Clone(value),
		DurationNanos: duration.Nanoseconds(),
	})
}

// Has records a KVStore.Has access against module.
func (st *StoreTracer) Has(key []byte, module string, duration time.Duration) {
	st.recordAccess(module, Access{
		Op:            Has,
		Key:           slices.Clone(key),
		DurationNanos: duration.Nanoseconds(),
	})
}

// Delete records a KVStore.Delete access against module.
func (st *StoreTracer) Delete(key []byte, module string, duration time.Duration) {
	st.recordAccess(module, Access{
		Op:            Delete,
		Key:           slices.Clone(key),
		DurationNanos: duration.Nanoseconds(),
	})
}

// StartIterator records the opening of an iterator over [start, end) and
// allocates a tracer-scoped iteratorID the caller uses to tag subsequent
// Next/Value events. Past maxStoreTraceIterators the IteratorTrace record is
// dropped (the access-log event is still recorded) and the returned ID lets
// later calls no-op gracefully.
func (st *StoreTracer) StartIterator(start, end []byte, ascending bool, module string, duration time.Duration) int {
	st.mu.Lock()
	defer st.mu.Unlock()

	mt := st.getOrSetModuleTrace(module)
	st.nextIteratorID++
	iteratorID := st.nextIteratorID

	mt.Accesses = append(mt.Accesses, Access{
		Op:            IteratorOpen,
		Key:           slices.Clone(start),
		Value:         slices.Clone(end),
		DurationNanos: duration.Nanoseconds(),
	})

	if len(mt.Iterators) >= maxStoreTraceIterators {
		return iteratorID
	}

	idx := len(mt.Iterators)
	mt.Iterators = append(mt.Iterators, &IteratorTrace{
		Start:         slices.Clone(start),
		End:           slices.Clone(end),
		Ascending:     ascending,
		DurationNanos: duration.Nanoseconds(),
	})
	mt.iteratorIndexBy[iteratorID] = idx
	return iteratorID
}

// RecordIteratorValue records that the tx read the current key/value from
// the iterator identified by iteratorID. Beyond maxStoreTraceIteratorKeys the
// iterator is flagged Truncated and further keys are dropped from the
// per-iterator sample (the access-log event is still recorded).
func (st *StoreTracer) RecordIteratorValue(iteratorID int, key []byte, value []byte, module string) {
	st.mu.Lock()
	defer st.mu.Unlock()

	mt := st.getOrSetModuleTrace(module)
	mt.Accesses = append(mt.Accesses, Access{
		Op:    IteratorValue,
		Key:   slices.Clone(key),
		Value: slices.Clone(value),
	})

	idx, ok := mt.iteratorIndexBy[iteratorID]
	if !ok {
		return
	}
	it := mt.Iterators[idx]
	if len(it.Keys) >= maxStoreTraceIteratorKeys {
		it.Truncated = true
		return
	}
	it.Keys = append(it.Keys, slices.Clone(key))
}

// RecordIteratorNext records a Next() advance on the iterator identified by
// iteratorID, adding to its cumulative step count and stepping time.
func (st *StoreTracer) RecordIteratorNext(iteratorID int, module string, duration time.Duration) {
	st.mu.Lock()
	defer st.mu.Unlock()

	mt := st.getOrSetModuleTrace(module)
	mt.Accesses = append(mt.Accesses, Access{
		Op:            IteratorNext,
		DurationNanos: duration.Nanoseconds(),
	})

	idx, ok := mt.iteratorIndexBy[iteratorID]
	if !ok {
		return
	}
	it := mt.Iterators[idx]
	it.NextCount++
	it.DurationNanos += duration.Nanoseconds()
}

func (st *StoreTracer) getOrSetModuleTrace(module string) (mt *ModuleTrace) {
	if _, ok := st.Modules[module]; !ok {
		mt = &ModuleTrace{
			Accesses:        []Access{},
			Iterators:       []*IteratorTrace{},
			LowLevelReads:   []seidbtypes.ReadTraceEvent{},
			iteratorIndexBy: map[int]int{},
		}
		st.Modules[module] = mt
	} else {
		mt = st.Modules[module]
	}
	return
}

func (st *StoreTracer) recordAccess(module string, access Access) {
	st.mu.Lock()
	defer st.mu.Unlock()
	mt := st.getOrSetModuleTrace(module)
	mt.Accesses = append(mt.Accesses, access)
}

// Clear resets the tracer to its empty state so a single StoreTracer can be
// reused across successive trace requests on the same connection.
func (st *StoreTracer) Clear() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.Modules = map[string]*ModuleTrace{}
	st.nextIteratorID = 0
}

type OperationSummary struct {
	Count      int   `json:"count"`
	TotalNanos int64 `json:"totalNanos"`
}

type StoreTraceDump struct {
	Modules map[string]ModuleTraceDump  `json:"modules"`
	Stats   map[string]OperationSummary `json:"stats,omitempty"`
}

type ModuleTraceDump struct {
	Reads           map[string]string           `json:"reads"`
	Has             []string                    `json:"has"`
	Stats           map[string]OperationSummary `json:"stats,omitempty"`
	Iterators       []IteratorTraceDump         `json:"iterators,omitempty"`
	LowLevelStats   map[string]OperationSummary `json:"lowLevelStats,omitempty"`
	LowLevelSamples []ReadTraceEventDump        `json:"lowLevelSamples,omitempty"`
}

type IteratorTraceDump struct {
	Start      string   `json:"start,omitempty"`
	End        string   `json:"end,omitempty"`
	Ascending  bool     `json:"ascending"`
	Keys       []string `json:"keys,omitempty"`
	NextCount  int      `json:"nextCount"`
	TotalNanos int64    `json:"totalNanos"`
	Truncated  bool     `json:"truncated,omitempty"`
}

type ReadTraceEventDump struct {
	Layer      string `json:"layer"`
	Operation  string `json:"operation"`
	TotalNanos int64  `json:"totalNanos"`
	Key        string `json:"key,omitempty"`
	Start      string `json:"start,omitempty"`
	End        string `json:"end,omitempty"`
	Reverse    bool   `json:"reverse,omitempty"`
}

// Dump materializes the tracer's accumulated per-module accesses into a
// wire-shaped StoreTraceDump. Reads that were later overwritten by a Set or
// Delete during the same tx are excluded so the Reads map reflects the
// pre-state the tx observed.
func (st *StoreTracer) Dump() StoreTraceDump {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.dumpLocked()
}

func (st *StoreTracer) dumpLocked() StoreTraceDump {
	d := StoreTraceDump{
		Modules: make(map[string]ModuleTraceDump, len(st.Modules)),
		Stats:   map[string]OperationSummary{},
	}
	for name, module := range st.Modules {
		mtd := ModuleTraceDump{
			Reads:           make(map[string]string),
			Has:             []string{},
			Stats:           map[string]OperationSummary{},
			Iterators:       make([]IteratorTraceDump, 0, len(module.Iterators)),
			LowLevelStats:   map[string]OperationSummary{},
			LowLevelSamples: make([]ReadTraceEventDump, 0, len(module.LowLevelReads)),
		}
		// any read for key XYZ after a Set/Delete to XYZ is discarded
		// because the result doesn't represent prestate.
		writtenKey := map[string]struct{}{}
		hasMap := map[string]struct{}{}
		for _, a := range module.Accesses {
			updateSummary(d.Stats, a.Op, a.DurationNanos)
			updateSummary(mtd.Stats, a.Op, a.DurationNanos)
			switch a.Op {
			case Get, IteratorValue:
				if _, ok := writtenKey[string(a.Key)]; ok {
					continue
				}
				mtd.Reads[hex.EncodeToString(a.Key)] = hex.EncodeToString(a.Value)
			case Has:
				if _, ok := writtenKey[string(a.Key)]; ok {
					continue
				}
				hasMap[hex.EncodeToString(a.Key)] = struct{}{}
			case Set, Delete:
				writtenKey[string(a.Key)] = struct{}{}
			}
		}
		for k := range hasMap {
			mtd.Has = append(mtd.Has, k)
		}
		for _, it := range module.Iterators {
			keys := make([]string, 0, len(it.Keys))
			for _, key := range it.Keys {
				keys = append(keys, hex.EncodeToString(key))
			}
			mtd.Iterators = append(mtd.Iterators, IteratorTraceDump{
				Start:      hex.EncodeToString(it.Start),
				End:        hex.EncodeToString(it.End),
				Ascending:  it.Ascending,
				Keys:       keys,
				NextCount:  it.NextCount,
				TotalNanos: it.DurationNanos,
				Truncated:  it.Truncated,
			})
		}
		for _, event := range module.LowLevelReads {
			key := event.Layer + "." + event.Operation
			summary := mtd.LowLevelStats[key]
			summary.Count++
			summary.TotalNanos += event.DurationNanos
			mtd.LowLevelStats[key] = summary
			mtd.LowLevelSamples = append(mtd.LowLevelSamples, ReadTraceEventDump{
				Layer:      event.Layer,
				Operation:  event.Operation,
				TotalNanos: event.DurationNanos,
				Key:        hex.EncodeToString(event.Key),
				Start:      hex.EncodeToString(event.Start),
				End:        hex.EncodeToString(event.End),
				Reverse:    event.Reverse,
			})
		}
		d.Modules[name] = mtd
	}
	return d
}

func updateSummary(stats map[string]OperationSummary, op OpType, durationNanos int64) {
	key := op.String()
	summary := stats[key]
	summary.Count++
	summary.TotalNanos += durationNanos
	stats[key] = summary
}

// DerivePrestateToJson returns a JSON encoding of the current trace state,
// used by debug_traceTransaction to attach AppState to the response.
func (st *StoreTracer) DerivePrestateToJson() []byte {
	st.mu.Lock()
	defer st.mu.Unlock()
	d := st.dumpLocked()
	bz, err := json.Marshal(&d)
	if err != nil {
		panic(err)
	}
	return bz
}

// RecordReadTrace implements seidbtypes.ReadTraceCollector so a state store
// backend can attach this tracer via WithReadTraceCollector and emit
// low-level read events (e.g. pebble Gets and iterator seeks) during a trace.
// Samples are capped at maxLowLevelReadSamples per module.
func (st *StoreTracer) RecordReadTrace(event seidbtypes.ReadTraceEvent) {
	st.mu.Lock()
	defer st.mu.Unlock()

	mt := st.getOrSetModuleTrace(event.StoreKey)
	if len(mt.LowLevelReads) >= maxLowLevelReadSamples {
		return
	}
	mt.LowLevelReads = append(mt.LowLevelReads, seidbtypes.ReadTraceEvent{
		StoreKey:      event.StoreKey,
		Layer:         event.Layer,
		Operation:     event.Operation,
		DurationNanos: event.DurationNanos,
		Key:           slices.Clone(event.Key),
		Start:         slices.Clone(event.Start),
		End:           slices.Clone(event.End),
		Reverse:       event.Reverse,
	})
}
