package statesync

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto/ed25519"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	pb "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/statesync"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
)

func TestValidateMsg(t *testing.T) {
	testcases := map[string]struct {
		msg   *pb.Message
		valid bool
	}{
		"nil": {nil, false},

		"ChunkRequest valid":    {wrap(&pb.ChunkRequest{Height: 1, Format: 1, Index: 1}), true},
		"ChunkRequest 0 height": {wrap(&pb.ChunkRequest{Height: 0, Format: 1, Index: 1}), false},
		"ChunkRequest 0 format": {wrap(&pb.ChunkRequest{Height: 1, Format: 0, Index: 1}), true},
		"ChunkRequest 0 chunk":  {wrap(&pb.ChunkRequest{Height: 1, Format: 1, Index: 0}), true},

		"ChunkResponse valid": {
			wrap(&pb.ChunkResponse{Height: 1, Format: 1, Index: 1, Chunk: []byte{1}}),
			true,
		},
		"ChunkResponse 0 height": {
			wrap(&pb.ChunkResponse{Height: 0, Format: 1, Index: 1, Chunk: []byte{1}}),
			false,
		},
		"ChunkResponse 0 format": {
			wrap(&pb.ChunkResponse{Height: 1, Format: 0, Index: 1, Chunk: []byte{1}}),
			true,
		},
		"ChunkResponse 0 chunk": {
			wrap(&pb.ChunkResponse{Height: 1, Format: 1, Index: 0, Chunk: []byte{1}}),
			true,
		},
		"ChunkResponse empty body": {
			wrap(&pb.ChunkResponse{Height: 1, Format: 1, Index: 1, Chunk: []byte{}}),
			true,
		},
		"ChunkResponse nil body": {
			wrap(&pb.ChunkResponse{Height: 1, Format: 1, Index: 1, Chunk: nil}),
			false,
		},
		"ChunkResponse missing": {
			wrap(&pb.ChunkResponse{Height: 1, Format: 1, Index: 1, Missing: true}),
			true,
		},
		"ChunkResponse missing with empty": {
			wrap(&pb.ChunkResponse{Height: 1, Format: 1, Index: 1, Missing: true, Chunk: []byte{}}),
			true,
		},
		"ChunkResponse missing with body": {
			wrap(&pb.ChunkResponse{Height: 1, Format: 1, Index: 1, Missing: true, Chunk: []byte{1}}),
			false,
		},

		"SnapshotsRequest valid": {wrap(&pb.SnapshotsRequest{}), true},

		"SnapshotsResponse valid": {
			wrap(&pb.SnapshotsResponse{Height: 1, Format: 1, Chunks: 2, Hash: []byte{1}}),
			true,
		},
		"SnapshotsResponse 0 height": {
			wrap(&pb.SnapshotsResponse{Height: 0, Format: 1, Chunks: 2, Hash: []byte{1}}),
			false,
		},
		"SnapshotsResponse 0 format": {
			wrap(&pb.SnapshotsResponse{Height: 1, Format: 0, Chunks: 2, Hash: []byte{1}}),
			true,
		},
		"SnapshotsResponse 0 chunks": {
			wrap(&pb.SnapshotsResponse{Height: 1, Format: 1, Hash: []byte{1}}),
			false,
		},
		"SnapshotsResponse no hash": {
			wrap(&pb.SnapshotsResponse{Height: 1, Format: 1, Chunks: 2, Hash: []byte{}}),
			false,
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			if tc.valid {
				require.NoError(t, tc.msg.Validate())
			} else {
				require.Error(t, tc.msg.Validate())
			}
		})
	}
}

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
