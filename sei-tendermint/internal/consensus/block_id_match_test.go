package consensus

import (
	"testing"

	"github.com/gogo/protobuf/proto"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/version"
)

func testBlock(t *testing.T) *types.Block {
	t.Helper()
	return testBlockWith(t, nil, nil)
}

func testBlockWith(t *testing.T, txs types.Txs, lastCommit *types.Commit) *types.Block {
	t.Helper()
	valHash := crypto.CRandBytes(32)
	if lastCommit == nil {
		lastCommit = &types.Commit{}
	}
	block := &types.Block{
		Header: types.Header{
			Version:            version.Consensus{Block: version.BlockProtocol, App: 1},
			ChainID:            "test-chain",
			Height:             1,
			ValidatorsHash:     valHash,
			NextValidatorsHash: valHash,
			ConsensusHash:      crypto.CRandBytes(32),
			AppHash:            crypto.CRandBytes(32),
			LastResultsHash:    crypto.CRandBytes(32),
			ProposerAddress:    crypto.CRandBytes(crypto.AddressSize),
		},
		Data:       types.Data{Txs: txs},
		LastCommit: lastCommit,
	}
	block.LastCommitHash = block.LastCommit.Hash()
	block.DataHash = block.Data.Hash(false)
	block.EvidenceHash = block.Evidence.Hash()
	require.NotNil(t, block.Hash())
	return block
}

func TestBlockIDMatches(t *testing.T) {
	block := testBlock(t)
	hash := block.Hash()
	parts, err := block.MakePartSet(types.BlockPartSizeBytes)
	require.NoError(t, err)

	matching := types.BlockID{Hash: hash, PartSetHeader: parts.Header()}
	require.True(t, blockIDMatches(block, parts, matching))

	wrongParts := types.BlockID{
		Hash: hash,
		PartSetHeader: types.PartSetHeader{
			Total: parts.Total(),
			Hash:  crypto.CRandBytes(32),
		},
	}
	require.False(t, blockIDMatches(block, parts, wrongParts))

	wrongHash := types.BlockID{
		Hash:          crypto.CRandBytes(32),
		PartSetHeader: parts.Header(),
	}
	require.False(t, blockIDMatches(block, parts, wrongHash))
	require.False(t, blockIDMatches(nil, parts, matching))
	require.False(t, blockIDMatches(block, nil, matching))
	require.False(t, blockIDMatches(block, parts, types.BlockID{}))
	require.False(t, blockIDMatches(block, parts, types.BlockID{
		Hash: hash, // missing PartSetHeader → incomplete
	}))
}

func TestProposalMatchesLocked(t *testing.T) {
	block := testBlock(t)
	parts, err := block.MakePartSet(types.BlockPartSizeBytes)
	require.NoError(t, err)

	require.True(t, proposalMatchesLocked(block, block, parts, parts))

	otherPartsHeader := types.PartSetHeader{Total: 1, Hash: crypto.CRandBytes(32)}
	otherParts := types.NewPartSetFromHeader(otherPartsHeader)
	require.False(t, proposalMatchesLocked(block, block, parts, otherParts))
}

func TestNonCanonicalPartSetSameHeaderHash(t *testing.T) {
	block := testBlock(t)
	canonical, err := block.MakePartSet(types.BlockPartSizeBytes)
	require.NoError(t, err)

	pbb, err := block.ToProto()
	require.NoError(t, err)
	bz, err := proto.Marshal(pbb)
	require.NoError(t, err)

	// Append an unknown length-delimited protobuf field (field 999).
	nonCanonical := append(append([]byte{}, bz...), 0xba, 0x3e, 0x04, 'j', 'u', 'n', 'k')
	nonCanonicalParts := types.NewPartSetFromData(nonCanonical, types.BlockPartSizeBytes)
	require.False(t, nonCanonicalParts.Header().Equals(canonical.Header()))

	var pbb2 tmproto.Block
	require.NoError(t, proto.Unmarshal(nonCanonical, &pbb2))
	decoded, err := types.BlockFromProto(&pbb2)
	require.NoError(t, err)
	require.True(t, decoded.HashesTo(block.Hash()), "logical header hash unchanged")
	require.False(t, blockIDMatches(decoded, nonCanonicalParts, types.BlockID{
		Hash:          block.Hash(),
		PartSetHeader: canonical.Header(),
	}))
	require.True(t, blockIDMatches(decoded, nonCanonicalParts, types.BlockID{
		Hash:          block.Hash(),
		PartSetHeader: nonCanonicalParts.Header(),
	}))
}

