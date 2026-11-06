package littblock

import (
	"encoding/binary"
	"fmt"

	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"
)

// ledgerKey contains a record or alias kind and its encoded payload.
type ledgerKey struct {
	kind    recordKind
	payload []byte
}

// blockKey identifies a SuperBlock record by its road index.
type blockKey struct{ Index blocktypes.BlockNumber }

func (k blockKey) encode() ledgerKey {
	// Big-endian encoding preserves numeric road order in lexicographic comparisons.
	return ledgerKey{kind: kindBlock, payload: binary.BigEndian.AppendUint64(nil, uint64(k.Index))}
}

func (k ledgerKey) decodeBlock() (blockKey, error) {
	payload, err := k.checkedPayload(kindBlock, 8)
	if err != nil {
		return blockKey{}, err
	}
	return blockKey{Index: blocktypes.BlockNumber(binary.BigEndian.Uint64(payload))}, nil
}

// qcKey identifies a QC alias by its road index.
type qcKey struct{ Index blocktypes.BlockNumber }

func (k qcKey) encode() ledgerKey {
	// Big-endian encoding preserves numeric road order in lexicographic comparisons.
	return ledgerKey{kind: kindQC, payload: binary.BigEndian.AppendUint64(nil, uint64(k.Index))}
}

func (k ledgerKey) decodeQC() (qcKey, error) {
	payload, err := k.checkedPayload(kindQC, 8)
	if err != nil {
		return qcKey{}, err
	}
	return qcKey{Index: blocktypes.BlockNumber(binary.BigEndian.Uint64(payload))}, nil
}

// appQCKey identifies an AppQC record by its road index.
type appQCKey struct{ Index blocktypes.BlockNumber }

func (k appQCKey) encode() ledgerKey {
	// Big-endian encoding preserves numeric road order in lexicographic comparisons.
	return ledgerKey{kind: kindAppQC, payload: binary.BigEndian.AppendUint64(nil, uint64(k.Index))}
}

func (k ledgerKey) decodeAppQC() (appQCKey, error) {
	payload, err := k.checkedPayload(kindAppQC, 8)
	if err != nil {
		return appQCKey{}, err
	}
	return appQCKey{Index: blocktypes.BlockNumber(binary.BigEndian.Uint64(payload))}, nil
}

// appProposalKey identifies an AppProposal record by its road index.
type appProposalKey struct{ Index blocktypes.BlockNumber }

func (k appProposalKey) encode() ledgerKey {
	// Big-endian encoding preserves numeric road order in lexicographic comparisons.
	return ledgerKey{kind: kindAppProp, payload: binary.BigEndian.AppendUint64(nil, uint64(k.Index))}
}

func (k ledgerKey) decodeAppProposal() (appProposalKey, error) {
	payload, err := k.checkedPayload(kindAppProp, 8)
	if err != nil {
		return appProposalKey{}, err
	}
	return appProposalKey{Index: blocktypes.BlockNumber(binary.BigEndian.Uint64(payload))}, nil
}

// laneBlockKey identifies a lane block by road index and header offset.
type laneBlockKey struct {
	Index  blocktypes.BlockNumber
	Offset uint64
}

func (k laneBlockKey) encode() ledgerKey {
	payload := binary.BigEndian.AppendUint64(nil, uint64(k.Index))
	return ledgerKey{kind: kindLaneBlock, payload: binary.BigEndian.AppendUint64(payload, k.Offset)}
}

func (k ledgerKey) decodeLaneBlock() (laneBlockKey, error) {
	payload, err := k.checkedPayload(kindLaneBlock, 16)
	if err != nil {
		return laneBlockKey{}, err
	}
	return laneBlockKey{Index: blocktypes.BlockNumber(binary.BigEndian.Uint64(payload)), Offset: binary.BigEndian.Uint64(payload[8:])}, nil
}

// blockHashKey identifies a SuperBlock by its hash.
type blockHashKey struct{ Hash blocktypes.BlockHash }

func (k blockHashKey) encode() ledgerKey {
	return ledgerKey{kind: kindBlockHash, payload: k.Hash[:]}
}

func (k ledgerKey) decodeBlockHash() (blockHashKey, error) {
	var hash blocktypes.BlockHash
	payload, err := k.checkedPayload(kindBlockHash, len(hash))
	if err != nil {
		return blockHashKey{}, err
	}
	return blockHashKey{Hash: blocktypes.BlockHash(payload)}, nil
}

// checkedPayload returns the payload of a key with the expected kind and size.
func (k ledgerKey) checkedPayload(expected recordKind, size int) ([]byte, error) {
	if k.kind != expected || len(k.payload) != size {
		return nil, fmt.Errorf("invalid key for record kind %d: kind %d, payload %x", expected, k.kind, k.payload)
	}
	return k.payload, nil
}

// bytes returns the key's varint kind followed by its payload.
func (k ledgerKey) bytes() []byte {
	return append(binary.AppendUvarint(nil, uint64(k.kind)), k.payload...)
}

// decodeLedgerKey parses a key's varint kind and payload.
func decodeLedgerKey(value []byte) (ledgerKey, error) {
	kind, n := binary.Uvarint(value)
	if n <= 0 {
		return ledgerKey{}, fmt.Errorf("invalid record kind uvarint in key %x", value)
	}
	return ledgerKey{kind: recordKind(kind), payload: value[n:]}, nil
}
