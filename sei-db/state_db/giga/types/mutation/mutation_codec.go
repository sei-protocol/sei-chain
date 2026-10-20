package mutation

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// MutationVersion is the serialization version of a Mutation.
type MutationVersion uint8

// DO NOT CHANGE VERSION VALUES!!! Adding new versions is ok, but historical versions should never be removed/changed.
const MutationVersion0 MutationVersion = 0

/*
Serialization schema for Mutation version 0:

| Version | Key Length | Key      | Previous Value Unknown | Previous Value Present | Previous Value Length |
|---------|------------|----------|------------------------|------------------------|-----------------------|
| 1 byte  | 4 bytes    | variable | 1 byte                 | 1 byte                 | 4 bytes               |

| Previous Value | New Value Present | New Value Length | New Value |
|----------------|-------------------|------------------|-----------|
| variable       | 1 byte            | 4 bytes          | variable  |

Lengths are big-endian. Boolean fields are 0 (false) or 1 (true). A value that is not present has a length of 0.

A slice of mutations is serialized as the serialized mutations back to back, with no header.
*/

const (
	mutationVersionLength = 1
	mutationBoolLength    = 1
	mutationLengthLength  = 4
)

// Serialize returns the version 0 encoding of the mutation.
func (m *Mutation) Serialize() []byte {
	return appendMutation(make([]byte, 0, m.serializedSize()), m)
}

// DeserializeMutation decodes one mutation from the front of data, returning it and the number of bytes consumed.
// Bytes after the mutation are ignored. The returned mutation does not alias data.
func DeserializeMutation(data []byte) (*Mutation, int, error) {
	reader := mutationReader{data: data}

	version, err := reader.readByte()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read version: %w", err)
	}
	if MutationVersion(version) != MutationVersion0 {
		return nil, 0, fmt.Errorf("unsupported serialization version: %d", version)
	}

	key, err := reader.readBytes()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read key: %w", err)
	}
	previousValueUnknown, err := reader.readBool()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read previous value unknown flag: %w", err)
	}
	previousValue, err := reader.readOptionalBytes()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read previous value: %w", err)
	}
	newValue, err := reader.readOptionalBytes()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read new value: %w", err)
	}

	mutation := &Mutation{
		Key:                  key,
		PreviousValueUnknown: previousValueUnknown,
		PreviousValue:        previousValue,
		NewValue:             newValue,
	}
	return mutation, len(data) - len(reader.data), nil
}

// SerializeMutations returns the encoding of mutations, in order.
func SerializeMutations(mutations []Mutation) []byte {
	size := 0
	for i := range mutations {
		size += mutations[i].serializedSize()
	}
	data := make([]byte, 0, size)
	for i := range mutations {
		data = appendMutation(data, &mutations[i])
	}
	return data
}

// DeserializeMutations decodes a byte slice produced by SerializeMutations(). It returns an error if data does not
// consist entirely of whole mutations. The returned mutations do not alias data.
func DeserializeMutations(data []byte) ([]Mutation, error) {
	mutations := make([]Mutation, 0)
	for offset := 0; offset < len(data); {
		mutation, consumed, err := DeserializeMutation(data[offset:])
		if err != nil {
			return nil, fmt.Errorf(
				"failed to deserialize mutation %d at offset %d: %w", len(mutations), offset, err)
		}
		mutations = append(mutations, *mutation)
		offset += consumed
	}
	return mutations, nil
}

// serializedSize returns the length of the mutation's encoding.
func (m *Mutation) serializedSize() int {
	return mutationVersionLength +
		mutationLengthLength + len(m.Key) +
		2*mutationBoolLength + mutationLengthLength + len(m.PreviousValue) +
		mutationBoolLength + mutationLengthLength + len(m.NewValue)
}

// appendMutation appends the version 0 encoding of m to data.
func appendMutation(data []byte, m *Mutation) []byte {
	data = append(data, byte(MutationVersion0))
	data = appendMutationBytes(data, m.Key)
	data = appendMutationBool(data, m.PreviousValueUnknown)
	data = appendMutationBool(data, m.PreviousValue != nil)
	data = appendMutationBytes(data, m.PreviousValue)
	data = appendMutationBool(data, m.NewValue != nil)
	data = appendMutationBytes(data, m.NewValue)
	return data
}

// appendMutationBool appends value as a single 0 or 1 byte.
func appendMutationBool(data []byte, value bool) []byte {
	if value {
		return append(data, 1)
	}
	return append(data, 0)
}

// appendMutationBytes appends value prefixed by its 4-byte big-endian length.
func appendMutationBytes(data []byte, value []byte) []byte {
	//nolint:gosec // lengths fit in 4 bytes by contract
	data = binary.BigEndian.AppendUint32(data, uint32(len(value)))
	return append(data, value...)
}

// mutationReader consumes the fields of a serialized Mutation from the front of a byte slice.
type mutationReader struct {
	// The bytes not yet consumed.
	data []byte
}

// readByte consumes a single byte.
func (r *mutationReader) readByte() (byte, error) {
	if len(r.data) < 1 {
		return 0, fmt.Errorf("need 1 byte, have 0")
	}
	value := r.data[0]
	r.data = r.data[1:]
	return value, nil
}

// readBool consumes a single byte, which must be 0 or 1.
func (r *mutationReader) readBool() (bool, error) {
	value, err := r.readByte()
	if err != nil {
		return false, fmt.Errorf("failed to read bool: %w", err)
	}
	switch value {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, fmt.Errorf("invalid bool byte: %d", value)
	}
}

// readBytes consumes a 4-byte big-endian length and that many bytes, returning a copy of the bytes.
func (r *mutationReader) readBytes() ([]byte, error) {
	if len(r.data) < mutationLengthLength {
		return nil, fmt.Errorf("need %d length bytes, have %d", mutationLengthLength, len(r.data))
	}
	length := uint64(binary.BigEndian.Uint32(r.data))
	r.data = r.data[mutationLengthLength:]
	if length > uint64(len(r.data)) {
		return nil, fmt.Errorf("need %d bytes, have %d", length, len(r.data))
	}
	value := bytes.Clone(r.data[:length])
	r.data = r.data[length:]
	return value, nil
}

// readOptionalBytes consumes a presence flag followed by a length-prefixed value. It returns nil when the value is
// not present.
func (r *mutationReader) readOptionalBytes() ([]byte, error) {
	present, err := r.readBool()
	if err != nil {
		return nil, fmt.Errorf("failed to read presence flag: %w", err)
	}
	value, err := r.readBytes()
	if err != nil {
		return nil, fmt.Errorf("failed to read value: %w", err)
	}
	if !present {
		if len(value) != 0 {
			return nil, fmt.Errorf("value is not present but has length %d", len(value))
		}
		return nil, nil
	}
	return value, nil
}