func TestCanonicalPartBytesRoundTripShapes(t *testing.T) {
	ctx := t.Context()
	config := configSetup(t)
	cs, _ := makeState(ctx, t, makeStateArgs{config: config, validators: 2})

	valAddr := crypto.CRandBytes(crypto.AddressSize)
	sig := utils.OrPanic1(crypto.SigFromBytes(crypto.CRandBytes(64)))
	mixedCommit := &types.Commit{
		Height: 1,
		Round:  0,
		BlockID: types.BlockID{
			Hash: crypto.CRandBytes(32),
			PartSetHeader: types.PartSetHeader{
				Total: 1,
				Hash:  crypto.CRandBytes(32),
			},
		},
		Signatures: []types.CommitSig{
			{
				BlockIDFlag:      types.BlockIDFlagCommit,
				ValidatorAddress: valAddr,
				Timestamp:        cs.state.LastBlockTime,
				Signature:        utils.Some(sig),
			},
			types.NewCommitSigAbsent(),
		},
	}

	cases := []struct {
		name  string
		block *types.Block
	}{
		{name: "empty", block: testBlock(t)},
		{name: "with_txs", block: testBlockWith(t, types.Txs{[]byte("tx-a"), []byte("tx-b")}, nil)},
		{name: "mixed_last_commit", block: testBlockWith(t, types.Txs{[]byte("tx")}, mixedCommit)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parts, err := tc.block.MakePartSet(types.BlockPartSizeBytes)
			require.NoError(t, err)

			cs.roundState.SetProposal(nil)
			cs.roundState.SetProposalBlockParts(parts)
			require.NoError(t, cs.verifyCanonicalProposalParts(tc.block))

			// Trailing unknown field → different PartSetHeader under the same chunk size.
			pbb, err := tc.block.ToProto()
			require.NoError(t, err)
			bz, err := proto.Marshal(pbb)
			require.NoError(t, err)
			junk := append(append([]byte{}, bz...), 0xba, 0x3e, 0x04, 'j', 'u', 'n', 'k')
			cs.roundState.SetProposalBlockParts(types.NewPartSetFromData(junk, types.BlockPartSizeBytes))
			err = cs.verifyCanonicalProposalParts(tc.block)
			require.ErrorIs(t, err, ErrNonCanonicalProposalParts)
		})
	}
}

// Same canonical bytes with non-default part size yield a different PartSetHeader
// and must be rejected (blocksync rebuilds with BlockPartSizeBytes).
func TestRejectNonDefaultPartChunking(t *testing.T) {
	ctx := t.Context()
	config := configSetup(t)
	cs, _ := makeState(ctx, t, makeStateArgs{config: config, validators: 2})

	// Large enough that a smaller part size splits into multiple parts.
	block := testBlockWith(t, types.Txs{make([]byte, 8*1024)}, nil)
	pbb, err := block.ToProto()
	require.NoError(t, err)
	bz, err := proto.Marshal(pbb)
	require.NoError(t, err)

	altPartSize := uint32(512)
	require.Greater(t, len(bz), int(altPartSize))
	altParts := types.NewPartSetFromData(bz, altPartSize)
	canonical, err := block.MakePartSet(types.BlockPartSizeBytes)
	require.NoError(t, err)
	require.False(t, altParts.Header().Equals(canonical.Header()))
	require.Greater(t, altParts.Total(), uint32(1))

	cs.roundState.SetProposal(nil)
	cs.roundState.SetProposalBlockParts(altParts)
	err = cs.verifyCanonicalProposalParts(block)
	require.ErrorIs(t, err, ErrNonCanonicalProposalParts)
}
