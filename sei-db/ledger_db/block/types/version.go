package types

import "fmt"

// RecordVersion identifies the encoding of a stored record.
type RecordVersion uint8

// CurrentRecordVersion is the shared version of the current record encoding.
const CurrentRecordVersion RecordVersion = 1

// ConsumeVersion validates and removes the shared block-storage version prefix.
func ConsumeVersion(value []byte) ([]byte, error) {
	if len(value) == 0 {
		return nil, fmt.Errorf("record serialization version is missing")
	}
	if RecordVersion(value[0]) != CurrentRecordVersion {
		return nil, fmt.Errorf("unsupported record serialization version %d", value[0])
	}
	return value[1:], nil
}
