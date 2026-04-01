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

var _ VType = (*CodeData)(nil)

// Used for encapsulating and serializing contract bytecode in the FlatKV code database.
//
// This data structure is not threadsafe. Values passed into and values received from this data structure
// are not safe to modify without first copying them.
type CodeData struct {
	version     CodeDataVersion
	blockHeight int64
	bytecode    []byte
}

// Create a new CodeData with the given bytecode.
func NewCodeData() *CodeData {
	return &CodeData{version: CodeDataVersion0}
}

// Serialize the code data to a byte slice.
func (c *CodeData) Serialize() []byte {
	if c == nil {
		return make([]byte, codeBytecodeStart)
	}
	data := make([]byte, codeBytecodeStart+len(c.bytecode))
	data[codeVersionStart] = byte(c.version)
	binary.BigEndian.PutUint64(data[codeBlockHeightStart:codeBytecodeStart], uint64(c.blockHeight)) //nolint:gosec
	copy(data[codeBytecodeStart:], c.bytecode)
	return data
}

// Deserialize the code data from the given byte slice.
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

	bytecode := make([]byte, len(data)-codeBytecodeStart)
	copy(bytecode, data[codeBytecodeStart:])

	return &CodeData{
		version:     version,
		blockHeight: int64(binary.BigEndian.Uint64(data[codeBlockHeightStart:codeBytecodeStart])), //nolint:gosec
		bytecode:    bytecode,
	}, nil
}

// Get the serialization version for this CodeData instance.
func (c *CodeData) GetSerializationVersion() CodeDataVersion {
	if c == nil {
		return CodeDataVersion0
	}
	return c.version
}

// Get the block height when this code was last modified.
func (c *CodeData) GetBlockHeight() int64 {
	if c == nil {
		return 0
	}
	return c.blockHeight
}

// Get the contract bytecode.
func (c *CodeData) GetBytecode() []byte {
	if c == nil {
		return []byte{}
	}
	return c.bytecode
}

// Set the contract bytecode. Returns self (or a new CodeData if nil).
func (c *CodeData) SetBytecode(bytecode []byte) *CodeData {
	if c == nil {
		c = NewCodeData()
	}
	c.bytecode = append([]byte(nil), bytecode...)
	return c
}

// Check if this code data signifies a deletion operation. A deletion operation is automatically
// performed when the bytecode is empty (with the exception of the serialization version and block height).
func (c *CodeData) IsDelete() bool {
	if c == nil {
		return true
	}
	return len(c.bytecode) == 0
}

// Set the block height when this code was last modified/touched. Returns self (or a new CodeData if nil).
func (c *CodeData) SetBlockHeight(blockHeight int64) *CodeData {
	if c == nil {
		c = NewCodeData()
	}
	c.blockHeight = blockHeight
	return c
}
