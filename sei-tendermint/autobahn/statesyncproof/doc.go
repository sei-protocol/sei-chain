// Package statesyncproof reads and writes Giga state sync proof files. The
// node writes them; the snapshot CLI and the restore seed read them. It is
// separate from sei-tendermint/internal/statesync.
//
// A proof file belongs to the FlatKV checkpoint at road H:
//
//	name: the road index as %020d, plus ".bin"
//	body: "SEISSP" magic | version byte | StateSyncProof protobuf
//
// FileName and ParseFileName convert between the road index and the name. The
// protobuf holds the CommitQC, the lane blocks and the AppQC of road H. Encode
// builds a body, and Decode checks a body against the road in its name.
// Version 1 lets a reader verify the road block and the AppHash against the
// epoch; it does not prove the FlatKV state.
//
// NewExporter returns the writer. Each call to Sweep writes the file of each
// retained checkpoint whose AppQC is durable and removes the files of
// checkpoints that are no longer retained. It writes no file for the last road
// of an epoch. A file appears complete or not at all.
//
// A change that readers must understand needs a new version, so that old
// readers reject the file. Other extensions use new protobuf fields.
package statesyncproof
