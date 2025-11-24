package statesync

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/libs/utils"
	"github.com/tendermint/tendermint/libs/utils/require"
	pb "github.com/tendermint/tendermint/proto/tendermint/statesync"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

// TODO(gprusak): why do we need this test?
// AFAICT we don't need these messages to have stable representations.
func TestStateSyncVectors(t *testing.T) {
	testCases := []struct {
		testName string
		msg      *pb.Message
		expBytes string
	}{
		{
			"SnapshotsRequest",
			wrap(&pb.SnapshotsRequest{}),
			"0a00",
		},
		{
			"SnapshotsResponse",
			wrap(&pb.SnapshotsResponse{
				Height:   1,
				Format:   2,
				Chunks:   3,
				Hash:     []byte("chuck hash"),
				Metadata: []byte("snapshot metadata"),
			}),
			"1225080110021803220a636875636b20686173682a11736e617073686f74206d65746164617461",
		},
		{
			"ChunkRequest",
			wrap(&pb.ChunkRequest{
				Height: 1,
				Format: 2,
				Index:  3,
			}),
			"1a06080110021803",
		},
		{
			"ChunkResponse",
			wrap(&pb.ChunkResponse{
				Height: 1,
				Format: 2,
				Index:  3,
				Chunk:  []byte("it's a chunk"),
			}),
			"2214080110021803220c697427732061206368756e6b",
		},
		{
			"LightBlockRequest",
			wrap(&pb.LightBlockRequest{
				Height: 100,
			}),
			"2a020864",
		},
		{
			"LightBlockResponse",
			wrap(&pb.LightBlockResponse{
				LightBlock: nil,
			}),
			"3200",
		},
		{
			"ParamsRequest",
			wrap(&pb.ParamsRequest{
				Height: 9001,
			}),
			"3a0308a946",
		},
		{
			"ParamsResponse",
			wrap(&pb.ParamsResponse{
				Height: 9001,
				ConsensusParams: tmproto.ConsensusParams{
					Block: &tmproto.BlockParams{
						MaxBytes: 10,
						MaxGas:   20,
					},
					Evidence: &tmproto.EvidenceParams{
						MaxAgeNumBlocks: 10,
						MaxAgeDuration:  300,
						MaxBytes:        100,
					},
					Validator: &tmproto.ValidatorParams{
						PubKeyTypes: []string{ed25519.KeyType},
					},
					Version: &tmproto.VersionParams{
						AppVersion: 11,
					},
					Synchrony: &tmproto.SynchronyParams{
						MessageDelay: utils.Alloc(550 * time.Nanosecond),
						Precision:    utils.Alloc(90 * time.Nanosecond),
					},
				},
			}),
			"423008a946122b0a04080a10141209080a120310ac0218641a090a07656432353531392202080b2a090a0310a6041202105a",
		},
	}

	for _, tc := range testCases {
		bz, err := tc.msg.Marshal()
		require.NoError(t, err)
		require.Equal(t, tc.expBytes, hex.EncodeToString(bz), tc.testName)
	}
}
