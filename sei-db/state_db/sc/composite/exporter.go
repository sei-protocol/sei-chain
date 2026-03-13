package composite

import (
	"errors"

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
// FlatKV data is exported as a separate "evm_flatkv" module appended after all
// cosmos modules complete. This keeps the two backends fully independent in the
// snapshot stream.
type SnapshotExporter struct {
	cosmosExporter types.Exporter
	evmExporter    types.Exporter
	phase          exportPhase
}

func NewExporter(cosmosExporter types.Exporter, evmExporter types.Exporter) *SnapshotExporter {
	return &SnapshotExporter{
		cosmosExporter: cosmosExporter,
		evmExporter:    evmExporter,
		phase:          phaseCosmos,
	}
}

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
