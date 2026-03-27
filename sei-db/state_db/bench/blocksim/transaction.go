package blocksim

import (
	"encoding/binary"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/common/rand"
)

// A simulated transaction for the blocksim benchmark.
type transaction struct {
	// The unique ID of the transaction. Used to determinstically generate the transaction hash.
	id uint64

	// The (simulated) hash of the transaction.
	hash []byte

	// Data contained by the transaction. These bytes are randomly generated.
	payload []byte
}

// Creates a randomized transaction with the given ID.
func RandomTransaction(
	id uint64,
	crand *rand.CannedRandom,
	size int,
) *transaction {

	hash := crand.Address(0, int64(id), 32)
	payload := crand.Bytes(size)

	return &transaction{
		id:      id,
		hash:    hash,
		payload: payload,
	}
}

// Returns the hash of the transaction.
//
// Data is not safe to modify in place, make a copy before modifying.
func (t *transaction) Hash() []byte {
	return t.hash
}

// Returns the payload of the transaction.
//
// Data is not safe to modify in place, make a copy before modifying.
func (t *transaction) Payload() []byte {
	return t.payload
}

// Returns the ID of the transaction.
func (t *transaction) ID() uint64 {
	return t.id
}

// Returns the serialized transaction.
func (t *transaction) Serialize() []byte {
	data := make([]byte, len(t.payload)+8 /* id */ +4 /* payload size */)
	binary.BigEndian.PutUint64(data[:8], t.id)
	binary.BigEndian.PutUint32(data[8:12], uint32(len(t.payload)))
	copy(data[12:], t.payload)
	return data
}

// Deserializes a transaction from the given data.
func DeserializeTransaction(crand *rand.CannedRandom, data []byte) (*transaction, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("data too short to contain a transaction")
	}

	id := binary.BigEndian.Uint64(data[:8])
	payloadSize := binary.BigEndian.Uint32(data[8:12])
	if len(data) < 12+int(payloadSize) {
		return nil, fmt.Errorf("data too short to contain a transaction")
	}

	payload := make([]byte, payloadSize)
	copy(payload, data[12:12+payloadSize])
	hash := crand.Address(0, int64(id), 32)

	return &transaction{
		id:      id,
		hash:    hash,
		payload: payload,
	}, nil
}
