package consensus

import (
	"testing"

	"github.com/gogo/protobuf/proto"

	"github.com/sei-protocol/sei-chain/sei-tendermint/crypto"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/version"
)

func testBlock(t *testing.T) *types.Block {
	t.Helper()
	valHash := crypto.CRandBytes(32)
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
		LastCommit: &types.Commit{},
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
