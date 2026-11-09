// Package statesyncproof encodes the Giga state sync proof file shared by the
// running node, the snapshot CLI and the restored-home seed.
//
// A proof is the packed BlockDB row of the proof road: the three records a
// restored node writes into its empty BlockDB. The fourth record, AppProposal,
// is AppQC's proposal. This format is for Giga state sync and is separate from
// sei-tendermint/internal/statesync.
//
// A file stores:
//
//	"SEISSP" magic
//	byte version
//	StateSyncProof protobuf bytes
//
// The file name carries the road index; the body does not. Version 1 carries
// what a reader needs to verify the proof road's block and AppHash against the
// epoch. It does not prove the FlatKV state. Compatible extensions use new protobuf fields. Changes a
// reader must understand, such as mainnet state proofs, require a new file
// version so old readers reject them.
package statesyncproof
