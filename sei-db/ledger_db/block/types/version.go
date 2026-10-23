package types

import "fmt"

// RecordVersion identifies the encoding of a stored record.
type RecordVersion uint8

// CurrentRecordVersion is the shared version of the current record encoding.
const CurrentRecordVersion RecordVersion = 1

// Validate checks whether the record version is supported.
func (version RecordVersion) Validate() error {
	if version != CurrentRecordVersion {
		return fmt.Errorf("unsupported record serialization version %d", version)
	}
	return nil
}
