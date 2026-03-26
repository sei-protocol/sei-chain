package blocksim

import "github.com/sei-protocol/sei-chain/sei-db/common/rand"

// A simulated transaction for the blocksim benchmark.
type transaction struct {
	// The unique ID of the transaction. Used to determinstically generate the transaction hash.
	id uint64

	// The (simulated) hash of the transaction.
	hash []byte

	// Data contained by the transaction. These bytes are randomly generated.
	payload []byte
}

// Creates a new transaction with the given ID.
func NewTransaction(
	id uint64,
	crand *rand.CannedRandom,
) *transaction {
	return &transaction{
		id: id,
	}
}
