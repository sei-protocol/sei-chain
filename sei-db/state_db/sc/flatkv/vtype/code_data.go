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
	codeBlockHeightStart = 1
	codeBytecodeStart    = 9
)

// Used for encapsulating and serializing contract bytecode in the FlatKV code database.
//
// This data structure is not threadsafe. Values passed into and values received from this data structure
// are not safe to modify without first copying them.
type CodeData struct {
	data []byte
}

// Create a new CodeData with the given bytecode.
func NewCodeData() *CodeData {
	data := make([]byte, codeBytecodeStart)
	data[codeVersionStart] = byte(CodeDataVersion0)
	return &CodeData{data: data}
}

// Serialize the code data to a byte slice.
//
// The returned byte slice is not safe to modify without first copying it.
func (c *CodeData) Serialize() []byte {
	return c.data
}

// Deserialize the code data from the given byte slice.
func DeserializeCodeData(data []byte) (*CodeData, error) {
	if len(data) == 0 {
		return nil, errors.New("data is empty")
	}

	codeData := &CodeData{
		data: data,
	}

	serializationVersion := codeData.GetSerializationVersion()
	if serializationVersion != CodeDataVersion0 {
		return nil, fmt.Errorf("unsupported serialization version: %d", serializationVersion)
	}

	if len(data) < codeBytecodeStart {
		return nil, fmt.Errorf("data length at version %d should be at least %d, got %d",
			serializationVersion, codeBytecodeStart, len(data))
	}

	return codeData, nil
}

// Get the serialization version for this CodeData instance.
func (c *CodeData) GetSerializationVersion() CodeDataVersion {
	return (CodeDataVersion)(c.data[codeVersionStart])
}

// Get the block height when this code was last modified.
func (c *CodeData) GetBlockHeight() int64 {
	return int64(binary.BigEndian.Uint64(c.data[codeBlockHeightStart:codeBytecodeStart])) //nolint:gosec // block height is always within int64 range
}

// Get the contract bytecode.
func (c *CodeData) GetBytecode() []byte {
	return c.data[codeBytecodeStart:]
}

// Set the contract bytecode.
func (c *CodeData) SetBytecode(bytecode []byte) *CodeData {
	newData := make([]byte, codeBytecodeStart+len(bytecode))
	copy(newData, c.data[:codeBytecodeStart])
	copy(newData[codeBytecodeStart:], bytecode)
	c.data = newData
	return c
}

// Check if this code data signifies a deletion operation. A deletion operation is automatically
// performed when the bytecode is empty (with the exception of the serialization version and block height).
func (c *CodeData) IsDelete() bool {
	return len(c.data) == codeBytecodeStart
}

// Set the block height when this code was last modified/touched. Returns self.
func (c *CodeData) SetBlockHeight(blockHeight int64) *CodeData {
	binary.BigEndian.PutUint64(c.data[codeBlockHeightStart:codeBytecodeStart], uint64(blockHeight)) //nolint:gosec // block height is always non-negative
	return c
}
