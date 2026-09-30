package apphash

import "crypto/sha256"

// appHashVersion is the version of the app hash schema produced by this package.
const appHashVersion uint8 = 1

// appHashDomain is the domain separation tag that prefixes the canonical byte format in the app hash preimage.
const appHashDomain = "sei-apphash"

// Contains the data used to produce an app hash.
type AppHashData struct {

	// The version of the app hash schema.
	version uint8

	// The block height corresponding to this
	// app hash.
	blockHeight uint64

	// The hash of the block.
	blockHash [32]byte

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

	// The app hash: SHA-256 of appHashDomain followed by the canonical byte format. 
	// It is not itself serialized.
	appHash [32]byte
}

// Construct a new AppHashData object at the current schema version.
func NewAppHashData(
	// The block height corresponding to this app hash.
	blockHeight uint64,
	// The hash of the block.
	blockHash [32]byte,
	// The hash of the state lattice hash.
	stateHash [32]byte,
	// The "Block Update Digest", aka the hash of
	// the key-value pairs that changed as a result
	// of executing this block.
	bud [32]byte,
	// The hash of the transaction receipts produced by executing this block.
	receiptHash [32]byte,
	// The app hash of block (blockHeight-1), or all 0s if this is the first block.
	previousAppHash [32]byte,
) *AppHashData {
	ahd := &AppHashData{
		version:         appHashVersion,
		blockHeight:     blockHeight,
		blockHash:       blockHash,
		stateHash:       stateHash,
		bud:             bud,
		receiptHash:     receiptHash,
		previousAppHash: previousAppHash,
	}
	ahd.appHash = sha256.Sum256(append([]byte(appHashDomain), ahd.Serialize()...))
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

// BlockHash returns the hash of the block.
func (ahd *AppHashData) BlockHash() [32]byte {
	return ahd.blockHash
}

// StateHash returns the hash of the state lattice hash.
func (ahd *AppHashData) StateHash() [32]byte {
	return ahd.stateHash
}

// BUD returns the "Block Update Digest", aka the hash of the key-value pairs that changed as a result of executing
// this block.
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

// AppHash returns the app hash: the SHA-256 of appHashDomain followed by the canonical byte format.
func (ahd *AppHashData) AppHash() [32]byte {
	return ahd.appHash
}
