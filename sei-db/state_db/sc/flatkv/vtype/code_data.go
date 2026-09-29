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

// CodeData is a contract bytecode row in the FlatKV code database. The zero value holds no bytecode.
//
// The bytecode is shared rather than copied when a CodeData is copied, and must not be mutated.
type CodeData struct {
	// The block height at which this code was last modified.
	blockHeight uint64

	// The contract bytecode.
	bytecode []byte
}

// NewCodeData returns a new CodeData with every field zero.
func NewCodeData() *CodeData {
	return &CodeData{}
}

// AppendCodeData appends the serialized form of code to dst and returns the extended slice.
func AppendCodeData(dst []byte, code CodeData) []byte {
	dst = append(dst, byte(CodeDataVersion0))
	dst = binary.BigEndian.AppendUint64(dst, code.blockHeight)
	return append(dst, code.bytecode...)
}

// Serialize returns the serialized form of the code in a new slice.
func (c CodeData) Serialize() []byte {
	return AppendCodeData(make([]byte, 0, codeBytecodeStart+len(c.bytecode)), c)
}

// DeserializeCodeData parses contract code from its serialized form. The returned bytecode aliases data.
func DeserializeCodeData(data []byte) (CodeData, error) {
	if len(data) == 0 {
		return CodeData{}, errors.New("data is empty")
	}

	version := CodeDataVersion(data[codeVersionStart])
	if version != CodeDataVersion0 {
		return CodeData{}, fmt.Errorf("unsupported serialization version: %d", version)
	}
	if len(data) < codeBytecodeStart {
		return CodeData{}, fmt.Errorf("data length at version %d should be at least %d, got %d",
			version, codeBytecodeStart, len(data))
	}

	return CodeData{
		blockHeight: binary.BigEndian.Uint64(data[codeBlockHeightStart:codeBytecodeStart]),
		bytecode:    data[codeBytecodeStart:],
	}, nil
}

// GetBlockHeight returns the block height at which the code was last modified.
func (c CodeData) GetBlockHeight() uint64 {
	return c.blockHeight
}

// GetBytecode returns the contract bytecode, which must not be mutated.
func (c CodeData) GetBytecode() []byte {
	return c.bytecode
}

// IsDelete reports whether the bytecode is empty. The store deletes code that is set to empty.
func (c CodeData) IsDelete() bool {
	return len(c.bytecode) == 0
}

// SetBlockHeight sets the block height at which the code was last modified. Returns the receiver.
func (c *CodeData) SetBlockHeight(blockHeight uint64) *CodeData {
	c.blockHeight = blockHeight
	return c
}

// SetBytecode sets the contract bytecode to a copy of bytecode. Returns the receiver.
func (c *CodeData) SetBytecode(bytecode []byte) *CodeData {
	c.bytecode = append([]byte(nil), bytecode...)
	return c
}
