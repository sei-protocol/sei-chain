package vtype

import (
	"encoding/binary"
	"errors"
	"fmt"
)

type CodeDataVersion uint8

// DO NOT CHANGE VERSION VALUES!!! Adding new versions is ok, but historical versions should never be removed/changed.
const (
	CodeDataVersion0 CodeDataVersion = 0
)

/*
Serialization schema for CodeData version 0:

| Version | Block Height | Bytecode     |
|---------|--------------|--------------|
| 1 byte  | 8 bytes      | variable     |

Data is stored in big-endian order. Bytecode is variable length.
*/

const (
	codeVersionStart     = 0
	codeBlockHeightStart = codeVersionStart + VersionLength

	codeBytecodeStart = codeBlockHeightStart + BlockHeightLength
)

var _ VType = (*CodeData)(nil)

// Used for encapsulating and serializing contract bytecode in the FlatKV code database.
//
// This data structure is not threadsafe. Values passed into and values received from this data structure
// are not safe to modify without first copying them. The value is held in its serialized form.
type CodeData struct {
	data []byte
}

// Create a new CodeData with no bytecode.
func NewCodeData() *CodeData {
	return &CodeData{data: make([]byte, codeBytecodeStart)}
}

// NewCodeDataFrom returns the code data for bytecode written at blockHeight, built directly in its
// serialized form so the bytecode is copied once rather than once here and again at serialize time.
func NewCodeDataFrom(blockHeight int64, bytecode []byte) *CodeData {
	data := make([]byte, codeBytecodeStart+len(bytecode))
	data[codeVersionStart] = byte(CodeDataVersion0)
	heightBytes := data[codeBlockHeightStart:codeBytecodeStart]
	binary.BigEndian.PutUint64(heightBytes, uint64(blockHeight)) //nolint:gosec // height is non-negative
	copy(data[codeBytecodeStart:], bytecode)
	return &CodeData{data: data}
}

// Serialize the code data to a byte slice.
//
// The returned byte slice is not safe to modify without first copying it.
func (c *CodeData) Serialize() []byte {
	if c == nil {
		return make([]byte, codeBytecodeStart)
	}
	return c.data
}

// Deserialize the code data from the given byte slice.
//
// The returned CodeData owns its bytes; data may be reused or modified afterwards.
func DeserializeCodeData(data []byte) (*CodeData, error) {
	if len(data) == 0 {
		return nil, errors.New("data is empty")
	}

	version := CodeDataVersion(data[codeVersionStart])
	if version != CodeDataVersion0 {
		return nil, fmt.Errorf("unsupported serialization version: %d", version)
	}

	if len(data) < codeBytecodeStart {
		return nil, fmt.Errorf("data length at version %d should be at least %d, got %d",
			version, codeBytecodeStart, len(data))
	}

	// Copied rather than aliased: the caller's buffer is commonly borrowed from the storage engine
	// or an iterator, and GetBytecode hands out a subslice of whatever is held here.
	owned := make([]byte, len(data))
	copy(owned, data)
	return &CodeData{data: owned}, nil
}

// Get the serialization version for this CodeData instance.
func (c *CodeData) GetSerializationVersion() CodeDataVersion {
	if c == nil {
		return CodeDataVersion0
	}
	return CodeDataVersion(c.data[codeVersionStart])
}

// Get the block height when this code was last modified.
func (c *CodeData) GetBlockHeight() int64 {
	if c == nil {
		return 0
	}
	heightBytes := c.data[codeBlockHeightStart:codeBytecodeStart]
	return int64(binary.BigEndian.Uint64(heightBytes)) //nolint:gosec // height fits in int64
}

// Get the contract bytecode.
func (c *CodeData) GetBytecode() []byte {
	if c == nil {
		return []byte{}
	}
	return c.data[codeBytecodeStart:]
}

// Set the contract bytecode. Returns self (or a new CodeData if nil).
func (c *CodeData) SetBytecode(bytecode []byte) *CodeData {
	if c == nil {
		c = NewCodeData()
	}
	next := make([]byte, codeBytecodeStart+len(bytecode))
	copy(next, c.data[:codeBytecodeStart])
	copy(next[codeBytecodeStart:], bytecode)
	c.data = next
	return c
}

// Check if this code data signifies a deletion operation. A deletion operation is automatically
// performed when the bytecode is empty (with the exception of the serialization version and block height).
func (c *CodeData) IsDelete() bool {
	if c == nil {
		return true
	}
	return len(c.data) == codeBytecodeStart
}

// Set the block height when this code was last modified/touched. Returns self (or a new CodeData if nil).
func (c *CodeData) SetBlockHeight(blockHeight int64) *CodeData {
	if c == nil {
		c = NewCodeData()
	}
	heightBytes := c.data[codeBlockHeightStart:codeBytecodeStart]
	binary.BigEndian.PutUint64(heightBytes, uint64(blockHeight)) //nolint:gosec // height is non-negative
	return c
}
