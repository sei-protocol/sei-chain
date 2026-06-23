package protoutils_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	autopb "github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/pb"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/test/a/pb"
)

const (
	maxTxsPerBlock  = 2000                             // Payload.txs max_count
	maxTotalTxBytes = 2048000                          // Payload.txs max_total_size
	maxBytesPerTx   = maxTotalTxBytes / maxTxsPerBlock // 1024
)

// maxBlock builds a Block at the wireguard limits: maxTxsPerBlock transactions
// each of maxBytesPerTx bytes, with a minimal valid BlockHeader.
func maxBlock() *autopb.Block {
	txs := make([][]byte, maxTxsPerBlock)
	for i := range txs {
		txs[i] = make([]byte, maxBytesPerTx)
	}
	return &autopb.Block{
		Header: &autopb.BlockHeader{
			Lane:        &autopb.PublicKey{Ed25519: make([]byte, 32)},
			BlockNumber: proto.Uint64(1),
			ParentHash:  make([]byte, 32),
			PayloadHash: make([]byte, 32),
		},
		Payload: &autopb.Payload{
			CreatedAt:         &autopb.Timestamp{Seconds: proto.Int64(1), Nanos: proto.Int32(0)},
			TotalGasWanted:    proto.Uint64(100_000_000),
			TotalGasEstimated: proto.Uint64(100_000_000),
			Txs:               txs,
		},
	}
}

// TestUnmarshalWithLimit_MaxBlockAccepted verifies that a legitimately
// max-sized Block (max txs × max bytes/tx + valid header) is accepted by
// UnmarshalWithLimit with a generous limit. This guards against the estimate
// being so conservative that valid messages are incorrectly rejected.
func TestUnmarshalWithLimit_MaxBlockAccepted(t *testing.T) {
	block := maxBlock()
	bz, err := proto.Marshal(block)
	require.NoError(t, err)
	t.Logf("max block wire size: %d bytes", len(bz))

	// Limit is 4MB: 2× the 2MB tx payload, leaving room for header overhead
	// and the ~8× varint amplification in the worst case.
	const limitBytes = 4 << 20
	_, err = protoutils.UnmarshalWithLimit[*autopb.Block](bz, limitBytes)
	require.NoError(t, err, "legitimately max-sized block must be accepted")
}

// amplifiedPayload builds a wire payload for OuterNotSized: 10k empty
// SizedOk message entries. Each encodes as 2 bytes on the wire (tag + length 0)
// but would allocate a full SizedOk struct during proto.Unmarshal.
// Total wire size: ~20KB. Total allocation without the limit guard: many MB.
func amplifiedPayload() []byte {
	var bz []byte
	for range 10_000 {
		bz = protowire.AppendTag(bz, 2, protowire.BytesType) // OuterNotSized.b (repeated SizedOk)
		bz = protowire.AppendBytes(bz, nil)                  // empty SizedOk
	}
	return bz
}

// BenchmarkUnmarshalWithLimit_MaxBlock measures the overhead of allocEstimate
// on a max-sized Block wire payload. The pre-scan should be cheap relative to
// proto.Unmarshal itself.
func BenchmarkUnmarshalWithLimit_MaxBlock(b *testing.B) {
	block := maxBlock()
	bz, err := proto.Marshal(block)
	require.NoError(b, err)
	b.Logf("wire size: %d bytes", len(bz))

	const limitBytes = 4 << 20
	b.ResetTimer()
	b.SetBytes(int64(len(bz)))
	for range b.N {
		_, err := protoutils.UnmarshalWithLimit[*autopb.Block](bz, limitBytes)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkUnmarshalWithLimit_AmplifiedPayload measures the extreme bad case:
// 10k empty repeated-message entries that are ~20KB on the wire but would
// allocate many MB of Go structs during proto.Unmarshal. allocEstimate must
// catch and reject this quickly. Unmarshal is never called for rejected messages.
func BenchmarkUnmarshalWithLimit_AmplifiedPayload(b *testing.B) {
	bz := amplifiedPayload()
	b.Logf("wire size: %d bytes", len(bz))

	const limitBytes = 1 << 20 // 1MB
	b.ResetTimer()
	b.SetBytes(int64(len(bz)))
	for range b.N {
		_, err := protoutils.UnmarshalWithLimit[*pb.OuterNotSized](bz, limitBytes)
		if err == nil {
			b.Fatal("amplified payload must be rejected")
		}
	}
}
