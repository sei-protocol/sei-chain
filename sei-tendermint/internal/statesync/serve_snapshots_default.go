//go:build !mock_chain_validation

package statesync

// servesSnapshots reports whether the reactor answers peers' snapshot and
// chunk requests.
const servesSnapshots = true
