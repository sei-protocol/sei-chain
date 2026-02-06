package flatkv

import (
	"errors"
	"fmt"
)

// Close closes all database instances.
func (s *CommitStore) Close() error {
	var errs []error

	if s.changelog != nil {
		if err := s.changelog.Close(); err != nil {
			errs = append(errs, fmt.Errorf("changelog close: %w", err))
		}
	}

	if s.metadataDB != nil {
		if err := s.metadataDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("metadataDB close: %w", err))
		}
	}

	if s.storageDB != nil {
		if err := s.storageDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("storageDB close: %w", err))
		}
	}
	if s.codeDB != nil {
		if err := s.codeDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("codeDB close: %w", err))
		}
	}
	if s.accountDB != nil {
		if err := s.accountDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("accountDB close: %w", err))
		}
	}

	if s.legacyDB != nil {
		if err := s.legacyDB.Close(); err != nil {
			errs = append(errs, fmt.Errorf("legacyDB close: %w", err))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	s.log.Info("FlatKV store closed")
	return nil
}

// Exporter creates an exporter for the given version.
// NOTE: Not yet implemented. Will be added with state-sync support.
// The future implementation will export each DB separately with internal key format.
func (s *CommitStore) Exporter(version int64) (Exporter, error) {
	// Return a placeholder exporter that indicates not implemented
	return &notImplementedExporter{}, nil
}

// WriteSnapshot writes a complete snapshot to the given directory.
func (s *CommitStore) WriteSnapshot(dir string) error {
	// TODO: Implement snapshot writing
	return fmt.Errorf("WriteSnapshot not implemented")
}

// Rollback restores state to targetVersion.
func (s *CommitStore) Rollback(targetVersion int64) error {
	s.log.Info("FlatKV Rollback called (no-op)", "targetVersion", targetVersion)
	return nil
}
