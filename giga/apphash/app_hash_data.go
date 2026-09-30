package apphash

import "crypto/sha256"

// appHashVersion is the version of the app hash schema produced by this package.
const appHashVersion uint8 = 1

// Contains the data used to produce an app hash.
type AppHashData struct {

	// The version of the app hash schema.
	version uint8

	// The block height corresponding to this
	// app hash.
	blockHeight uint64

	// The hash of the block header.
	blockHeaderHash [32]byte

	// The hash of the state lattice hash.
	stateHash [32]byte

	// The "Block Update Digest", aka the hash of
	// the key-value pairs that changed as a result
	// of executing this block.
	bud [32]byte

	// The hash of the transaction receipts
	// produced by executing this block.
	receiptHash [32]byte

	// The app hash of block (blockHeight-1).
	// All 0s if this is the first block.
	previousAppHash [32]byte

	// The app hash: the SHA-256 of the fields above in the canonical byte format. It is not itself serialized.
	hash [32]byte
}

// Construct a new AppHashData object at the current schema version.
func NewAppHashData(
	// The block height corresponding to this app hash.
	blockHeight uint64,
	// The hash of the block header.
	blockHeaderHash [32]byte,
	// The hash of the state lattice hash.
	stateHash [32]byte,
	// The Block Update Digest.
	bud [32]byte,
	// The hash of the transaction receipts produced by executing this block.
	receiptHash [32]byte,
	// The app hash of block (blockHeight-1), or all 0s if this is the first block.
	previousAppHash [32]byte,
) *AppHashData {
	ahd := &AppHashData{
		version:         appHashVersion,
		blockHeight:     blockHeight,
		blockHeaderHash: blockHeaderHash,
		stateHash:       stateHash,
		bud:             bud,
		receiptHash:     receiptHash,
		previousAppHash: previousAppHash,
	}
	ahd.hash = sha256.Sum256(ahd.Serialize())
	return ahd
}

// Version returns the version of the app hash schema.
func (ahd *AppHashData) Version() uint8 {
	return ahd.version
}

// BlockHeight returns the block height corresponding to this app hash.
func (ahd *AppHashData) BlockHeight() uint64 {
	return ahd.blockHeight
}

// BlockHeaderHash returns the hash of the block header.
func (ahd *AppHashData) BlockHeaderHash() [32]byte {
	return ahd.blockHeaderHash
}

// StateHash returns the hash of the state lattice hash.
func (ahd *AppHashData) StateHash() [32]byte {
	return ahd.stateHash
}

// BUD returns the Block Update Digest.
func (ahd *AppHashData) BUD() [32]byte {
	return ahd.bud
}

// ReceiptHash returns the hash of the transaction receipts produced by executing this block.
func (ahd *AppHashData) ReceiptHash() [32]byte {
	return ahd.receiptHash
}

// PreviousAppHash returns the app hash of block (blockHeight-1), or all 0s if this is the first block.
func (ahd *AppHashData) PreviousAppHash() [32]byte {
	return ahd.previousAppHash
}

// Hash returns the app hash: the SHA-256 of the app hash data in its canonical byte format.
func (ahd *AppHashData) Hash() [32]byte {
	return ahd.hash
}
