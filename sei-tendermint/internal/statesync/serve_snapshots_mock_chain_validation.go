//go:build mock_chain_validation

package statesync

// servesSnapshots is false in this build: it runs only as a reserve, whose state
// diverges from the network's once the network migrates, so its snapshots must
// never reach a peer. Snapshot creation is unaffected.
const servesSnapshots = false
