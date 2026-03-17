package composite

import (
	"errors"
	"fmt"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

var _ types.Exporter = (*SnapshotExporter)(nil)

type exportPhase int

const (
	phaseCosmos exportPhase = iota
	phaseFlatKV
	phaseDone
)

// SnapshotExporter coordinates export from cosmos (memiavl) and flatKV backends.
//
// Next() returns items in stream order. Each item is either:
//   - string: a module name header that starts a new module section
//   - *types.SnapshotNode: a leaf key/value belonging to the current module
//
// FlatKV data is exported as a separate "evm_flatkv" module appended after all
// cosmos modules complete. This keeps the two backends fully independent in the
// snapshot stream.
type SnapshotExporter struct {
	cosmosExporter types.Exporter
	evmExporter    types.Exporter
	phase          exportPhase
}

// NewExporter creates a composite exporter. cosmosExporter must not be nil.
// evmExporter may be nil when FlatKV is not active.
func NewExporter(cosmosExporter types.Exporter, evmExporter types.Exporter) (*SnapshotExporter, error) {
	if cosmosExporter == nil {
		return nil, fmt.Errorf("cosmosExporter must not be nil")
	}
	return &SnapshotExporter{
		cosmosExporter: cosmosExporter,
		evmExporter:    evmExporter,
		phase:          phaseCosmos,
	}, nil
}

// Next returns the next item in the composite snapshot stream.
//
// The stream is split into two sequential phases:
//  1. phaseCosmos — drains all items from the cosmos (memiavl) exporter.
//     When the cosmos exporter is exhausted, if a FlatKV exporter is present,
//     the phase transitions to phaseFlatKV and emits the EVMFlatKVStoreName
//     module header as the first item.
//  2. phaseFlatKV — drains all items from the FlatKV exporter.
//
// Returns ErrorExportDone when both phases are complete.
func (s *SnapshotExporter) Next() (interface{}, error) {
	switch s.phase {
	case phaseCosmos:
		return s.nextFromCosmos()
	case phaseFlatKV:
		return s.nextFromFlatKV()
	default:
		return nil, errorutils.ErrorExportDone
	}
}

// nextFromCosmos pulls items from the cosmos exporter. On exhaustion it
// transitions to phaseFlatKV (emitting the module header) or phaseDone.
func (s *SnapshotExporter) nextFromCosmos() (interface{}, error) {
	item, err := s.cosmosExporter.Next()
	if err != nil {
		if !errors.Is(err, errorutils.ErrorExportDone) {
			return nil, err
		}

		// Cosmos done. Append flatKV as a separate module.
		if s.evmExporter != nil {
			s.phase = phaseFlatKV
			return EVMFlatKVStoreName, nil
		}

		s.phase = phaseDone
		return nil, errorutils.ErrorExportDone
	}
	return item, nil
}

// nextFromFlatKV pulls items from the FlatKV exporter. On exhaustion it
// transitions to phaseDone.
func (s *SnapshotExporter) nextFromFlatKV() (interface{}, error) {
	item, err := s.evmExporter.Next()
	if err != nil {
		if !errors.Is(err, errorutils.ErrorExportDone) {
			return nil, err
		}
		s.phase = phaseDone
		return nil, errorutils.ErrorExportDone
	}
	return item, nil
}

func (s *SnapshotExporter) Close() error {
	var errCosmos, errEVM error
	if s.cosmosExporter != nil {
		errCosmos = s.cosmosExporter.Close()
	}
	if s.evmExporter != nil {
		errEVM = s.evmExporter.Close()
	}
	return errors.Join(errCosmos, errEVM)
}
